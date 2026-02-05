# Just commands for LogChef Vue3 + Golang project

# Build variables
version := `git describe --tags --always --match 'v*'`
last_commit := `git rev-parse --short HEAD`
# Use UTC timestamp for consistency
build_time := `date -u +%Y%m%dT%H%M%SZ`
# Build info string for embedding
build_info := version + '-commit-' + last_commit + '-build-' + build_time

# Build flags - pass both version and build info
ldflags := "-s -w -X 'main.buildString=" + build_info + "' -X 'main.versionString=" + version + "'"

# Binary output
bin := "bin/logchef.bin"

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

# === CLI (Rust) ===

cli_bin := "cli/target/release/logchef"
cli_bin_debug := "cli/target/debug/logchef"

# Build CLI (release)
build-cli:
    @echo "Building CLI (release)..."
    cd cli && cargo build --release

# Build CLI (debug)
build-cli-debug:
    @echo "Building CLI (debug)..."
    cd cli && cargo build

# Install CLI locally
install-cli: build-cli
    @echo "Installing CLI to ~/.cargo/bin..."
    cp {{cli_bin}} ~/.cargo/bin/logchef
    @echo "CLI installed. Run 'logchef --help' to get started."

# Run CLI tests
test-cli:
    @echo "Running CLI tests..."
    cd cli && cargo test

# Run CLI with cargo clippy
lint-cli:
    @echo "Linting CLI..."
    cd cli && cargo clippy -- -D warnings

# Format CLI code
fmt-cli:
    @echo "Formatting CLI..."
    cd cli && cargo fmt

# Check CLI formatting
fmt-cli-check:
    @echo "Checking CLI formatting..."
    cd cli && cargo fmt --check

# Run all CLI checks
check-cli: fmt-cli-check lint-cli test-cli

# Clean CLI build artifacts
clean-cli:
    @echo "Cleaning CLI artifacts..."
    cd cli && cargo clean

# Quick test: run query against dev server
test-cli-query server_url="" team="" source="":
    @echo "Testing CLI query..."
    {{cli_bin_debug}} --server {{server_url}} query "level:error" --team {{team}} --source {{source}} --limit 10

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

dev-docker-detach:
    cd dev && docker compose up -d

dev-init-tables:
    @echo "Creating ClickHouse tables..."
    docker exec -i dev-clickhouse-local-1 clickhouse-client -n < dev/init-clickhouse.sql

dev-seed: dev-init-tables
    cd dev && ./seed.sh

dev-setup:
    #!/usr/bin/env bash
    set -e
    echo "=== LogChef Dev Setup ==="
    echo "Starting infrastructure..."
    cd dev && docker compose up -d
    echo "Waiting for ClickHouse..."
    for i in {1..30}; do
      if docker exec dev-clickhouse-local-1 clickhouse-client --query "SELECT 1" > /dev/null 2>&1; then
        echo "ClickHouse ready"
        break
      fi
      sleep 1
    done
    cd ..
    just dev-seed
    echo ""
    echo "=== Setup Complete ==="
    echo "Run: just run-backend   (terminal 1)"
    echo "Run: just run-frontend  (terminal 2)"
    echo "Open: http://localhost:5173"

dev-reset:
    #!/usr/bin/env bash
    set -e
    echo "=== LogChef Dev Reset ==="
    echo "Truncating ClickHouse tables..."
    docker exec dev-clickhouse-local-1 clickhouse-client --query "TRUNCATE TABLE IF EXISTS default.http"
    docker exec dev-clickhouse-local-1 clickhouse-client --query "TRUNCATE TABLE IF EXISTS default.syslogs"
    echo "Resetting SQLite data..."
    rm -f local.db local.db-shm local.db-wal
    echo "Re-seeding requires backend to create DB first."
    echo "Run: just run-backend (briefly, then Ctrl+C)"
    echo "Then: just dev-seed"

dev-clean:
    @echo "Stopping containers, removing volumes, and deleting local database..."
    cd dev && docker compose down -v
    rm -f local.db local.db-shm local.db-wal
    @echo "Clean complete. Run 'just dev-setup' to start fresh."

dev-ingest-logs duration="60":
    #!/usr/bin/env bash
    echo "Ingesting logs for {{duration}}s..."
    cd dev
    vector -c http.toml & pid1=$!
    vector -c syslog.toml & pid2=$!
    sleep {{duration}}
    kill $pid1 $pid2 2>/dev/null
    echo "Done."

# View webhook receiver logs (for testing alerts)
dev-webhook-logs:
    cd dev && docker compose logs -f webhook-receiver

# Test webhook receiver is working
dev-test-webhook:
    @echo "Testing webhook receiver..."
    curl -X POST http://localhost:8888/webhook \
        -H "Content-Type: application/json" \
        -d '{"test": "alert", "message": "This is a test webhook payload"}'
    @echo "\nCheck webhook logs with: just dev-webhook-logs"

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
    go test -v -race -coverprofile=coverage/coverage.out ./... && \
    go tool cover -html=coverage/coverage.out -o coverage/coverage.html
    @echo "Coverage report generated at coverage/coverage.html"
    @go tool cover -func=coverage/coverage.out | grep total | awk '{print "Total coverage: " $$3}'

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
