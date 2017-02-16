FROM alpine
MAINTAINER Team ACID @ Zalando <team-acid@zalando.de>

# We need root certificates for dealing with teams api over https
RUN apk --no-cache add ca-certificates

COPY build/* /

ENTRYPOINT ["/postgres-operator"]
