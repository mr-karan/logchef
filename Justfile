# Just commands for LogChef Vue3 + Golang project

# Build variables
last_commit := `git rev-parse --short HEAD`
last_commit_date := `git show -s --format=%ci HEAD`
version := `git describe --tags --always`
build_time := `date +"%Y-%m-%d %H:%M:%S %z"`
build_info := version + " (Commit: " + last_commit_date + " (" + last_commit + "), Build: " + build_time + ")"

# Build flags
ldflags := "-X 'main.buildString=" + build_info + "'"

# Binary output
bin := "bin/logchef.bin"

# Config file - can be overridden with 'just CONFIG=other.toml target'
config := env_var_or_default('CONFIG', 'config.toml')

# Default recipe (runs when just is called with no arguments)
default:
    @just --list

# Build both backend and frontend
build: build-ui build-backend

# Build only the backend
build-backend:
    @echo "Building backend..."
    CGO_ENABLED=0 go build -o ../{{bin}} -ldflags="{{ldflags}}" ./cmd/server

# Build only the frontend
build-ui:
    @echo "Building frontend..."
    cd frontend && \
    [ -d "node_modules" ] || pnpm install --frozen-lockfile --silent && \
    pnpm build

# Run the server with config
run: build
    @echo "Running server with config {{config}}..."
    ../{{bin}} -config {{config}}

# Run only the backend server
run-backend: build-backend
    @echo "Running backend server with config {{config}}..."
    ../{{bin}} -config {{config}}

# Dev mode - run frontend with hot reload
run-frontend:
    cd frontend && pnpm dev

# Clean build artifacts
clean:
    @echo "Cleaning build artifacts..."
    rm -rf bin
    rm -rf coverage
    rm -rf backend/cmd/server/ui
    rm -rf frontend/dist/

# Clean and rebuild everything
fresh: clean build run

# Run tests with coverage
test:
    @echo "Running tests with coverage..."
    mkdir -p coverage
    go test -v -race -coverprofile=../coverage/coverage.out ./... && \
    go tool cover -html=../coverage/coverage.out -o ../coverage/coverage.html
    @echo "Coverage report generated at coverage/coverage.html"
    @go tool cover -func=../coverage/coverage.out | grep total | awk '{print "Total coverage: " $$3}'

# Run tests without coverage and race detection (faster)
test-short:
    @echo "Running tests (short mode)..."
    go test -v ./...

# Run linter
lint:
    golangci-lint run

# Format Go code
fmt:
    go fmt ./...

# Run go vet
vet:
    go vet ./...

# Tidy go modules
tidy:
    go mod tidy

# Run all checks
check: fmt vet lint test

# Run frontend and backend in dev mode (requires tmux)
dev:
    tmux new-session -d -s logchef-dev 'just run-backend'
    tmux split-window -h 'just run-frontend'
    tmux -2 attach-session -d -t logchef-dev
