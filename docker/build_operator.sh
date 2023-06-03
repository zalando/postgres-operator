#!/bin/bash

export DEBIAN_FRONTEND=noninteractive

arch=$(dpkg --print-architecture)

set -ex

# Install dependencies

apt-get update
apt-get install -y wget

(
    cd /tmp
    wget -q "https://storage.googleapis.com/golang/go1.19.8.linux-${arch}.tar.gz" -O go.tar.gz
    tar -xf go.tar.gz
    mv go /usr/local
    ln -s /usr/local/go/bin/go /usr/bin/go
    go version
)

# Build

export PATH="$PATH:$HOME/go/bin"
export GOPATH="$HOME/go"
mkdir -p build

GO111MODULE=on go mod vendor
CGO_ENABLED=0 go build -o build/postgres-operator -v -ldflags "$OPERATOR_LDFLAGS" cmd/main.go
