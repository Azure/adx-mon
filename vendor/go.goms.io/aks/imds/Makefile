VERSION=$(shell git describe --tags --always --dirty)

PROJECT_NAME=aks-imds
DEB_NAME=$(PROJECT_NAME)_$(VERSION)_amd64.deb
IMDS_PROXY_BINARY_NAME=imds-proxy
IMDS_FAKE_BINARY_NAME=imds-faker

.PHONY: default
default: all ;

build: format test
	go build -o bin/imds-proxy -mod vendor cmd/imdsproxy/main.go
	go build -o bin/imds-faker -mod vendor cmd/imdsfaker/main.go

clean:
	rm -rf bin
	rm -rf dist

format:
	goimports -w .
	go fmt ./...

lint:
	golangci-lint run
	go vet ./...

test: lint
	go test ./...

all: build

dist: deb

deb:
	mkdir -p dist
	cd dist && fpm \
	--force \
	--input-type=dir \
	--output-type=deb \
	--name=$(IMDS_PROXY_BINARY_NAME) \
	--version=$(VERSION) \
	--deb-no-default-config-files \
	../bin/$(IMDS_PROXY_BINARY_NAME)=/usr/local/bin/$(IMDS_PROXY_BINARY_NAME) \
	../bin/$(IMDS_FAKE_BINARY_NAME)=/usr/local/bin/$(IMDS_FAKE_BINARY_NAME)

release-deb:
	cd dist \
	&& ../build/scripts/release_deb.sh \
		$(PROJECT_NAME) \
		dist/$(DEB_NAME) \
		$(VERSION) \
		$(shell git rev-parse --verify HEAD)
