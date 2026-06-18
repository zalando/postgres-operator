FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder
ARG VERSION=latest
ARG TARGETOS
ARG TARGETARCH

COPY  . /go/src/github.com/zalando/postgres-operator
WORKDIR /go/src/github.com/zalando/postgres-operator

RUN go clean -cache -modcache
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o build/postgres-operator -v -ldflags "-X=main.version=${VERSION}" cmd/main.go

FROM ${BASE_IMAGE}
LABEL maintainer="Team ACID @ Zalando <team-acid@zalando.de>"
LABEL org.opencontainers.image.source="https://github.com/zalando/postgres-operator"

# We need root certificates to deal with teams api over https
RUN apk -U upgrade --no-cache \
    && apk add --no-cache curl ca-certificates

FROM --platform=$TARGETPLATFORM alpine:latest
COPY --from=builder /go/src/github.com/zalando/postgres-operator/build/* /

RUN addgroup -g 1000 pgo
RUN adduser -D -u 1000 -G pgo -g 'Postgres Operator' pgo

USER 1000:1000

ENTRYPOINT ["/postgres-operator"]
