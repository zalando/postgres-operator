FROM golang:1.13.6 as builder
WORKDIR /app
COPY ./ ./
ARG NAME
ARG VERSION
RUN GOOS=linux GOARCH=386 go build -o /postgres-operator -ldflags "-X \"main.version=$VERSION\""  cmd/main.go


FROM alpine
MAINTAINER Team ACID @ Zalando <team-acid@zalando.de>

# We need root certificates to deal with teams api over https
RUN apk --no-cache add ca-certificates

COPY --from=builder /postgres-operator /

RUN addgroup -g 1000 pgo
RUN adduser -D -u 1000 -G pgo -g 'Postgres Operator' pgo

USER 1000:1000

ENTRYPOINT ["/postgres-operator"]

