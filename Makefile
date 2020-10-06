NAME := cortex-tenant
MAINTAINER := Igor Novgorodov
DESCRIPTION := Cortex tenant proxy
URL := https://github.com/blind-oracle/cortex-tenant
LICENSE := MPL

VERSION := 1.0.0
RELEASE := 1

GO ?= go

RPM := $(NAME)-$(VERSION)-$(RELEASE).x86_64.rpm
DIR := $(NAME)-git
OUT := .out

REPO_HOST := tvovma-mgt035
REPO := cortex

all: rpm

build:
	GOARCH=amd64 \
	GOOS=linux \
	$(GO) build -ldflags "-s -w -extldflags \"-static\" -X main.version=$(VERSION)"

prepare:
	cd deploy && \
	rm -rf $(OUT) && \
	mkdir -p $(OUT)/usr/sbin $(OUT)/var/lib/$(NAME) $(OUT)/etc/sysconfig $(OUT)/usr/lib/systemd/system && \
	cp $(NAME).env $(OUT)/etc/sysconfig/$(NAME) && \
	cp $(NAME).service $(OUT)/usr/lib/systemd/system && \
	cp $(NAME).yml $(OUT)/etc/$(NAME).yml && \
	cp ../$(NAME) $(OUT)/usr/sbin

rpm: build prepare build-rpm

rpm-upload:
	scp $(RPM) $(REPO_HOST):
	ssh $(REPO_HOST) sudo pulp-admin rpm repo uploads rpm --repo-id $(REPO) -f $(RPM)
	ssh $(REPO_HOST) sudo pulp-admin rpm repo publish run --repo-id $(REPO)

build-rpm:
	fpm \
		-s dir \
		--config-files etc/$(NAME).yml \
		--config-files etc/sysconfig/$(NAME) \
		-C deploy/$(OUT)/ \
		-t rpm \
		--after-install deploy/after_install.sh \
		-n $(NAME) \
		-v $(VERSION) \
		--iteration $(RELEASE) \
		--force \
		--rpm-compression bzip2 \
		--rpm-os linux \
		--url $(URL) \
		--description "$(DESCRIPTION)" \
		-m "$(MAINTAINER)" \
		--license "$(LICENSE)" \
		-a amd64 \
		.
