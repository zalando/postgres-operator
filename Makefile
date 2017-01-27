.PHONY: clean local linux macos docker push scm-source.json

BINARY ?= postgres-operator
BUILD_FLAGS ?= -i
DOCKERFILE = Dockerfile
IMAGE ?= pierone.example.com/acid/$(BINARY)
TAG ?= $(VERSION)
GITHEAD = $(shell git rev-parse --short HEAD)
GITURL = $(shell git config --get remote.origin.url)
GITSTATUS = $(shell git status --porcelain || echo "no changes")
SOURCES = cmd/main.go
VERSION ?= $(shell git describe --tags --always --dirty)
IMAGE ?= pierone.example.com/acid/$(BINARY)

default: local

clean:
	rm -rf build

local: build/${BINARY}
linux: build/linux/${BINARY}
macos: build/macos/${BINARY}

build/${BINARY}: ${SOURCES}
	go build -o $@ $(BUILD_FLAGS) $^

build/linux/${BINARY}: ${SOURCES}
	GOOS=linux GOARCH=amd64 go build -o $@ ${BUILD_FLAGS} $^

build/macos/${BINARY}: ${SOURCES}
	GOOS=darwin GOARCH=amd64 go build -o $@ ${BUILD_FLAGS} $^

docker: ${DOCKERFILE} scm-source.json linux
	docker build --rm -t "$(IMAGE):$(TAG)" -f $< .

push:
	docker push "$(IMAGE):$(TAG)"

scm-source.json: .git
	echo '{\n "url": "$(GITURL)",\n "revision": "$(GITHEAD)",\n "author": "$(USER)",\n "status": "$(GITSTATUS)"\n}' > scm-source.json

