ARG BASE_IMAGE=registry.opensource.zalan.do/library/alpine-3:latest
ARG VERSION=latest

FROM ubuntu:20.04 as builder

ARG VERSION

COPY  . /go/src/github.com/zalando/postgres-operator
WORKDIR /go/src/github.com/zalando/postgres-operator

ENV OPERATOR_LDFLAGS="-X=main.version=${VERSION}"
RUN bash docker/build_operator.sh

FROM ${BASE_IMAGE}
LABEL maintainer="Team ACID @ Zalando <team-acid@zalando.de>"
LABEL org.opencontainers.image.source="https://github.com/zalando/postgres-operator"

# We need root certificates to deal with teams api over https
RUN apk --no-cache add curl
RUN apk --no-cache add ca-certificates

COPY --from=builder /go/src/github.com/zalando/postgres-operator/build/* /

RUN addgroup -g 1000 pgo
RUN adduser -D -u 1000 -G pgo -g 'Postgres Operator' pgo

USER 1000:1000

ENTRYPOINT ["/postgres-operator"]
