.PHONY: test test-local build

ROOT_GOWORK := $(abspath ../../go.work)

test:
	GOWORK=$(ROOT_GOWORK) go test -count=1 ./...

test-local:
	GOWORK=$(ROOT_GOWORK) go test -tags local -count=1 -v ./...

build:
	GOWORK=$(ROOT_GOWORK) go build .
