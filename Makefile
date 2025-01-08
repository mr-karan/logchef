.PHONY: build build-backend build-ui run clean test test-short lint help

# Build variables
LAST_COMMIT := $(shell git rev-parse --short HEAD)
LAST_COMMIT_DATE := $(shell git show -s --format=%ci ${LAST_COMMIT})
VERSION := $(shell git describe --tags --always)
BUILDSTR := ${VERSION} (Commit: ${LAST_COMMIT_DATE} (${LAST_COMMIT}), Build: $(shell date +"%Y-%m-%d %H:%M:%S %z"))

# Binary output
BIN := bin/server

# Config file
CONFIG ?= config.toml

# Build both backend and frontend
build: build-ui build-backend ## Build both backend and frontend

# Build only backend (depends on frontend being built)
build-backend: ## Build only the backend
	cd backend && CGO_ENABLED=0 go build -o ../${BIN} -ldflags="-X 'main.buildString=${BUILDSTR}'" ./cmd/server

# Build only frontend
build-ui: ## Build only the frontend
	cd frontend && \
		[ -d "node_modules" ] || yarn install --prefer-offline --frozen-lockfile --silent && \
		yarn build

# Run the server
run: build ## Run the server with config
	cd backend && ../${BIN} -config config.toml

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
	cd backend && \
		go test -v -race -coverprofile=../coverage/coverage.out ./... && \
		go tool cover -html=../coverage/coverage.out -o ../coverage/coverage.html
	@echo "Coverage report generated at coverage/coverage.html"
	@cd backend && go tool cover -func=../coverage/coverage.out | grep total | awk '{print "Total coverage: " $$3}'

# Run tests without coverage (faster)
test-short: ## Run tests without coverage and race detection (faster)
	@echo "Running tests (short mode)..."
	cd backend && go test -v ./...

# Run linter
lint: ## Run linter
	cd backend && golangci-lint run

# Format code
fmt: ## Format Go code
	cd backend && go fmt ./...

# Run go vet
vet: ## Run go vet
	cd backend && go vet ./...

# Tidy go modules
tidy: ## Tidy go modules
	cd backend && go mod tidy

# Check all (fmt, vet, lint, test)
check: fmt vet lint test ## Run all checks

help: ## Display this help
	@echo "Usage:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
