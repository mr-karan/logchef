# syntax=docker/dockerfile:1
FROM golang:1.24.2-bullseye AS builder

# Declare build arguments
ARG APP_VERSION=unknown
ARG TARGETOS=linux
ARG TARGETARCH=amd64

# Install Node.js and pnpm prerequisites (separate update from install for better caching)
RUN apt-get update

# Install necessary dependencies
ENV NODE_VERSION=23.11.0
ENV PNPM_VERSION=10.7.1
ENV SQLC_VERSION=1.28.0
RUN apt-get install -y curl wget xz-utils libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*

# Install Node.js & pnpm
ENV NODE_URL=https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-x64.tar.xz
RUN wget -qO /tmp/node.tar.xz ${NODE_URL} \
    && mkdir -p /usr/local/lib/nodejs \
    && tar -xJf /tmp/node.tar.xz -C /usr/local/lib/nodejs --strip-components=1 \
    && rm /tmp/node.tar.xz
ENV PATH="/usr/local/lib/nodejs/bin:$PATH"
RUN npm install -g pnpm@$PNPM_VERSION

# Download and install sqlc binary directly (instead of go install)
RUN wget -qO /tmp/sqlc.tar.gz https://downloads.sqlc.dev/sqlc_${SQLC_VERSION}_linux_amd64.tar.gz \
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

# Install frontend dependencies using cache mounts
RUN --mount=type=bind,source=frontend/package.json,target=frontend/package.json \
    --mount=type=bind,source=frontend/pnpm-lock.yaml,target=frontend/pnpm-lock.yaml \
    --mount=type=cache,target=/root/.local/share/pnpm/store \
    cd frontend && pnpm install --frozen-lockfile

# Copy all files
COPY . .

# Generate sqlc code
RUN sqlc generate

# Build frontend with cache
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    cd frontend && pnpm build

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

# Copy the default config file
COPY config.toml .

# Expose the application port (update if necessary based on config.toml)
EXPOSE 8125

# Run the binary
ENTRYPOINT ["/app/logchef.bin"]
CMD ["-config", "/app/config.toml"]