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
	CGO_ENABLED=1 go build -o bin/collector ./cmd/collector/...
.PHONY: build

build: build-alerter build-ingestor build-collector
.PHONY: build

image: image-ingestor image-alerter image-collector
.PHONY: image

image-ingestor:
	docker build --no-cache -t ghcr.io/azure/adx-mon/ingestor:latest -f build/images/Dockerfile.ingestor .

image-alerter:
	docker build --no-cache -t ghcr.io/azure/adx-mon/alerter:latest -f build/images/Dockerfile.alerter .

image-collector:
	docker build --no-cache -t ghcr.io/azure/adx-mon/collector:latest -f build/images/Dockerfile.collector .

push:
	docker push ghcr.io/azure/adx-mon/alerter:latest
	docker push ghcr.io/azure/adx-mon/ingestor:latest
	docker push ghcr.io/azure/adx-mon/collector:latest

clean:
	rm bin/*
.PHONY: clean

test:
	INTEGRATION=1 go test -timeout 30m ./...
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
	
	# run the container so we can copy
	docker run -d --name temp-crdgen my-crdgen sleep infinity
	
	# copy the generated files in one shell block
	@MY_CRD=$(shell echo $(CRD) | tr A-Z a-z); \
	docker cp temp-crdgen:/code/api/v1/$${MY_CRD}_types.go ./api/v1/$${MY_CRD}_types.go; \
	docker cp temp-crdgen:/code/api/v1/zz_generated.deepcopy.go ./api/v1/zz_generated.deepcopy.go
	docker cp temp-crdgen:/code/PROJECT ./tools/crdgen/PROJECT

	@(for file in $$(docker exec temp-crdgen ls /code/config/crd/bases); do \
		base=$$(basename $$file); \
		rawname=$$(echo $$base | sed 's/^adx-mon\.azure\.com_//'); \
		if echo $$rawname | grep -q '_crd\.yaml$$'; then \
			finalname=$$rawname; \
		else \
			finalname=$$(echo $$rawname | sed -E 's/(\.yaml)$$/_crd\1/'); \
		fi; \
		docker cp temp-crdgen:/code/config/crd/bases/$$file ./kustomize/bases/$$finalname; \
	done)

	# remove the running container
	docker rm -f temp-crdgen
.PHONY: generate-crd

default:
	@$(MAKE) test
	@$(MAKE) build
