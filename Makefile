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
IMAGE ?= ghcr.io/zalando/$(BINARY)
TAG ?= $(VERSION)
GITHEAD = $(shell git rev-parse --short HEAD)
GITURL = $(shell git config --get remote.origin.url)
GITSTATUS = $(shell git status --porcelain || echo "no changes")
SOURCES = cmd/main.go
VERSION ?= $(shell git describe --tags --always --dirty)
CRD_SOURCES    = $(shell find pkg/apis/zalando.org pkg/apis/acid.zalan.do -name '*.go' -not -name '*.deepcopy.go')
GENERATED_CRDS = manifests/postgresteam.crd.yaml manifests/postgresql.crd.yaml pkg/apis/acid.zalan.do/v1/postgresql.crd.yaml
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
	rm $(GENERATED)
	rm $(GENERATED_CRDS)

verify:
	hack/verify-codegen.sh

$(GENERATED): go.mod $(CRD_SOURCES)
	hack/update-codegen.sh

$(GENERATED_CRDS): $(GENERATED)
	go tool controller-gen crd:crdVersions=v1,allowDangerousTypes=true paths=./pkg/apis/acid.zalan.do/... output:crd:dir=manifests
	# only generate postgresteam.crd.yaml and postgresql.crd.yaml for now
	@rm manifests/acid.zalan.do_operatorconfigurations.yaml
	@mv manifests/acid.zalan.do_postgresqls.yaml manifests/postgresql.crd.yaml
	@# hack to use lowercase kind and listKind
	@sed -i -e 's/kind: Postgresql/kind: postgresql/' manifests/postgresql.crd.yaml
	@sed -i -e 's/listKind: PostgresqlList/listKind: postgresqlList/' manifests/postgresql.crd.yaml
	@hack/adjust_postgresql_crd.sh
	@mv manifests/acid.zalan.do_postgresteams.yaml manifests/postgresteam.crd.yaml
	@cp manifests/postgresql.crd.yaml pkg/apis/acid.zalan.do/v1/postgresql.crd.yaml

local: ${SOURCES} $(GENERATED_CRDS)
	CGO_ENABLED=${CGO_ENABLED} go build -o build/${BINARY} $(LOCAL_BUILD_FLAGS) -ldflags "$(LDFLAGS)" $(SOURCES)

linux: ${SOURCES} $(GENERATED_CRDS)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=${CGO_ENABLED} go build -o build/linux/${BINARY} ${BUILD_FLAGS} -ldflags "$(LDFLAGS)" $(SOURCES)

macos: ${SOURCES} $(GENERATED_CRDS)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=${CGO_ENABLED} go build -o build/macos/${BINARY} ${BUILD_FLAGS} -ldflags "$(LDFLAGS)" $(SOURCES)

docker:
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

test: mocks $(GENERATED) $(GENERATED_CRDS)
	GO111MODULE=on go test ./...

codegen: $(GENERATED)

e2e: docker # build operator image to be tested
	cd e2e; make e2etest
