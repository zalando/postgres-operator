.PHONY: clean copy docker push tools test

BINARY ?= postgres-operator-e2e-tests-runner
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

PATH := $(GOPATH)/bin:$(PATH)

default: tools

clean:
	rm -rf manifests

copy: clean
	mkdir manifests
	cp ../manifests -r .

docker: scm-source.json
	docker build -t "$(IMAGE):$(TAG)" .

scm-source.json: ../.git
	echo '{\n "url": "git:$(GITURL)",\n "revision": "$(GITHEAD)",\n "author": "$(USER)",\n "status": "$(GITSTATUS)"\n}' > scm-source.json

push: docker
	docker push "$(IMAGE):$(TAG)"

tools:
	# install pinned version of 'kind'
	# go get must run outside of a dir with a (module-based) Go project !
	# otherwise go get updates project's dependencies and/or behaves differently
	cd "/tmp" && GO111MODULE=on go get sigs.k8s.io/kind@v0.9.0

e2etest: tools copy clean
	./run.sh main
