ARG BASE_IMAGE=ghcr.io/python/python-3.11-slim:latest
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

COPY requirements.txt start_server.sh ./
# Install dependencies for building psycopg2 from source as well
RUN dnf install libpq libpq-devel gcc -y \
    && pip install -r requirements.txt --no-cache-dir \
    && dnf remove libpq-devel gcc -y \
    && dnf clean all

COPY operator_ui operator_ui/

ARG VERSION=dev
RUN sed -i "s/__version__ = .*/__version__ = '${VERSION}'/" operator_ui/__init__.py

CMD ["python", "-m", "operator_ui"]
