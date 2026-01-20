---
title: Development Setup
description: Complete guide to setting up your LogChef development environment
---

# Development Setup

This guide covers multiple ways to set up your LogChef development environment.

## Prerequisites

LogChef requires:
- **Go 1.24+** - Backend development
- **Node.js 22+** - Frontend development
- **pnpm** - Frontend package management
- **Rust** - CLI development (optional, only if working on CLI)
- **Docker** - For running ClickHouse and test infrastructure
- **just** - Command runner for development tasks
- **sqlc** - SQL code generation

## Setup Options

### Option 1: Nix/NixOS (Recommended)

The easiest way to get started is using the provided Nix flake, which automatically installs all dependencies.

#### Using nix develop

```bash
# Clone the repository
git clone https://github.com/mr-karan/logchef.git
cd logchef

# Enter the development shell
nix develop

# You should see: "👉 Ready to build LogChef. Try: just build or just dev-docker"
```

#### Using direnv (Automatic activation)

For automatic environment activation when you enter the project directory:

1. Install direnv:
   ```bash
   # NixOS: Add to configuration.nix
   environment.systemPackages = [ pkgs.direnv ];

   # Other systems
   nix-env -i direnv
   ```

2. Hook direnv into your shell (`~/.bashrc` or `~/.zshrc`):
   ```bash
   eval "$(direnv hook bash)"  # or zsh
   ```

3. Create `.envrc` in the project root:
   ```bash
   echo "use flake" > .envrc
   direnv allow
   ```

The development environment will now activate automatically!

#### What's Included

The Nix flake provides:
- Go 1.24 with gopls, golangci-lint
- Node.js 22 with pnpm
- Rust toolchain for CLI development
- Development tools: just, sqlc, git
- Infrastructure: docker, docker-compose, vector
- Isolated Go/Node/Rust environments with proper caching

### Option 2: Manual Installation

If you prefer not to use Nix, install dependencies manually:

#### Go Installation

```bash
# Download and install Go 1.24+
wget https://go.dev/dl/go1.24.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.24.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin
```

#### Node.js and pnpm

```bash
# Using nvm (recommended)
curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.39.0/install.sh | bash
nvm install 22
nvm use 22

# Install pnpm
npm install -g pnpm
```

#### Additional Tools

```bash
# just - command runner
cargo install just
# OR
go install github.com/casey/just@latest

# sqlc - SQL code generator
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest

# Docker - follow official docs for your OS
# https://docs.docker.com/engine/install/
```

## Initial Setup

After installing dependencies (via Nix or manually):

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

### 4. Run LogChef

```bash
# Run with development config
just CONFIG=dev/config.toml run

# Or run backend/frontend separately
just run-backend
just run-frontend  # In another terminal
```

Access LogChef at `http://localhost:8125`

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

1. Modify migrations in `internal/sqlite/migrations/`
2. Update queries in `internal/sqlite/queries.sql`
3. Regenerate code:
   ```bash
   just sqlc-generate
   ```
4. Update models in `pkg/models/` if needed

### Frontend Development

```bash
cd frontend/

# Development server with hot reload
pnpm dev

# Type checking
pnpm typecheck

# Build for production
pnpm build
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

If using Nix, make sure you're in the dev shell:
```bash
nix develop
```

### Frontend Build Errors

Clear pnpm cache and reinstall:
```bash
cd frontend/
rm -rf node_modules .pnpm-store
pnpm install
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
- Check [CLAUDE.md](https://github.com/mr-karan/logchef/blob/main/CLAUDE.md) for development patterns
- See [Roadmap](/contributing/roadmap) for upcoming features
- Read the main [CONTRIBUTING.md](https://github.com/mr-karan/logchef/blob/main/CONTRIBUTING.md) for contribution guidelines
