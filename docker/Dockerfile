FROM golang:1.22 AS builder
ARG VERSION=latest

ENV DEBIAN_FRONTEND=noninteractive

# We need root certificates to deal with teams api over https
RUN apt update && apt install -y ca-certificates

COPY  . /go/src/github.com/zalando/postgres-operator
WORKDIR /go/src/github.com/zalando/postgres-operator

RUN GO111MODULE=on go mod vendor \
    && CGO_ENABLED=0 go build -o build/postgres-operator -v -ldflags "-s -w -X=main.version=${VERSION}" cmd/main.go

FROM scratch
LABEL maintainer="Team ACID @ Zalando <team-acid@zalando.de>"
LABEL org.opencontainers.image.source="https://github.com/zalando/postgres-operator"

COPY --from=builder /go/src/github.com/zalando/postgres-operator/build/* /
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

# since the ID instead of the name, no passwd file is needed
# if using name (like pgo), then there must be an entry to map to an ID.
USER 1000:1000

ENTRYPOINT ["/postgres-operator"]
