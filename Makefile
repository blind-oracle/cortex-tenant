NAME := cortex-tenant
MAINTAINER := Igor Novgorodov
DESCRIPTION := Cortex tenant proxy
URL := https://github.com/blind-oracle/cortex-tenant
LICENSE := MPL

VERSION := $(shell git describe --exact-match --tags)
RELEASE := 1

GO ?= go
OUT := .out

all: rpm deb

build:
	go test ./... && \
	GOARCH=amd64 \
	GOOS=linux \
	CGO_ENABLED=0 \
	$(GO) build -ldflags "-s -w -extldflags \"-static\" -X main.Version=$(VERSION)"

prepare:
	cd deploy && \
	rm -rf $(OUT) && \
	mkdir -p $(OUT)/etc $(OUT)/usr/sbin $(OUT)/var/lib/$(NAME) $(OUT)/usr/lib/systemd/system && \
	cp $(NAME).yml $(OUT)/etc/$(NAME).yml && \
	cp ../$(NAME) $(OUT)/usr/sbin

rpm: build prepare build-rpm
deb: build prepare build-deb

build-rpm:
	cd deploy && \
	mkdir -p $(OUT)/etc/sysconfig && \
	cp $(NAME).env $(OUT)/etc/sysconfig/$(NAME) && \
	cp $(NAME).rpm.service $(OUT)/usr/lib/systemd/system/$(NAME).service

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

build-deb:
	cd deploy && \
	mkdir -p $(OUT)/etc/default && \
	cp $(NAME).env $(OUT)/etc/default/$(NAME) && \
	cp $(NAME).deb.service $(OUT)/usr/lib/systemd/system/$(NAME).service

	fpm \
		-s dir \
		--config-files etc/$(NAME).yml \
		--config-files etc/default/$(NAME) \
		-C deploy/$(OUT)/ \
		-t deb \
		--after-install deploy/after_install.sh \
		-n $(NAME) \
		-v $(VERSION) \
		--iteration $(RELEASE) \
		--force \
		--url $(URL) \
		--description "$(DESCRIPTION)" \
		-m "$(MAINTAINER)" \
		--license "$(LICENSE)" \
		-a amd64 \
		.
