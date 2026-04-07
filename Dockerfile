# syntax=docker/dockerfile:1

# --- Frontend builder stage (Bun) ---
# Vite is configured to write its build output to ../cmd/server/ui (relative to
# frontend/) so the Go binary can embed it. We mirror that layout here so the
# resolved path lands inside this stage's /app workdir.
FROM oven/bun:1-debian AS frontend-builder

WORKDIR /app

# Install dependencies first for better layer caching
COPY frontend/package.json frontend/bun.lock ./frontend/
RUN --mount=type=cache,target=/root/.bun/install/cache \
    cd frontend && bun install --frozen-lockfile

# Copy frontend sources plus the cmd/server/ui scaffolding (index.html etc.)
COPY frontend/ ./frontend/
COPY cmd/server/ui/ ./cmd/server/ui/

# Build — output goes to /app/cmd/server/ui via vite's outDir
RUN --mount=type=cache,target=/root/.bun/install/cache \
    cd frontend && bun run build

# --- Backend builder stage (Go) ---
FROM golang:1.24.2-bullseye AS builder

# Declare build arguments
ARG APP_VERSION=unknown
ARG TARGETOS=linux
ARG TARGETARCH=amd64

# Install build prerequisites
ENV SQLC_VERSION=1.29.0
RUN apt-get update \
    && apt-get install -y curl wget xz-utils libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*

# Download and install sqlc binary directly (instead of go install)
RUN wget -qO /tmp/sqlc.tar.gz https://downloads.sqlc.dev/sqlc_${SQLC_VERSION}_linux_${TARGETARCH}.tar.gz \
    && tar -xzf /tmp/sqlc.tar.gz -C /tmp \
    && mv /tmp/sqlc /usr/local/bin/ \
    && chmod +x /usr/local/bin/sqlc \
    && rm /tmp/sqlc.tar.gz

# Set working directory
WORKDIR /app

# Download Go dependencies using cache mount
RUN --mount=type=bind,source=go.mod,target=go.mod \
    --mount=type=bind,source=go.sum,target=go.sum \
    --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy all files
COPY . .

# Bring in the prebuilt frontend assets from the frontend-builder stage
COPY --from=frontend-builder /app/cmd/server/ui ./cmd/server/ui

# Generate sqlc code
RUN sqlc generate

# Set GOCACHE location for build caching
ENV GOCACHE=/root/.cache/go-build

# Build backend with cache mounts
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags="-s -w -X 'main.buildString=${APP_VERSION}'" \
    -o bin/logchef.bin \
    ./cmd/server

# Use a minimal base image for the final stage
FROM alpine:3.21.3

# Install CA certificates and timezone data
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy the built binary from the builder stage
COPY --from=builder /app/bin/logchef.bin .

# Install the default config at the same path Dockerfile.goreleaser uses so
# both image variants can be substituted for one another in deployments.
RUN mkdir -p /etc/logchef
COPY config.toml /etc/logchef/config.toml

# Expose the application port (update if necessary based on config.toml)
EXPOSE 8125

# Run the binary
ENTRYPOINT ["/app/logchef.bin"]
CMD ["-config", "/etc/logchef/config.toml"]
