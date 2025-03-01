# Version
GIT_HEAD_COMMIT ?= $(shell git rev-parse --short HEAD)
VERSION         ?= $(or $(shell git describe --abbrev=0 --tags --match "v*" 2>/dev/null),$(GIT_HEAD_COMMIT))
GOOS            ?= $(shell go env GOOS)
GOARCH          ?= $(shell go env GOARCH)

# Defaults
REGISTRY        ?= ghcr.io
REPOSITORY      ?= projectcapsule/cortex-tenant
GIT_TAG_COMMIT  ?= $(shell git rev-parse --short $(VERSION))
GIT_MODIFIED_1  ?= $(shell git diff $(GIT_HEAD_COMMIT) $(GIT_TAG_COMMIT) --quiet && echo "" || echo ".dev")
GIT_MODIFIED_2  ?= $(shell git diff --quiet && echo "" || echo ".dirty")
GIT_MODIFIED    ?= $(shell echo "$(GIT_MODIFIED_1)$(GIT_MODIFIED_2)")
GIT_REPO        ?= $(shell git config --get remote.origin.url)
BUILD_DATE      ?= $(shell git log -1 --format="%at" | xargs -I{} sh -c 'if [ "$(shell uname)" = "Darwin" ]; then date -r {} +%Y-%m-%dT%H:%M:%S; else date -d @{} +%Y-%m-%dT%H:%M:%S; fi')
IMG_BASE        ?= $(REPOSITORY)
IMG             ?= $(IMG_BASE):$(VERSION)
FULL_IMG        ?= $(REGISTRY)/$(IMG_BASE)

KIND_K8S_VERSION ?= "v1.30.0"
KIND_K8S_NAME    ?= "cortex-tenant"

## Tool Binaries
KUBECTL ?= kubectl
HELM ?= helm

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

####################
# -- Golang
####################

.PHONY: golint
golint: golangci-lint
	$(GOLANGCI_LINT) run -c .golangci.yml

.PHONY: golint
golint-fix: golangci-lint
	$(GOLANGCI_LINT) run -c .golangci.yml --fix

all: manager

# Run tests
.PHONY: test
test: test-clean  test-clean
	@GO111MODULE=on go test -v $(shell go list ./... | grep -v "e2e") -coverprofile coverage.out

.PHONY: test-clean
test-clean: ## Clean tests cache
	@go clean -testcache

# Build manager binary
manager: generate golint
	go build -o bin/manager

# Run against the configured Kubernetes cluster in ~/.kube/config
run:
	go run .

####################
# -- Docker
####################

KO_PLATFORM     ?= linux/$(GOARCH)
KOCACHE         ?= /tmp/ko-cache
KO_REGISTRY     := ko.local
KO_TAGS         ?= "latest"
ifdef VERSION
KO_TAGS         := $(KO_TAGS),$(VERSION)
endif

LD_FLAGS        := "-X main.Version=$(VERSION) \
					-X main.GitCommit=$(GIT_HEAD_COMMIT) \
					-X main.GitTag=$(VERSION) \
					-X main.GitTreeState=$(GIT_MODIFIED) \
					-X main.BuildDate=$(BUILD_DATE) \
					-X main.GitRepo=$(GIT_REPO)"

# Docker Image Build
# ------------------

.PHONY: ko-build-controller
ko-build-controller: ko
	@echo Building Controller $(FULL_IMG) - $(KO_TAGS) >&2
	@LD_FLAGS=$(LD_FLAGS) KOCACHE=$(KOCACHE) KO_DOCKER_REPO=$(FULL_IMG) \
		$(KO) build ./cmd/ --bare --tags=$(KO_TAGS) --push=false --local --platform=$(KO_PLATFORM)

.PHONY: ko-build-all
ko-build-all: ko-build-controller

# Docker Image Publish
# ------------------

REGISTRY_PASSWORD   ?= dummy
REGISTRY_USERNAME   ?= dummy

.PHONY: ko-login
ko-login: ko
	@$(KO) login $(REGISTRY) --username $(REGISTRY_USERNAME) --password $(REGISTRY_PASSWORD)

.PHONY: ko-publish-controller
ko-publish-controller: ko-login
	@echo Publishing Controller $(FULL_IMG) - $(KO_TAGS) >&2
	@LD_FLAGS=$(LD_FLAGS) KOCACHE=$(KOCACHE) KO_DOCKER_REPO=$(FULL_IMG) \
		$(KO) build ./cmd/ --bare --tags=$(KO_TAGS) --push=true

.PHONY: ko-publish-all
ko-publish-all: ko-publish-controller

####################
# -- Helm
####################

# Helm
SRC_ROOT = $(shell git rev-parse --show-toplevel)

helm-controller-version:
	$(eval VERSION := $(shell grep 'appVersion:' charts/cortex-tenant/Chart.yaml | awk '{print $$2}'))
	$(eval KO_TAGS := $(shell grep 'appVersion:' charts/cortex-tenant/Chart.yaml | awk '{print $$2}'))


helm-docs: helm-doc
	$(HELM_DOCS) --chart-search-root ./charts

helm-lint: ct
	@$(CT) lint --config .github/configs/ct.yaml --validate-yaml=false --all --debug

helm-schema: helm-plugin-schema
	cd charts/cortex-tenant && $(HELM) schema -output values.schema.json

helm-test: kind ct
	@$(KIND) create cluster --wait=60s --name $(KIND_K8S_NAME) --image=kindest/node:$(KIND_K8S_VERSION)
	@$(MAKE) e2e-install-distro
	@$(MAKE) helm-test-exec
	@$(KIND) delete cluster --name $(KIND_K8S_NAME)

helm-test-exec: ct helm-controller-version ko-build-all
	@$(KIND) load docker-image --name cortex-tenant $(FULL_IMG):$(VERSION)
	@$(CT) install --config $(SRC_ROOT)/.github/configs/ct.yaml --all --debug

docker:
	@hash docker 2>/dev/null || {\
		echo "You need docker" &&\
		exit 1;\
	}

####################
# -- Install E2E Tools
####################
e2e: e2e-build e2e-exec e2e-destroy

e2e-build: kind
	$(KIND) create cluster --wait=60s --name $(KIND_K8S_NAME) --config ./e2e/kind.yaml --image=kindest/node:$(KIND_K8S_VERSION)
	$(MAKE) e2e-install

e2e-exec: ginkgo
	$(GINKGO) -r -vv ./e2e

e2e-destroy: kind
	$(KIND) delete cluster --name $(KIND_K8S_NAME)

e2e-install: e2e-install-distro e2e-install-addon

.PHONY: e2e-install
e2e-install-addon: e2e-load-image
	helm upgrade \
	    --dependency-update \
		--debug \
		--install \
		--namespace cortex-tenant \
		--create-namespace \
		--set 'image.pullPolicy=Never' \
		--set "image.tag=$(VERSION)" \
		--set args.logLevel=10 \
		cortex-tenant \
		./charts/cortex-tenant

e2e-install-distro:
	@$(KUBECTL) kustomize e2e/objects/flux/ | kubectl apply -f -
	@$(KUBECTL) kustomize e2e/objects/distro/ | kubectl apply -f -
	@$(MAKE) wait-for-helmreleases

.PHONY: e2e-load-image
e2e-load-image: ko-build-all
	kind load docker-image --name $(KIND_K8S_NAME) $(FULL_IMG):$(VERSION)

wait-for-helmreleases:
	@ echo "Waiting for all HelmReleases to have observedGeneration >= 0..."
	@while [ "$$($(KUBECTL) get helmrelease -A -o jsonpath='{range .items[?(@.status.observedGeneration<0)]}{.metadata.namespace}{" "}{.metadata.name}{"\n"}{end}' | wc -l)" -ne 0 ]; do \
	  sleep 5; \
	done

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

####################
# -- Helm Plugins
####################

HELM_SCHEMA_VERSION   := ""
helm-plugin-schema:
	@$(HELM) plugin install https://github.com/losisin/helm-values-schema-json.git --version $(HELM_SCHEMA_VERSION) || true

HELM_DOCS         := $(LOCALBIN)/helm-docs
HELM_DOCS_VERSION := v1.14.1
HELM_DOCS_LOOKUP  := norwoodj/helm-docs
helm-doc:
	@test -s $(HELM_DOCS) || \
	$(call go-install-tool,$(HELM_DOCS),github.com/$(HELM_DOCS_LOOKUP)/cmd/helm-docs@$(HELM_DOCS_VERSION))

####################
# -- Tools
####################
GINKGO := $(LOCALBIN)/ginkgo
ginkgo:
	$(call go-install-tool,$(GINKGO),github.com/onsi/ginkgo/v2/ginkgo)

CT         := $(LOCALBIN)/ct
CT_VERSION := v3.12.0
CT_LOOKUP  := helm/chart-testing
ct:
	@test -s $(CT) && $(CT) version | grep -q $(CT_VERSION) || \
	$(call go-install-tool,$(CT),github.com/$(CT_LOOKUP)/v3/ct@$(CT_VERSION))

KIND         := $(LOCALBIN)/kind
KIND_VERSION := v0.27.0
KIND_LOOKUP  := kubernetes-sigs/kind
kind:
	@test -s $(KIND) && $(KIND) --version | grep -q $(KIND_VERSION) || \
	$(call go-install-tool,$(KIND),sigs.k8s.io/kind@$(KIND_VERSION))

KO           := $(LOCALBIN)/ko
KO_VERSION   := v0.17.1
KO_LOOKUP    := google/ko
ko:
	@test -s $(KO) && $(KO) -h | grep -q $(KO_VERSION) || \
	$(call go-install-tool,$(KO),github.com/$(KO_LOOKUP)@$(KO_VERSION))

GOLANGCI_LINT          := $(LOCALBIN)/golangci-lint
GOLANGCI_LINT_VERSION  := v1.64.5
GOLANGCI_LINT_LOOKUP   := golangci/golangci-lint
golangci-lint: ## Download golangci-lint locally if necessary.
	@test -s $(GOLANGCI_LINT) && $(GOLANGCI_LINT) -h | grep -q $(GOLANGCI_LINT_VERSION) || \
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/$(GOLANGCI_LINT_LOOKUP)/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION))

# go-install-tool will 'go install' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-install-tool
[ -f $(1) ] || { \
    set -e ;\
    GOBIN=$(LOCALBIN) go install $(2) ;\
}
endef
