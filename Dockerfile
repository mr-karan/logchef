# syntax=docker/dockerfile:1.7

#### --------- Builder stage ---------
FROM golang:1.21-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git nodejs npm curl bash

# Install pnpm and just
RUN corepack enable && corepack prepare pnpm@latest --activate && \
    curl -sSfL https://just.systems/install.sh | bash -s -- --to /usr/local/bin

COPY . .

RUN --mount=type=cache,target=/root/.pnpm-store \
    --mount=type=cache,target=/go/pkg/mod \
    just build

#### --------- Final minimal image ---------
FROM alpine:latest

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /app/bin/logchef.bin /app/logchef
COPY --from=builder /app/config.toml /app/config.toml

EXPOSE 8080

ENTRYPOINT ["/app/logchef"]
