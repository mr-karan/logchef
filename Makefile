.PHONY: build build-backend build-ui run run-backend clean test test-short lint help

# Build variables
LAST_COMMIT := $(shell git rev-parse --short HEAD)
LAST_COMMIT_DATE := $(shell git show -s --format=%ci ${LAST_COMMIT})
VERSION := $(shell git describe --tags --always)
BUILD_TIME := $(shell date +"%Y-%m-%d %H:%M:%S %z")
BUILD_INFO := ${VERSION} (Commit: ${LAST_COMMIT_DATE} (${LAST_COMMIT}), Build: ${BUILD_TIME})

# Build flags
LDFLAGS := -X 'main.buildString=${BUILD_INFO}'

# Binary output
BIN := bin/server

# Config file
CONFIG ?= config.toml

# Build both backend and frontend
build: build-ui build-backend ## Build both backend and frontend

# Build only backend (depends on frontend being built)
build-backend: ## Build only the backend
	CGO_ENABLED=0 go build -o ../${BIN} -ldflags="${LDFLAGS}" ./cmd/server

# Build only frontend
build-ui: ## Build only the frontend
	cd frontend && \
		[ -d "node_modules" ] || yarn install --prefer-offline --frozen-lockfile --silent && \
		yarn build

# Run the server with frontend
run: build ## Run the server with config
	../${BIN} -config ${CONFIG}

# Run only the backend (useful during development)
run-backend: build-backend ## Run only the backend server
	../${BIN} -config ${CONFIG}

# Clean build artifacts
clean: ## Clean build artifacts
	rm -rf bin
	rm -rf coverage
	rm -rf backend/cmd/server/ui
	rm -rf frontend/dist/

# Clean and rebuild everything
fresh: clean build run ## Clean and rebuild everything

# Run tests with coverage
test: ## Run tests with coverage
	@echo "Running tests..."
	@mkdir -p coverage
	\
		go test -v -race -coverprofile=../coverage/coverage.out ./... && \
		go tool cover -html=../coverage/coverage.out -o ../coverage/coverage.html
	@echo "Coverage report generated at coverage/coverage.html"
	@go tool cover -func=../coverage/coverage.out | grep total | awk '{print "Total coverage: " $$3}'

# Run tests without coverage (faster)
test-short: ## Run tests without coverage and race detection (faster)
	@echo "Running tests (short mode)..."
	go test -v ./...

# Run linter
lint: ## Run linter
	golangci-lint run

# Format code
fmt: ## Format Go code
	go fmt ./...

# Run go vet
vet: ## Run go vet
	go vet ./...

# Tidy go modules
tidy: ## Tidy go modules
	go mod tidy

# Check all (fmt, vet, lint, test)
check: fmt vet lint test ## Run all checks

help: ## Display this help
	@echo "Usage:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help