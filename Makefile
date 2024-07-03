.DEFAULT_GOAL := default

build-alerter:
	mkdir -p bin
	CGO_ENABLED=0 go build -o bin/alerter ./cmd/alerter/...
.PHONY: build

build-ingestor:
	mkdir -p bin
	CGO_ENABLED=0 go build -o bin/ingestor ./cmd/ingestor/...
.PHONY: build

build-collector:
	mkdir -p bin
	CGO_ENABLED=0 go build -o bin/collector ./cmd/collector/...
.PHONY: build

build: build-alerter build-ingestor build-collector
.PHONY: build

clean:
	rm bin/*
.PHONY: clean

test:
	go test ./...
.PHONY: test

e2e:
	KUSTO_INTEGRATION_TEST=true go test -timeout 5m -count=1 -v github.com/Azure/adx-mon/tools/test/logs
.PHONY: e2e

default:
	@$(MAKE) test
	@$(MAKE) build
