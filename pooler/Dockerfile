ARG BASE_IMAGE=alpine:3.22
FROM ${BASE_IMAGE} AS build_stage

RUN apk --update add \
    autoconf automake build-base c-ares-dev git libevent-dev libtool m4 \
    openssl-dev py3-docutils py3-pip python3

WORKDIR /src

RUN git clone \
    --single-branch \
    --branch=stable-1.23 \
    --depth 1 \
    https://github.com/pgbouncer/pgbouncer.git .

RUN git submodule init && git submodule update

RUN ./autogen.sh && \
    ./configure --prefix=/pgbouncer --with-libevent=/usr/lib && \
    sed -i '/dist_man_MANS/d' Makefile && \
    make && \
    make install

FROM ${BASE_IMAGE}

RUN apk -U upgrade --no-cache \
    && apk --no-cache add bash c-ares ca-certificates gettext libevent openssl postgresql-client

RUN addgroup -g 101 -S pgbouncer && \
    adduser -u 100 -S pgbouncer -G pgbouncer && \
    mkdir -p /etc/pgbouncer /var/log/pgbouncer /var/run/pgbouncer /etc/ssl/certs

COPY --from=build_stage /pgbouncer/bin/pgbouncer /bin/pgbouncer
COPY pgbouncer.ini.tmpl /etc/pgbouncer/
COPY entrypoint.sh /entrypoint.sh

RUN chown -R pgbouncer:pgbouncer \
    /var/log/pgbouncer \
    /var/run/pgbouncer \
    /etc/pgbouncer \
    /etc/ssl/certs \
    && chmod +x /entrypoint.sh

USER pgbouncer:pgbouncer
WORKDIR /etc/pgbouncer

ENTRYPOINT ["/bin/sh", "/entrypoint.sh"]