.PHONY: build build-backend build-ui run clean fresh test test-short lint help

BIN := bin/server

LAST_COMMIT := $(shell git rev-parse --short HEAD)
LAST_COMMIT_DATE := $(shell git show -s --format=%ci ${LAST_COMMIT})
VERSION := $(shell git describe --tags --always)
BUILDSTR := ${VERSION} (Commit: ${LAST_COMMIT_DATE} (${LAST_COMMIT}), Build: $(shell date +"%Y-%m-%d %H:%M:%S %z"))

# Build both backend and frontend
build: build-ui build-backend ## Build both backend and frontend

# Build only backend
build-backend: ## Build only the backend
	CGO_ENABLED=0 go build -o ${BIN} -ldflags="-X 'main.buildString=${BUILDSTR}'" ./cmd/server

# Build only frontend
build-ui: ## Build only the frontend
	cd ui && \
		[ -d "node_modules" ] || yarn install --prefer-offline --frozen-lockfile --silent && \
		yarn build

run: build
	./${BIN}

clean:
	rm -rf bin
	rm -rf coverage
	rm -rf internal/static/dist/

fresh: clean build run

test: ## Run tests with coverage
	@echo "Running tests..."
	@mkdir -p coverage
	@go test -v -race -coverprofile=coverage/coverage.out ./...
	@go tool cover -html=coverage/coverage.out -o coverage/coverage.html
	@echo "Coverage report generated at coverage/coverage.html"
	@go tool cover -func=coverage/coverage.out | grep total | awk '{print "Total coverage: " $$3}'

test-short: ## Run tests without coverage and race detection (faster)
	@echo "Running tests (short mode)..."
	@go test -v ./...

lint:
	golangci-lint run

help: ## Display this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
