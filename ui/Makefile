.PHONY: clean test appjs docker push mock

BINARY ?= postgres-operator-ui
BUILD_FLAGS ?= -v
CGO_ENABLED ?= 0
ifeq ($(RACE),1)
	BUILD_FLAGS += -race -a
    CGO_ENABLED=1
endif

LOCAL_BUILD_FLAGS ?= $(BUILD_FLAGS)
LDFLAGS ?= -X=main.version=$(VERSION)
DOCKERDIR = docker

IMAGE            ?= registry.opensource.zalan.do/acid/$(BINARY)
VERSION          ?= $(shell git describe --tags --always --dirty)
TAG              ?= $(VERSION)
GITHEAD          = $(shell git rev-parse --short HEAD)
GITURL           = $(shell git config --get remote.origin.url)
GITSTATU         = $(shell git status --porcelain || echo 'no changes')
TTYFLAGS         = $(shell test -t 0 && echo '-it')

default: docker

clean:
	rm -fr operator_ui/static/build

test:
	tox

appjs:
	docker run $(TTYFLAGS) -u $$(id -u) -v $$(pwd):/workdir -w /workdir/app node:10.1.0-alpine npm install
	docker run $(TTYFLAGS) -u $$(id -u) -v $$(pwd):/workdir -w /workdir/app node:10.1.0-alpine npm run build

docker: appjs
	docker build --build-arg "VERSION=$(VERSION)" -t "$(IMAGE):$(TAG)" .
	@echo 'Docker image $(IMAGE):$(TAG) can now be used.'

push: docker
	docker push "$(IMAGE):$(TAG)"

mock:
	docker run -it -p 8080:8080 "$(IMAGE):$(TAG)" --mock
