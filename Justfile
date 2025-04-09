# Just commands for LogChef Vue3 + Golang project

# Build variables
version := `git describe --tags --always`
last_commit := `git rev-parse --short HEAD`
# Use UTC timestamp for consistency
build_time := `date -u +%Y%m%dT%H%M%SZ`
# Build info string for embedding
build_info := version + '-commit-' + last_commit + '-build-' + build_time

# Build flags - use build info
ldflags := "-s -w -X 'main.buildString={{build_info}}'"

# Binary output
bin := "bin/logchef.bin"

# Config file - can be overridden with 'just CONFIG=other.toml target'
config := env_var_or_default('CONFIG', 'config.toml')

# sqlc command
sqlc_cmd := "sqlc"

# Default recipe (runs when just is called with no arguments)
default:
    @just --list

# Build both backend and frontend
build: build-ui build-backend

# Generate sqlc code
sqlc-generate:
    @echo "Generating sqlc code..."
    {{sqlc_cmd}} generate

# Build only the backend
build-backend: sqlc-generate
    @echo "Building backend..."
    # LDFLAGS uses the build info
    CGO_ENABLED=0 go build -o {{bin}} -ldflags "{{ldflags}}" ./cmd/server

# Build only the frontend
build-ui:
    @echo "Building frontend..."
    cd frontend && \
    [ -d "node_modules" ] || pnpm install --frozen-lockfile --silent && \
    pnpm build

# Run the server with config
run: build
    @echo "Running server with config {{config}}..."
    {{bin}} -config {{config}}

# Run only the backend server
run-backend: build-backend
    @echo "Running backend server with config {{config}}..."
    {{bin}} -config {{config}}

# Dev mode - run frontend with hot reload
run-frontend:
    cd frontend && pnpm dev

dev-docker:
    cd deploy && docker compose up

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
check: fmt vet lint sqlc-generate test

# === Docker ===
docker-image := "mr-karan/logchef"
docker-tag := version # Use the simple git describe output for the tag

build-docker:
    @echo "Building Docker image {{docker-image}}:{{docker-tag}} for linux/amd64..."
    @echo "Embedding build string: {{build_info}}"
    DOCKER_BUILDKIT=1 docker build \
        --build-arg APP_VERSION="{{build_info}}" \
        --build-arg TARGETOS=linux \
        --build-arg TARGETARCH=amd64 \
        --tag "{{docker-image}}:{{docker-tag}}" \
        --tag "{{docker-image}}:latest" \
        --file Dockerfile \
        .

# Run frontend, backend, and infrastructure in dev mode (requires tmux)
# dev:
#     tmux new-session -d -s logchef-dev 'cd deploy && docker compose up; exec bash'
#     tmux split-window -h 'just run-frontend; exec bash'
#     tmux split-window -v 'just run-backend; exec bash'
#     tmux select-pane -t 0
#     tmux -2 attach-session -d -t logchef-dev
