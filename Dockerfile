# Use Go 1.24.2 as base image
FROM golang:1.24.2-bullseye AS builder

# Declare build arguments
ARG APP_VERSION=unknown
ARG TARGETOS=linux
ARG TARGETARCH=amd64

# Install Node.js, pnpm, and sqlc prerequisites
ENV NODE_VERSION=23.11.0
ENV PNPM_VERSION=10.7.1
ENV SQLC_VERSION=v1.28.0
RUN apt-get update && apt-get install -y curl wget xz-utils libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*

# Install Node.js & pnpm
ENV NODE_URL=https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-x64.tar.xz
RUN wget -qO /tmp/node.tar.xz ${NODE_URL} \
    && mkdir -p /usr/local/lib/nodejs \
    && tar -xJf /tmp/node.tar.xz -C /usr/local/lib/nodejs --strip-components=1 \
    && rm /tmp/node.tar.xz
ENV PATH="/usr/local/lib/nodejs/bin:$PATH"
RUN npm install -g pnpm@$PNPM_VERSION

# Install sqlc (pin version for caching)
RUN go install github.com/sqlc-dev/sqlc/cmd/sqlc@${SQLC_VERSION}

# Set working directory
WORKDIR /app

# Copy necessary files for dependency resolution first
COPY go.mod go.sum ./
COPY frontend/package.json frontend/pnpm-lock.yaml ./frontend/
COPY sqlc.yaml ./

# Download Go dependencies
RUN go mod download

# Install frontend dependencies
RUN cd frontend && pnpm install --frozen-lockfile

# Copy the rest of the application code
COPY . .

# Build frontend
RUN cd frontend && pnpm build

# Generate sqlc code
RUN sqlc generate

# Build backend
# Use build args directly in ldflags
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
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
COPY --from=builder /app/config.toml .

# Expose the application port (update if necessary based on config.toml)
EXPOSE 8125

# Run the binary
ENTRYPOINT ["/app/logchef.bin"]
CMD ["-config", "/app/config.toml"]