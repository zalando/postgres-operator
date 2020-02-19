.PHONY: clean test appjs docker push mock

IMAGE            ?= registry.opensource.zalan.do/acid/postgres-operator-ui
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
	echo `(env)`
	echo "Tag ${TAG}"
	echo "Version ${VERSION}"
	echo "git describe $(shell git describe --tags --always --dirty)"
	docker build --rm -t "$(IMAGE):$(TAG)" -f Dockerfile .

push: docker
	docker push "$(IMAGE):$(TAG)"

mock:
	docker run -it -p 8080:8080 "$(IMAGE):$(TAG)" --mock
