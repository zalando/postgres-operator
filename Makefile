.PHONY: clean local test linux macos mocks docker push e2e

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

BASE_IMAGE ?= alpine:latest
IMAGE ?= $(BINARY)
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
	DEBUG_POSTFIX := -debug-$(shell date hhmmss)
	BUILD_FLAGS += -gcflags "-N -l"
else
	DOCKERFILE = Dockerfile
endif

ifeq ($(FRESH),1)
  DEBUG_FRESH=$(shell date +"%H-%M-%S")
endif

#ifdef CDP_PULL_REQUEST_NUMBER
#	CDP_TAG := -${CDP_BUILD_VERSION}
#endif

ifndef GOPATH
	GOPATH := $(HOME)/go
endif

PATH 		:= $(GOPATH)/bin:$(PATH)
SHELL 		:= env PATH="$(PATH)" $(SHELL)
IMAGE_TAG 	:= $(IMAGE):$(TAG)$(CDP_TAG)$(DEBUG_FRESH)$(DEBUG_POSTFIX)

default: local

clean:
	rm -rf build

local: ${SOURCES}
	hack/verify-codegen.sh
	CGO_ENABLED=${CGO_ENABLED} go build -o build/${BINARY} $(LOCAL_BUILD_FLAGS) -ldflags "$(LDFLAGS)" $^

linux: ${SOURCES}
	GOOS=linux GOARCH=amd64 CGO_ENABLED=${CGO_ENABLED} go build -o build/linux/${BINARY} ${BUILD_FLAGS} -ldflags "$(LDFLAGS)" $^

macos: ${SOURCES}
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=${CGO_ENABLED} go build -o build/macos/${BINARY} ${BUILD_FLAGS} -ldflags "$(LDFLAGS)" $^

docker: ${DOCKERDIR}/${DOCKERFILE}
	echo `(env)`
	echo "Tag ${TAG}"
	echo "Version ${VERSION}"
	echo "CDP tag ${CDP_TAG}"
	echo "git describe $(shell git describe --tags --always --dirty)"
	docker build --rm -t "$(IMAGE_TAG)" -f "${DOCKERDIR}/${DOCKERFILE}" --build-arg VERSION="${VERSION}" --build-arg BASE_IMAGE="${BASE_IMAGE}" .

indocker-race:
	docker run --rm -v "${GOPATH}":"${GOPATH}" -e GOPATH="${GOPATH}" -e RACE=1 -w ${PWD} golang:1.25.3 bash -c "make linux"

docker-push:
	echo `(env)`
	echo "Tag ${TAG}"
	echo "Version ${VERSION}"
	echo "CDP tag ${CDP_TAG}"
	echo "git describe $(shell git describe --tags --always --dirty)"
	docker buildx create --config /etc/cdp-buildkitd.toml --driver-opt network=host --bootstrap --use
	docker buildx build --platform "linux/amd64,linux/arm64" \
						--build-arg BASE_IMAGE="${BASE_IMAGE}" \
						--build-arg VERSION="${VERSION}" \
						-t "$(IMAGE_TAG)" \
						-f "${DOCKERDIR}/${DOCKERFILE}" \
						--push .
	echo "$(IMAGE_TAG)"

mocks:
	GO111MODULE=on go generate ./...

tools:
	GO111MODULE=on go get k8s.io/client-go@kubernetes-1.32.9
	GO111MODULE=on go install github.com/golang/mock/mockgen@v1.6.0
	GO111MODULE=on go mod tidy

fmt:
	@gofmt -l -w -s $(DIRS)

vet:
	@go vet $(PKG)
	@staticcheck $(PKG)

deps: tools
	GO111MODULE=on go mod vendor

test:
	hack/verify-codegen.sh
	GO111MODULE=on go test ./...

codegen:
	hack/update-codegen.sh

e2e: docker # build operator image to be tested
	cd e2e; make e2etest
