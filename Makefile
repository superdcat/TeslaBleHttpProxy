# build vars
TAG_NAME := $(shell test -d .git && git describe --abbrev=0 --tags)
SHA := $(shell test -d .git && git rev-parse --short HEAD)
VERSION := $(if $(TAG_NAME),$(TAG_NAME),$(SHA))

# An empty VERSION (no .git, e.g. a Docker build without --build-arg VERSION) keeps the code default.
VERSION_FLAG := $(if $(VERSION),-X github.com/wimaha/TeslaBleHttpProxy/config.Version=$(VERSION))
LD_FLAGS := $(VERSION_FLAG) -s -w
BUILD_ARGS := -ldflags='$(LD_FLAGS)'
BUILD_DATE := $(shell date -u '+%Y-%m-%d_%H:%M:%S')

# docker (local build only: images are published by .github/workflows/release.yml)
DOCKER_IMAGE := ghcr.io/superdcat/tesla-ble-http-proxy

export DOCKER_CLI_EXPERIMENTAL=enabled

default:: build

lint::
	golangci-lint run

build::
	@echo Version: $(VERSION) $(SHA) $(BUILD_DATE)
	go build $(BUILD_ARGS)

build-docker::
	@echo Version: $(VERSION) $(SHA) $(BUILD_DATE)
	go build $(BUILD_ARGS) -o /go/bin/teslaBleHttpProxy main.go

docker::
	@echo Version: $(VERSION) $(SHA) $(BUILD_DATE)
	docker buildx build --build-arg VERSION=$(VERSION) --tag $(DOCKER_IMAGE):local --output "type=docker,push=false" .
#--progress=plain --no-cache 
