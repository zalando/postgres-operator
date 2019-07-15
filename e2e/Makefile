.PHONY: clean copy docker push tools test

BINARY ?= postgres-operator-e2e-tests
BUILD_FLAGS ?= -v
CGO_ENABLED ?= 0
ifeq ($(RACE),1)
	BUILD_FLAGS += -race -a
    CGO_ENABLED=1
endif

LOCAL_BUILD_FLAGS ?= $(BUILD_FLAGS)
LDFLAGS ?= -X=main.version=$(VERSION)

IMAGE            ?= registry.opensource.zalan.do/acid/$(BINARY)
VERSION          ?= $(shell git describe --tags --always --dirty)
TAG              ?= $(VERSION)
GITHEAD          = $(shell git rev-parse --short HEAD)
GITURL           = $(shell git config --get remote.origin.url)
GITSTATU         = $(shell git status --porcelain || echo 'no changes')
TTYFLAGS         = $(shell test -t 0 && echo '-it')

ifndef GOPATH
	GOPATH := $(HOME)/go
endif

KIND_PATH := $(GOPATH)/bin
PATH := $(GOPATH)/bin:$(PATH)

default: tools

clean:
	rm -fr manifests

copy: clean
	mkdir manifests
	cp ../manifests -r .

docker: copy
	docker build --build-arg "VERSION=$(VERSION)" -t "$(IMAGE):$(TAG)" .

push: docker
	docker push "$(IMAGE):$(TAG)"

tools: docker
	# install pinned version of 'kind'
	# leave the name as is to avoid overwriting official binary named `kind`
	wget https://github.com/kubernetes-sigs/kind/releases/download/v0.4.0/kind-linux-amd64
	chmod +x kind-linux-amd64
	mv kind-linux-amd64 $(KIND_PATH)

test:
	./run.sh
