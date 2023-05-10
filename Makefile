.DEFAULT_GOAL := default

build-alerter:
	mkdir -p bin
	CGO_ENABLED=0 go build -o bin/alerter cmd/alerter/main.go
.PHONY: build

build-ingestor:
	mkdir -p bin
	CGO_ENABLED=0 go build -o bin/ingestor cmd/ingestor/main.go
.PHONY: build

build-collector:
	mkdir -p bin
	CGO_ENABLED=0 go build -o bin/collector cmd/collector/main.go
.PHONY: build


build: build-alerter build-ingestor build-collector
.PHONY: build

image:
	docker build -t alerter -f image/Dockerfile.alerter .
	docker build -t ingestor -f image/Dockerfile.ingestor .
	docker build -t collector -f image/Dockerfile.collector .
.PHONY: image

clean:
	rm bin/*
.PHONY: clean

test:
	go test ./...
.PHONY: test

default:
	@$(MAKE) test
	@$(MAKE) build
