.DEFAULT_GOAL := default

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null)
BUILD_TIME ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -X 'github.com/Azure/adx-mon/pkg/version.Version=$(VERSION)' \
	-X 'github.com/Azure/adx-mon/pkg/version.GitCommit=$(GIT_COMMIT)' \
	-X 'github.com/Azure/adx-mon/pkg/version.BuildTime=$(BUILD_TIME)'

build-alerter:
	mkdir -p bin
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o bin/alerter ./cmd/alerter/...
.PHONY: build-alerter

build-ingestor:
	mkdir -p bin
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o bin/ingestor ./cmd/ingestor/...
.PHONY: build-ingestor

build-collector:
	mkdir -p bin
	CGO_ENABLED=1 go build -ldflags="$(LDFLAGS)" -o bin/collector ./cmd/collector/
.PHONY: build-collector

build-operator:
	mkdir -p bin
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o bin/operator ./cmd/operator/...
.PHONY: build-operator

build-adxexporter:
	mkdir -p bin
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o bin/adxexporter ./cmd/adxexporter/...
.PHONY: build-adxexporter

build: build-alerter build-ingestor build-collector build-operator build-adxexporter
.PHONY: build

image: image-ingestor image-alerter image-collector image-operator
.PHONY: image

image-ingestor:
	docker build --no-cache --build-arg VERSION=$(VERSION) --build-arg GIT_COMMIT=$(GIT_COMMIT) --build-arg BUILD_TIME=$(BUILD_TIME) -t ghcr.io/azure/adx-mon/ingestor:latest -f build/images/Dockerfile.ingestor .
.PHONY: image-ingestor

image-alerter:
	docker build --no-cache --build-arg VERSION=$(VERSION) --build-arg GIT_COMMIT=$(GIT_COMMIT) --build-arg BUILD_TIME=$(BUILD_TIME) -t ghcr.io/azure/adx-mon/alerter:latest -f build/images/Dockerfile.alerter .
.PHONY: image-alerter

image-collector:
	docker build --no-cache --build-arg VERSION=$(VERSION) --build-arg GIT_COMMIT=$(GIT_COMMIT) --build-arg BUILD_TIME=$(BUILD_TIME) -t ghcr.io/azure/adx-mon/collector:latest -f build/images/Dockerfile.collector .
.PHONY: image-collector

image-operator:
	docker build --no-cache --build-arg VERSION=$(VERSION) --build-arg GIT_COMMIT=$(GIT_COMMIT) --build-arg BUILD_TIME=$(BUILD_TIME) -t ghcr.io/azure/adx-mon/operator:latest -f build/images/Dockerfile.operator .
.PHONY: image-operator

image-operator-dev:
.PHONY: image-operator-dev

push:
	docker push ghcr.io/azure/adx-mon/alerter:latest
	docker push ghcr.io/azure/adx-mon/ingestor:latest
	docker push ghcr.io/azure/adx-mon/collector:latest
	docker push ghcr.io/azure/adx-mon/operator:latest
.PHONY: push

clean:
	rm bin/*
.PHONY: clean

gendocs:
	go run tools/docgen/config/config.go
.PHONY: gendocs

test:
	ENABLE_ASSERTIONS=true INTEGRATION=1 go test -timeout 30m ./...
.PHONY: test

# Generate CRDs, replacing MY_CRD with the _kind_ of the CRD you want to create
# and CMD is either create, to create a new CRD, or update, to update our existing CRDs.
# 
# To generate a new CRD with kind=TestTest
# make generate-crd CRD=TestTest CMD=create
#
# To update our existing CRDs because of an updated field in api/v1/*.go
# make generate-crd CMD=update
generate-crd:
	docker build --file tools/crdgen/Dockerfile --build-arg crd=$(CRD) --build-arg cmd=$(CMD) -t my-crdgen .
	docker create --name my-crdgen-container my-crdgen

	docker cp my-crdgen-container:/code/bin/. $(shell pwd)/bin
	docker rm my-crdgen-container
	docker rmi my-crdgen

	mv bin/*.yaml kustomize/bases/
	mv bin/*.go api/v1/
	mv bin/PROJECT tools/crdgen/PROJECT

	mkdir -p operator/manifests/crds
	cp kustomize/bases/*.yaml operator/manifests/crds
.PHONY: generate-crd

k8s-bundle:
	./build/scripts/generate-bundle.sh
.PHONY: k8s-bundle

default:
	@$(MAKE) test
	@$(MAKE) build
.PHONY: default
