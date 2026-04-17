FROM container-registry.zalando.net/library/alpine-3.19:latest AS build_stage

WORKDIR /
RUN apk --update add \
    autoconf automake build-base c-ares-dev git libevent-dev libtool m4 \
    openssl-dev py3-docutils py3-pip python3

RUN git clone \
    --single-branch \
    --branch=stable-1.23 \
    --depth 1 \
    https://github.com/pgbouncer/pgbouncer.git src

WORKDIR /bin
RUN ln -s ../usr/bin/rst2man.py rst2man

WORKDIR /src
RUN mkdir /pgbouncer
RUN git submodule init
RUN git submodule update
RUN ./autogen.sh
RUN	./configure --prefix=/pgbouncer --with-libevent=/usr/lib

RUN sed -i '/dist_man_MANS/d' Makefile
RUN make
RUN make install

WORKDIR /src/test
RUN make check

FROM container-registry.zalando.net/library/alpine-3.19:latest
RUN apk -U upgrade --no-cache \
    && apk --no-cache add bash c-ares ca-certificates gettext libevent openssl postgresql-client

RUN \
    addgroup -g 101 -S pgbouncer && \
    adduser -u 100 -S pgbouncer && \
    mkdir -p /etc/pgbouncer /var/log/pgbouncer /var/run/pgbouncer /etc/ssl/certs

COPY --from=build_stage /pgbouncer/bin/pgbouncer /bin/pgbouncer
COPY pgbouncer.ini.tmpl auth_file.txt.tmpl /etc/pgbouncer/
COPY entrypoint.sh ./

RUN chown -R pgbouncer:pgbouncer \
        /var/log/pgbouncer \
        /var/run/pgbouncer \
        /etc/pgbouncer \
        /etc/ssl/certs \
    && chmod 0755 /entrypoint.sh

USER pgbouncer:pgbouncer
ENTRYPOINT ["/bin/sh", "/entrypoint.sh"]
