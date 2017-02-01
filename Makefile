.PHONY: clean local linux macos docker push scm-source.json

ifeq ($(RACE),1)
	GOFLAGS=-race
endif

BINARY ?= postgres-operator
BUILD_FLAGS ?= -i -v
LDFLAGS ?= -X=main.version=$(VERSION)
DOCKERFILE = docker/Dockerfile
IMAGE ?= pierone.example.com/acid/$(BINARY)
TAG ?= $(VERSION)
GITHEAD = $(shell git rev-parse --short HEAD)
GITURL = $(shell git config --get remote.origin.url)
GITSTATUS = $(shell git status --porcelain || echo "no changes")
SOURCES = cmd/main.go
VERSION ?= $(shell git describe --tags --always --dirty)
IMAGE ?= pierone.example.com/acid/$(BINARY)
DIRS := cmd pkg
PKG := `go list ./... | grep -v /vendor/`

PATH := $(GOPATH)/bin:$(PATH)
SHELL := env PATH=$(PATH) $(SHELL)

default: local

clean:
	rm -rf build scm-source.json

local: build/${BINARY}
linux: build/linux/${BINARY}
macos: build/macos/${BINARY}

build/${BINARY}: ${SOURCES}
	go build -o $@ $(BUILD_FLAGS) -ldflags "$(LDFLAGS)" $^

build/linux/${BINARY}: ${SOURCES}
	GOOS=linux GOARCH=amd64 go build -o $@ ${BUILD_FLAGS} -ldflags "$(LDFLAGS)" $^

build/macos/${BINARY}: ${SOURCES}
	GOOS=darwin GOARCH=amd64 go build -o $@ ${BUILD_FLAGS} -ldflags "$(LDFLAGS)" $^

docker-context: scm-source.json linux
	mkdir -p docker/build/
	cp build/linux/${BINARY} scm-source.json docker/build/

docker: ${DOCKERFILE} docker-context
	cd docker && docker build --rm -t "$(IMAGE):$(TAG)" .

push:
	docker push "$(IMAGE):$(TAG)"

scm-source.json: .git
	echo '{\n "url": "$(GITURL)",\n "revision": "$(GITHEAD)",\n "author": "$(USER)",\n "status": "$(GITSTATUS)"\n}' > scm-source.json

tools:
	@go get -u honnef.co/go/staticcheck/cmd/staticcheck
	@go get -u github.com/Masterminds/glide

fmt:
	@gofmt -l -w -s $(DIRS)

vet:
	@go vet $(PKG)
	@staticcheck $(PKG)

deps:
	@glide install
