FROM ubuntu:20.04
LABEL maintainer="Team ACID @ Zalando <team-acid@zalando.de>"

# We need root certificates to deal with teams api over https
RUN apt-get update \
    && apt-get install --no-install-recommends -y \
        curl \
        ca-certificates

COPY build/* /

RUN addgroup -g 1000 pgo
RUN adduser -D -u 1000 -G pgo -g 'Postgres Operator' pgo

USER 1000:1000

ENTRYPOINT ["/postgres-operator"]
