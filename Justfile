# Just commands for LogChef Vue3 + Golang project

# Build variables
version := `git describe --tags --always`
last_commit := `git rev-parse --short HEAD`
# Use UTC timestamp for consistency
build_time := `date -u +%Y%m%dT%H%M%SZ`
# Build info string for embedding
build_info := version + '-commit-' + last_commit + '-build-' + build_time

# Build flags - pass both version and build info
ldflags := "-s -w -X 'main.buildString=" + build_info + "' -X 'main.versionString=" + version + "'"

# Binary output
bin := "bin/logchef.bin"
cli_bin := "bin/logchef"

# Config file - can be overridden with 'just CONFIG=other.toml target'
config := env_var_or_default('CONFIG', 'config.toml')

# sqlc command
sqlc_cmd := "sqlc"

# Default recipe (runs when just is called with no arguments)
default:
    @just --list

# Build both backend and frontend (frontend first - backend embeds the dist)
build: build-ui build-backend

# Generate sqlc code
sqlc-generate:
    @echo "Generating sqlc code..."
    {{sqlc_cmd}} generate

# Build only the backend
build-backend: sqlc-generate
    @echo "Building backend..."
    CGO_ENABLED=0 go build -o {{bin}} -ldflags "{{ldflags}}" ./cmd/server

# Build only the frontend
build-ui:
    @echo "Building frontend..."
    cd frontend && \
    [ -d "node_modules" ] || bun install --frozen-lockfile && \
    bun run build

# Build frontend with bundle analysis
build-ui-analyze:
    @echo "Building frontend with bundle analysis..."
    cd frontend && \
    [ -d "node_modules" ] || bun install --frozen-lockfile && \
    bun run build:analyze

# Run the server with config
run: build
    @echo "Running server with config {{config}}..."
    {{bin}} -config {{config}}

# Run only the backend server
run-backend: build-backend
    @echo "Running backend server with config {{config}}..."
    {{bin}} -config {{config}}

# === CLI ===

# Build the CLI
build-cli:
    @echo "Building CLI..."
    CGO_ENABLED=0 go build -o {{cli_bin}} -ldflags "{{ldflags}}" ./cmd/logchef

# Install CLI locally
install-cli: build-cli
    @echo "Installing CLI to ~/go/bin..."
    cp {{cli_bin}} ~/go/bin/logchef
    @echo "CLI installed. Run 'logchef --help' to get started."

# Run CLI tests
test-cli:
    @echo "Running CLI tests..."
    go test -v ./internal/cli/... ./cmd/logchef/...

# Run CLI tests with coverage
test-cli-coverage:
    @echo "Running CLI tests with coverage..."
    mkdir -p coverage
    go test -v -race -coverprofile=coverage/cli-coverage.out ./internal/cli/... ./cmd/logchef/...
    go tool cover -html=coverage/cli-coverage.out -o coverage/cli-coverage.html
    @echo "CLI coverage report generated at coverage/cli-coverage.html"
    @go tool cover -func=coverage/cli-coverage.out | grep total | awk '{print "CLI coverage: " $$3}'

# Run only the frontend server
run-frontend:
    cd frontend && bun run dev

# Run the documentation server locally
run-docs:
    @echo "Starting documentation development server..."
    cd docs && npm run dev

# Build the documentation
build-docs:
    @echo "Building documentation site..."
    cd docs && npm install && npm run build

# Setup docs custom domain (only run once after creating GitHub Pages)
setup-docs-domain:
    @echo "Setting up custom domain for documentation..."
    echo "logchef.app" > docs/public/CNAME
    @echo "Created CNAME file. Make sure to set up DNS records pointing to GitHub Pages."

dev-docker:
    cd dev && docker compose up

dev-init-tables:
    @echo "Creating ClickHouse tables..."
    docker exec -i dev-clickhouse-local-1 clickhouse-client -n < dev/init-clickhouse.sql

dev-seed: dev-init-tables
    cd dev && ./seed.sh

dev-clean:
    @echo "Stopping containers, removing volumes, and deleting local database..."
    cd dev && docker compose down -v
    rm -f local.db local.db-shm local.db-wal
    @echo "Clean complete. Run 'just dev-docker' then 'just dev-seed' to start fresh."

dev-ingest-logs duration="60":
    #!/usr/bin/env bash
    echo "Ingesting logs for {{duration}}s..."
    cd dev
    vector -c http.toml & pid1=$!
    vector -c syslog.toml & pid2=$!
    sleep {{duration}}
    kill $pid1 $pid2 2>/dev/null
    echo "Done."

# View Alertmanager webhook receiver logs (for testing alerts)
dev-webhook-logs:
    cd dev && docker compose logs -f webhook-receiver

# View Alertmanager logs
dev-alertmanager-logs:
    cd dev && docker compose logs -f alertmanager

# Test webhook receiver is working
dev-test-webhook:
    @echo "Testing webhook receiver..."
    curl -X POST http://localhost:8888/webhook \
        -H "Content-Type: application/json" \
        -d '{"test": "alert", "message": "This is a test webhook payload"}'
    @echo "\nCheck webhook logs with: just dev-webhook-logs"

# Open Alertmanager UI in browser
dev-alertmanager-ui:
    @echo "Opening Alertmanager UI at http://localhost:9093"
    @command -v xdg-open >/dev/null 2>&1 && xdg-open http://localhost:9093 || \
     command -v open >/dev/null 2>&1 && open http://localhost:9093 || \
     echo "Please open http://localhost:9093 in your browser"

# Clean build artifacts
clean:
    @echo "Cleaning build artifacts..."
    rm -rf bin
    rm -rf coverage
    rm -rf backend/cmd/server/ui
    rm -rf frontend/dist/
    rm -rf frontend/node_modules/.vite

# Deep clean (includes node_modules)
clean-all: clean
    @echo "Removing node_modules..."
    rm -rf frontend/node_modules

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

# Build for ARM64 architecture (Apple Silicon)
build-docker-arm:
    @echo "Building Docker image {{docker-image}}:{{docker-tag}} for linux/arm64..."
    @echo "Embedding build string: {{build_info}}"
    DOCKER_BUILDKIT=1 docker build \
        --build-arg APP_VERSION="{{build_info}}" \
        --build-arg TARGETOS=linux \
        --build-arg TARGETARCH=arm64 \
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
