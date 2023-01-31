.PHONY: default
default: all ;

format:
	goimports -w .
	go fmt ./...

lint:
	golangci-lint run
	go vet ./...

test:
	go test -v ./...

all: format test

# build logic is encapsulated in docker for repeatability in CI environments
docker:
	docker build .
