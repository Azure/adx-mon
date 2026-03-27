# syntax = docker/dockerfile-upstream:1.22.0-labs

# THIS FILE WAS AUTOMATICALLY GENERATED, PLEASE DO NOT EDIT.
#
# Generated on 2026-03-13T17:41:07Z by kres e68c408.

ARG TOOLCHAIN=scratch

# cleaned up specs and compiled versions
FROM scratch AS generate

# runs markdownlint
FROM docker.io/oven/bun:1.3.10-alpine AS lint-markdown
WORKDIR /src
RUN bun i markdownlint-cli@0.48.0 sentences-per-line@0.5.2
COPY .markdownlint.json .
COPY ./README.md ./README.md
RUN bunx markdownlint --ignore "CHANGELOG.md" --ignore "**/node_modules/**" --ignore '**/hack/chglog/**' --rules markdownlint-sentences-per-line .

# base toolchain image
FROM --platform=${BUILDPLATFORM} ${TOOLCHAIN} AS toolchain
RUN apk --update --no-cache add bash build-base curl jq protoc protobuf-dev

# build tools
FROM --platform=${BUILDPLATFORM} toolchain AS tools
ENV GO111MODULE=on
ARG CGO_ENABLED
ENV CGO_ENABLED=${CGO_ENABLED}
ARG GOTOOLCHAIN
ENV GOTOOLCHAIN=${GOTOOLCHAIN}
ARG GOEXPERIMENT
ENV GOEXPERIMENT=${GOEXPERIMENT}
ENV GOPATH=/go
ARG DEEPCOPY_VERSION
RUN --mount=type=cache,target=/root/.cache/go-build,id=go-kmsg/root/.cache/go-build --mount=type=cache,target=/go/pkg,id=go-kmsg/go/pkg go install github.com/siderolabs/deep-copy@${DEEPCOPY_VERSION} \
	&& mv /go/bin/deep-copy /bin/deep-copy
ARG GOLANGCILINT_VERSION
RUN --mount=type=cache,target=/root/.cache/go-build,id=go-kmsg/root/.cache/go-build --mount=type=cache,target=/go/pkg,id=go-kmsg/go/pkg go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCILINT_VERSION} \
	&& mv /go/bin/golangci-lint /bin/golangci-lint
RUN --mount=type=cache,target=/root/.cache/go-build,id=go-kmsg/root/.cache/go-build --mount=type=cache,target=/go/pkg,id=go-kmsg/go/pkg go install golang.org/x/vuln/cmd/govulncheck@latest \
	&& mv /go/bin/govulncheck /bin/govulncheck
ARG GOFUMPT_VERSION
RUN go install mvdan.cc/gofumpt@${GOFUMPT_VERSION} \
	&& mv /go/bin/gofumpt /bin/gofumpt

# tools and sources
FROM tools AS base
WORKDIR /src
COPY go.mod go.mod
COPY go.sum go.sum
RUN cd .
RUN --mount=type=cache,target=/go/pkg,id=go-kmsg/go/pkg go mod download
RUN --mount=type=cache,target=/go/pkg,id=go-kmsg/go/pkg go mod verify
COPY ./kmsg.go ./kmsg.go
COPY ./message.go ./message.go
COPY ./message_test.go ./message_test.go
COPY ./reader.go ./reader.go
COPY ./reader_test.go ./reader_test.go
COPY ./utils_bsd.go ./utils_bsd.go
COPY ./utils_linux.go ./utils_linux.go
COPY ./writer.go ./writer.go
COPY ./writer_test.go ./writer_test.go
RUN --mount=type=cache,target=/go/pkg,id=go-kmsg/go/pkg go list -mod=readonly all >/dev/null

# runs gofumpt
FROM base AS lint-gofumpt
RUN FILES="$(gofumpt -l .)" && test -z "${FILES}" || (echo -e "Source code is not formatted with 'gofumpt -w .':\n${FILES}"; exit 1)

# runs golangci-lint
FROM base AS lint-golangci-lint
WORKDIR /src
COPY .golangci.yml .
ENV GOGC=50
RUN --mount=type=cache,target=/root/.cache/go-build,id=go-kmsg/root/.cache/go-build --mount=type=cache,target=/root/.cache/golangci-lint,id=go-kmsg/root/.cache/golangci-lint,sharing=locked --mount=type=cache,target=/go/pkg,id=go-kmsg/go/pkg golangci-lint run --config .golangci.yml

# runs golangci-lint fmt
FROM base AS lint-golangci-lint-fmt-run
WORKDIR /src
COPY .golangci.yml .
ENV GOGC=50
RUN --mount=type=cache,target=/root/.cache/go-build,id=go-kmsg/root/.cache/go-build --mount=type=cache,target=/root/.cache/golangci-lint,id=go-kmsg/root/.cache/golangci-lint,sharing=locked --mount=type=cache,target=/go/pkg,id=go-kmsg/go/pkg golangci-lint fmt --config .golangci.yml
RUN --mount=type=cache,target=/root/.cache/go-build,id=go-kmsg/root/.cache/go-build --mount=type=cache,target=/root/.cache/golangci-lint,id=go-kmsg/root/.cache/golangci-lint,sharing=locked --mount=type=cache,target=/go/pkg,id=go-kmsg/go/pkg golangci-lint run --fix --issues-exit-code 0 --config .golangci.yml

# runs govulncheck
FROM base AS lint-govulncheck
WORKDIR /src
COPY --chmod=0755 hack/govulncheck.sh ./hack/govulncheck.sh
RUN --mount=type=cache,target=/root/.cache/go-build,id=go-kmsg/root/.cache/go-build --mount=type=cache,target=/go/pkg,id=go-kmsg/go/pkg ./hack/govulncheck.sh ./...

# runs unit-tests with race detector
FROM base AS unit-tests-race
WORKDIR /src
ARG TESTPKGS
RUN --mount=type=cache,target=/root/.cache/go-build,id=go-kmsg/root/.cache/go-build --mount=type=cache,target=/go/pkg,id=go-kmsg/go/pkg --mount=type=cache,target=/tmp,id=go-kmsg/tmp CGO_ENABLED=1 go test -race ${TESTPKGS}

# runs unit-tests
FROM base AS unit-tests-run
WORKDIR /src
ARG TESTPKGS
RUN --mount=type=cache,target=/root/.cache/go-build,id=go-kmsg/root/.cache/go-build --mount=type=cache,target=/go/pkg,id=go-kmsg/go/pkg --mount=type=cache,target=/tmp,id=go-kmsg/tmp go test -covermode=atomic -coverprofile=coverage.txt -coverpkg=${TESTPKGS} ${TESTPKGS}

# clean golangci-lint fmt output
FROM scratch AS lint-golangci-lint-fmt
COPY --from=lint-golangci-lint-fmt-run /src .

FROM scratch AS unit-tests
COPY --from=unit-tests-run /src/coverage.txt /coverage-unit-tests.txt

