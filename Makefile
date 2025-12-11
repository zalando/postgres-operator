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
CRD_SOURCES    = $(shell find pkg/apis/zalando.org pkg/apis/acid.zalan.do -name '*.go' -not -name '*.deepcopy.go')
GENERATED      = pkg/apis/zalando.org/v1/zz_generated.deepcopy.go pkg/apis/acid.zalan.do/v1/zz_generated.deepcopy.go
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

ifdef CDP_PULL_REQUEST_NUMBER
	CDP_TAG := -${CDP_BUILD_VERSION}
endif

ifndef GOPATH
	GOPATH := $(HOME)/go
endif

PATH 		:= $(GOPATH)/bin:$(PATH)
SHELL 		:= env PATH="$(PATH)" $(SHELL)
IMAGE_TAG 	:= $(IMAGE):$(TAG)$(CDP_TAG)$(DEBUG_FRESH)$(DEBUG_POSTFIX)

default: local

clean:
	rm -rf build

verify:
	hack/verify-codegen.sh

$(GENERATED): go.mod $(CRD_SOURCES)
	hack/update-codegen.sh

local: ${SOURCES} $(GENERATED)
	CGO_ENABLED=${CGO_ENABLED} go build -o build/${BINARY} $(LOCAL_BUILD_FLAGS) -ldflags "$(LDFLAGS)" $(SOURCES)

linux: ${SOURCES} $(GENERATED)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=${CGO_ENABLED} go build -o build/linux/${BINARY} ${BUILD_FLAGS} -ldflags "$(LDFLAGS)" $(SOURCES)

macos: ${SOURCES} $(GENERATED)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=${CGO_ENABLED} go build -o build/macos/${BINARY} ${BUILD_FLAGS} -ldflags "$(LDFLAGS)" $(SOURCES)

docker: ${DOCKERDIR}/${DOCKERFILE}
	echo `(env)`
	echo "Tag ${TAG}"
	echo "Version ${VERSION}"
	echo "CDP tag ${CDP_TAG}"
	echo "git describe $(shell git describe --tags --always --dirty)"
	docker build --rm -t "$(IMAGE_TAG)" -f "${DOCKERDIR}/${DOCKERFILE}" --build-arg VERSION="${VERSION}" --build-arg BASE_IMAGE="${BASE_IMAGE}" .

indocker-race:
	docker run --rm -v "${GOPATH}":"${GOPATH}" -e GOPATH="${GOPATH}" -e RACE=1 -w ${PWD} golang:1.25.3 bash -c "make linux"

mocks:
	GO111MODULE=on go generate ./...

fmt:
	@gofmt -l -w -s $(DIRS)

vet:
	@go vet $(PKG)
	@staticcheck $(PKG)

test: mocks $(GENERATED)
	GO111MODULE=on go test ./...

codegen: $(GENERATED)

e2e: docker # build operator image to be tested
	cd e2e; make e2etest
