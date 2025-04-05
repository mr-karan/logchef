# syntax=docker/dockerfile:1.7

#### --------- Frontend build stage ---------
FROM node:20-alpine AS frontend-builder

WORKDIR /app/frontend

RUN corepack enable && corepack prepare pnpm@latest --activate

COPY frontend/pnpm-lock.yaml ./
COPY frontend/package.json ./
RUN --mount=type=cache,target=/root/.pnpm-store \
    pnpm install --frozen-lockfile --silent

COPY frontend/ ./
RUN --mount=type=cache,target=/root/.pnpm-store \
    pnpm build

#### --------- Backend build stage ---------
FROM golang:1.21-alpine AS backend-builder

WORKDIR /app

RUN apk add --no-cache git

COPY go.mod ./
COPY go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# Embed the frontend build into the backend
RUN mkdir -p ./cmd/server/ui && \
    cp -r frontend/dist/* ./cmd/server/ui/

RUN --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 go build -o /app/bin/logchef.bin -ldflags="-s -w" ./cmd/server

#### --------- Final minimal image ---------
FROM alpine:latest

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=backend-builder /app/bin/logchef.bin /app/logchef
COPY --from=backend-builder /app/config.toml /app/config.toml

EXPOSE 8080

ENTRYPOINT ["/app/logchef"]
