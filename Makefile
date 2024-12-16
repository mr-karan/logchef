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
	rm -rf coverage

fresh: clean build run

.PHONY: test
test: ## Run tests with coverage
	@echo "Running tests..."
	@mkdir -p coverage
	@go test -v -race -coverprofile=coverage/coverage.out ./...
	@go tool cover -html=coverage/coverage.out -o coverage/coverage.html
	@echo "Coverage report generated at coverage/coverage.html"
	@go tool cover -func=coverage/coverage.out | grep total | awk '{print "Total coverage: " $$3}'

.PHONY: test-short
test-short: ## Run tests without coverage and race detection (faster)
	@echo "Running tests (short mode)..."
	@go test -v ./...

.PHONY: lint
lint:
	golangci-lint run

.PHONY: help
help: ## Display this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
