ARG FROM_FOUNDATION="mcr.microsoft.com/oss/go/microsoft/golang:1.17.9-buster"

FROM ${FROM_FOUNDATION} as foundation

ARG GOLANGCI_VERSION=v1.44.0

ENV GOBIN=/go/bin
ENV PATH=$GOBIN:$PATH

# golangci-lint recommends not using a Go Module dependency to grab the tool so we manually install it instead.
RUN curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh ${GOLANGCI_VERSION} | sh -s -- -b $(go env GOPATH)/bin ${GOLANGCI_VERSION}

# fetch necessary dependencies so this task is only run when go.mod or go.sum change
WORKDIR /project
COPY go.mod .
COPY go.sum .
RUN go mod download

COPY tools.go .
RUN cat tools.go | grep _ | awk -F'"' '{print $2}' | xargs -tI % go install %

FROM foundation as builder

COPY --from=foundation /go/pkg/mod /go/pkg/mod
COPY --from=foundation /go/bin     /go/bin

# WORKAROUND: https://github.com/moby/moby/issues/37965
RUN true

COPY . .

RUN make lint test
