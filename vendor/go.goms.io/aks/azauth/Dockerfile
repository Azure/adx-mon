ARG FROM_FOUNDATION="golang:1.14-buster"

FROM ${FROM_FOUNDATION} as foundation

ARG GOLANGCI_VERSION=v1.25.0

ENV GOBIN=/go/bin
ENV GOFLAGS="-mod=vendor"
ENV PATH=$GOBIN:$PATH

# golangci-lint recommends not using a Go Module dependency to grab the tool so we manually install it instead.
RUN curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh ${GOLANGCI_VERSION} | sh -s -- -b $(go env GOPATH)/bin ${GOLANGCI_VERSION}

WORKDIR /project
COPY . .
RUN make test
