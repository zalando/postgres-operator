FROM ubuntu:18.04
LABEL maintainer="Team ACID @ Zalando <team-acid@zalando.de>"

SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN apt-get update     \
    && apt-get install --no-install-recommends -y \
        apt-utils=1.6.10 \
        ca-certificates=20180409 \
        lsb-release=9.20170808ubuntu1 \
        pigz=2.4-1 \
        python3-pip=9.0.1-2.3~ubuntu1 \
        python3-setuptools=39.0.1-2 \
        curl=7.58.0-2ubuntu3.7 \
        jq=1.5+dfsg-2 \
        gnupg=2.2.4-1ubuntu1.2 \
    && pip3 install --no-cache-dir awscli==1.14.44 --upgrade \
    && echo "deb http://apt.postgresql.org/pub/repos/apt/ $(lsb_release -cs)-pgdg main" > /etc/apt/sources.list.d/pgdg.list \
    && cat /etc/apt/sources.list.d/pgdg.list \
    && curl --silent https://www.postgresql.org/media/keys/ACCC4CF8.asc | apt-key add - \
    && apt-get update \
    && apt-get install --no-install-recommends -y  \
        postgresql-client-11=11.3-1.pgdg18.04+1    \
        postgresql-client-10=10.8-1.pgdg18.04+1    \
        postgresql-client-9.6=9.6.13-1.pgdg18.04+1 \
        postgresql-client-9.5=9.5.17-1.pgdg18.04+1 \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

COPY scm-source.json dump.sh ./

ENV PG_DIR=/usr/lib/postgresql/

ENTRYPOINT ["/dump.sh"]
