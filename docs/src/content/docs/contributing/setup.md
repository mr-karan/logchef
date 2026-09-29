---
title: Development Setup
description: Complete guide to setting up a local Logchef development environment for building and contributing to the project.
---

This guide covers multiple ways to set up your Logchef development environment.

## Prerequisites

Logchef requires:
- **Go 1.27+** - Backend development
- **Bun 1.4.2** - Frontend and docs package manager (the version CI uses)
- **Rust** - CLI development (optional, only if working on CLI)
- **Docker** - For running ClickHouse and test infrastructure
- **just** - Command runner for development tasks
- **sqlc v1.31.1** - SQL code generation
- **golangci-lint v2.14.0** - Go linting. `just lint` fails if your version differs
  from the one pinned in `.github/workflows/go-tests.yml`.

## Installation

Install dependencies using your system's package manager (or the methods below).

#### Go Installation

Download Go 1.27 or newer from [go.dev/dl](https://go.dev/dl/), or use your
package manager (for example `brew install go`).

#### Bun

Install Bun with your package manager (for example `brew install oven-sh/bun/bun`),
or follow [bun.sh/docs/installation](https://bun.sh/docs/installation). The
frontend lockfile is `frontend/bun.lock`; do not use npm, pnpm, or yarn.

#### Additional Tools

```bash
# just - command runner
cargo install just

# sqlc - SQL code generator (pin the version CI uses)
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1

# golangci-lint - install the pinned release binary, for example:
brew install golangci-lint
golangci-lint version   # must print 2.14.0

# Docker - follow official docs for your OS
# https://docs.docker.com/engine/install/
```

## Initial Setup

After installing dependencies:

### 1. Generate Database Code

```bash
# Generate SQLC code from SQL queries
just sqlc-generate
```

### 2. Start Development Infrastructure

```bash
# Start ClickHouse and Dex OIDC provider
just dev-docker
```

This starts:
- ClickHouse on port 9000 (HTTP: 8123)
- Dex OIDC provider on port 5556
- Sample log data and configurations

### 3. Build the Application

```bash
# Build backend + frontend
just build

# Or build separately
just build-backend
just build-frontend
```

### 4. Run Logchef

```bash
# Run with development config
just CONFIG=dev/config.toml run

# Or run backend/frontend separately
just run-backend
just run-frontend  # In another terminal
```

Access Logchef at `http://localhost:8125`

## Development Workflow

### Common Commands

```bash
# Run all checks (format, vet, lint, sqlc, tests)
just check

# Run tests with coverage
just test

# Run tests without coverage (faster)
just test-short

# Format code
just fmt

# Lint code
just lint

# Vet code
just vet
```

### Working with Database Changes

1. Modify migrations in `internal/store/sqlite/migrations/`
2. Update queries in `internal/store/sqlite/queries.sql`
3. Regenerate code:
   ```bash
   just sqlc-generate
   ```
4. Update models in `pkg/models/` if needed

### Frontend Development

```bash
cd frontend/

# Development server with hot reload
bun run dev

# Type checking
bun run typecheck

# Unit tests
bun run test

# Build for production
bun run build
```

### CLI Development

The CLI is written in Rust and lives in the `cli/` directory:

```bash
# Build debug version (fast compilation)
just build-cli-debug

# Build release version (optimized)
just build-cli

# Run tests
just test-cli

# Lint with clippy
just lint-cli

# Format code
just fmt-cli

# Run all CLI checks
just check-cli
```

The debug binary is at `cli/target/debug/logchef`, release at `cli/target/release/logchef`.

### Docker Development

```bash
# Build Docker image
just build-docker

# Run with Docker Compose
docker compose -f deployment/docker/docker-compose.yml up
```

## Troubleshooting

### SQLC Generation Fails

Ensure sqlc is installed and in your PATH:
```bash
sqlc version
```

### Frontend Build Errors

Reinstall dependencies from the lockfile:
```bash
cd frontend/
bun install --frozen-lockfile
```

### Docker Permission Issues

Add your user to the docker group:
```bash
sudo usermod -aG docker $USER
newgrp docker
```

### Port Already in Use

Check if another process is using the required ports:
```bash
# Backend (8125)
lsof -i :8125

# ClickHouse (9000, 8123)
lsof -i :9000
lsof -i :8123
```

## Next Steps

- Review the [Architecture Overview](/core/architecture) to understand the codebase
- See [Roadmap](/contributing/roadmap) for upcoming features
- Read the main [CONTRIBUTING.md](https://github.com/mr-karan/logchef/blob/main/CONTRIBUTING.md) for contribution guidelines
