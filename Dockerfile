# --------- Frontend build stage ---------
FROM node:20-alpine AS frontend-builder

WORKDIR /app/frontend

# Enable pnpm via corepack
RUN corepack enable && corepack prepare pnpm@latest --activate

COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile --silent

COPY frontend/ ./
RUN pnpm build

# --------- Backend build stage ---------
FROM golang:1.21-alpine AS backend-builder

WORKDIR /app

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Embed the frontend build into the backend
RUN mkdir -p ./cmd/server/ui && \
    cp -r frontend/dist/* ./cmd/server/ui/

RUN CGO_ENABLED=0 go build -o /app/bin/logchef.bin -ldflags="-s -w" ./cmd/server

# --------- Final minimal image ---------
FROM alpine:latest

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=backend-builder /app/bin/logchef.bin /app/logchef
COPY --from=backend-builder /app/config.toml /app/config.toml

# Expose default port (adjust if needed)
EXPOSE 8080

ENTRYPOINT ["/app/logchef"]
