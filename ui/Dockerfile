ARG BASE_IMAGE=registry.opensource.zalan.do/library/python-3.11-slim:latest
ARG NODE_IMAGE=node:lts-alpine

FROM $NODE_IMAGE AS build

COPY . /workdir
WORKDIR /workdir/app

RUN npm install \
    && npm run build

FROM $BASE_IMAGE
LABEL maintainer="Team ACID @ Zalando <team-acid@zalando.de>"

EXPOSE 8081
WORKDIR /app

RUN apt-get -qq -y update \
 # https://www.psycopg.org/docs/install.html#psycopg-vs-psycopg-binary
 && apt-get -qq -y install --no-install-recommends g++ libpq-dev python3-dev python3-distutils \
 && apt-get -qq -y clean \
 && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .
COPY start_server.sh .
RUN pip install -r requirements.txt

COPY operator_ui operator_ui/
COPY --from=build /workdir/operator_ui/static/build/ operator_ui/static/build/

ARG VERSION=dev
RUN sed -i "s/__version__ = .*/__version__ = '${VERSION}'/" operator_ui/__init__.py

CMD ["python", "-m", "operator_ui"]
