.PHONY: clean test appjs docker push mock

IMAGE            ?= registry.opensource.zalan.do/acid/postgres-operator-ui
VERSION          ?= $(shell git describe --tags --always --dirty)
TAG              ?= $(VERSION)
GITHEAD          = $(shell git rev-parse --short HEAD)
GITURL           = $(shell git config --get remote.origin.url)
GITSTATUS        = $(shell git status --porcelain || echo 'no changes')
TTYFLAGS         = $(shell test -t 0 && echo '-it')

ifdef CDP_PULL_REQUEST_NUMBER
	CDP_TAG := -${CDP_BUILD_VERSION}
endif

default: docker

clean:
	rm -fr operator_ui/static/build

test:
	tox

appjs:
	docker run $(TTYFLAGS) -u $$(id -u) -v $$(pwd):/workdir -w /workdir/app node:lts-alpine npm install --cache /workdir/.npm
	docker run $(TTYFLAGS) -u $$(id -u) -v $$(pwd):/workdir -w /workdir/app node:lts-alpine npm run build --cache /workdir/.npm

docker: appjs
	echo `(env)`
	echo "Tag ${TAG}"
	echo "Version ${VERSION}"
	echo "CDP tag ${CDP_TAG}"
	echo "git describe $(shell git describe --tags --always --dirty)"
	docker build --rm -t "$(IMAGE):$(TAG)$(CDP_TAG)" -f Dockerfile .

push:
	docker push "$(IMAGE):$(TAG)$(CDP_TAG)"

mock:
	docker run -it -p 8081:8081 "$(IMAGE):$(TAG)" --mock
