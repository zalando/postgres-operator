.PHONY: clean local test linux macos docker push scm-source.json e2e-run e2e-tools e2e-build

BINARY ?= postgres-operator
BUILD_FLAGS ?= -v
CGO_ENABLED ?= 0
ifeq ($(RACE),1)
	BUILD_FLAGS += -race -a
    CGO_ENABLED=1
endif

LOCAL_BUILD_FLAGS ?= $(BUILD_FLAGS)
LDFLAGS ?= -X=main.version=$(VERSION)
DOCKERDIR = docker

IMAGE ?= registry.opensource.zalan.do/acid/$(BINARY)
TAG ?= $(VERSION)
GITHEAD = $(shell git rev-parse --short HEAD)
GITURL = $(shell git config --get remote.origin.url)
GITSTATUS = $(shell git status --porcelain || echo "no changes")
SOURCES = cmd/main.go
VERSION ?= $(shell git describe --tags --always --dirty)
DIRS := cmd pkg
PKG := `go list ./... | grep -v /vendor/`

ifeq ($(DEBUG),1)
	DOCKERFILE = DebugDockerfile
	DEBUG_POSTFIX := -debug
	BUILD_FLAGS += -gcflags "-N -l"
else
	DOCKERFILE = Dockerfile
endif

ifdef CDP_PULL_REQUEST_NUMBER
	CDP_TAG := -${CDP_BUILD_VERSION}
endif

KIND_PATH := $(GOPATH)/bin
PATH := $(GOPATH)/bin:$(PATH)
SHELL := env PATH=$(PATH) $(SHELL)

default: local

clean:
	rm -rf build scm-source.json

local: ${SOURCES}
	hack/verify-codegen.sh
	CGO_ENABLED=${CGO_ENABLED} go build -o build/${BINARY} $(LOCAL_BUILD_FLAGS) -ldflags "$(LDFLAGS)" $^

linux: ${SOURCES}
	GOOS=linux GOARCH=amd64 CGO_ENABLED=${CGO_ENABLED} go build -o build/linux/${BINARY} ${BUILD_FLAGS} -ldflags "$(LDFLAGS)" $^

macos: ${SOURCES}
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=${CGO_ENABLED} go build -o build/macos/${BINARY} ${BUILD_FLAGS} -ldflags "$(LDFLAGS)" $^

docker-context: scm-source.json linux
	mkdir -p docker/build/
	cp build/linux/${BINARY} scm-source.json docker/build/

docker: ${DOCKERDIR}/${DOCKERFILE} docker-context
	echo `(env)`
	echo "Tag ${TAG}"
	echo "Version ${VERSION}"
	echo "CDP tag ${CDP_TAG}"
	echo "git describe $(shell git describe --tags --always --dirty)"
	cd "${DOCKERDIR}" && docker build --rm -t "$(IMAGE):$(TAG)$(CDP_TAG)$(DEBUG_POSTFIX)" -f "${DOCKERFILE}" .

indocker-race:
	docker run --rm -v "${GOPATH}":"${GOPATH}" -e GOPATH="${GOPATH}" -e RACE=1 -w ${PWD} golang:1.8.1 bash -c "make linux"

push:
	docker push "$(IMAGE):$(TAG)$(CDP_TAG)"

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

test:
	hack/verify-codegen.sh
	@go test ./...

e2e-build:
	docker build --tag="postgres-operator-e2e-tests" -f e2e/Dockerfile .

e2e-tools:
	# install pinned version of 'kind' 
	# leave the name as is to avoid overwriting official binary named `kind`
	wget https://github.com/kubernetes-sigs/kind/releases/download/v0.3.0/kind-linux-amd64
	chmod +x kind-linux-amd64
	mv kind-linux-amd64 $(KIND_PATH)

e2e-run: docker
	e2e/run.sh
