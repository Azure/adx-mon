FROM golang:1.19.4-bullseye as builder

ADD . /code
WORKDIR /code

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ./bin/prom-adx-linux ./cmd/collector

FROM mcr.microsoft.com/cbl-mariner/distroless/minimal:2.0
COPY --from=builder /code/bin /

ENTRYPOINT ["/prom-adx-linux"]
