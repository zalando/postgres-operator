ARG BASE_IMAGE=registry.opensource.zalan.do/library/alpine-3:latest
FROM golang:1.23-alpine AS builder
ARG VERSION=latest

COPY  . /go/src/github.com/zalando/postgres-operator
WORKDIR /go/src/github.com/zalando/postgres-operator

RUN GO111MODULE=on go mod vendor \
    && CGO_ENABLED=0 go build -o build/postgres-operator -v -ldflags "-X=main.version=${VERSION}" cmd/main.go

FROM ${BASE_IMAGE}
LABEL maintainer="Team ACID @ Zalando <team-acid@zalando.de>"
LABEL org.opencontainers.image.source="https://github.com/zalando/postgres-operator"

# We need root certificates to deal with teams api over https
RUN apk -U upgrade --no-cache \
    && apk add --no-cache curl ca-certificates

COPY --from=builder /go/src/github.com/zalando/postgres-operator/build/* /

RUN addgroup -g 1000 pgo
RUN adduser -D -u 1000 -G pgo -g 'Postgres Operator' pgo

USER 1000:1000

ENTRYPOINT ["/postgres-operator"]
