.PHONY: build run clean

BIN := bin/server

LAST_COMMIT := $(shell git rev-parse --short HEAD)
LAST_COMMIT_DATE := $(shell git show -s --format=%ci ${LAST_COMMIT})
VERSION := $(shell git describe --tags --always)
BUILDSTR := ${VERSION} (Commit: ${LAST_COMMIT_DATE} (${LAST_COMMIT}), Build: $(shell date +"%Y-%m-%d %H:%M:%S %z"))

build: build-ui
	CGO_ENABLED=0 go build -o ${BIN} -ldflags="-X 'main.buildString=${BUILDSTR}'" ./cmd/server

.PHONY: build-ui
build-ui:
	cd ui && yarn install
	cd ui && rm -rf ../internal/static/dist/
	cd ui && yarn run build

run: build
	./${BIN}

clean:
	rm -rf bin

fresh: clean build run

.PHONY: test
test:
	go test -v ./...

.PHONY: lint
lint:
	golangci-lint run
