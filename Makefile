.DEFAULT_GOAL := default

build-alerter:
	mkdir -p bin
	CGO_ENABLED=0 go build -o bin/alerter cmd/alerter/main.go
.PHONY: build

build-ingestor:
	mkdir -p bin
	CGO_ENABLED=0 go build -o bin/ingestor cmd/ingestor/main.go
.PHONY: build

clean:
	rm bin/*
.PHONY: clean

test:
	go test ./...
.PHONY: test

default:
	@$(MAKE) test
	@$(MAKE) build-alerter
	@$(MAKE) build-ingestor
