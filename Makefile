.PHONY: clean local linux macos docker push scm-source.json

BINARY ?= postgres-operator
BUILD_FLAGS ?= -v
CGO_ENABLED ?= 0
ifeq ($(RACE),1)
	BUILD_FLAGS += -race -a
    CGO_ENABLED=1
endif

LOCAL_BUILD_FLAGS ?= $(BUILD_FLAGS)
LDFLAGS ?= -X=main.version=$(VERSION)
DOCKERFILE = docker/Dockerfile
IMAGE ?= nboukeffa/$(BINARY)
TAG ?= $(VERSION)
GITHEAD = $(shell git rev-parse --short HEAD)
GITURL = $(shell git config --get remote.origin.url)
GITSTATUS = $(shell git status --porcelain || echo "no changes")
SOURCES = cmd/main.go
VERSION ?= $(shell git describe --tags --always --dirty)
DIRS := cmd pkg
PKG := `go list ./... | grep -v /vendor/`

PATH := $(GOPATH)/bin:$(PATH)
SHELL := env PATH=$(PATH) $(SHELL)

default: local

clean:
	rm -rf build scm-source.json

local: ${SOURCES}
	CGO_ENABLED=${CGO_ENABLED} go build -o build/${BINARY} $(LOCAL_BUILD_FLAGS) -ldflags "$(LDFLAGS)" $^

linux: ${SOURCES}
	GOOS=linux GOARCH=amd64 CGO_ENABLED=${CGO_ENABLED} go build -o build/linux/${BINARY} ${BUILD_FLAGS} -ldflags "$(LDFLAGS)" $^

macos: ${SOURCES}
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=${CGO_ENABLED} go build -o build/macos/${BINARY} ${BUILD_FLAGS} -ldflags "$(LDFLAGS)" $^

docker-context: scm-source.json linux
	mkdir -p docker/build/
	cp build/linux/${BINARY} scm-source.json docker/build/

docker: ${DOCKERFILE} docker-context
	cd docker && docker build --rm -t "$(IMAGE):$(TAG)" .

indocker-race:
	docker run --rm -v "${GOPATH}":"${GOPATH}" -e GOPATH="${GOPATH}" -e RACE=1 -w ${PWD} golang:1.8.1 bash -c "make linux"

push:
	docker push "$(IMAGE):$(TAG)"

scm-source.json: .git
	echo '{\n "url": "git:$(GITURL)",\n "revision": "$(GITHEAD)",\n "author": "$(USER)",\n "status": "$(GITSTATUS)"\n}' > scm-source.json

tools:
	@go get -u honnef.co/go/tools/cmd/staticcheck
	@go get -u github.com/Masterminds/glide

fmt:
	@gofmt -l -w -s $(DIRS)

vet:
	@go vet $(PKG)
	@staticcheck $(PKG)

deps:
	@glide install --strip-vendor
