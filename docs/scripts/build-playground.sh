#!/usr/bin/env bash
set -eu
cd "$(dirname "$0")/../.."
assets=docs/public/playground-runtime
GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o "$assets/compiler.wasm" ./cmd/logchefql-wasm
install -m 644 "$(go env GOROOT)/lib/wasm/wasm_exec.js" "$assets/wasm_exec.js"
go run ./cmd/logchefql-wasm < docs/src/components/playground/examples.json > docs/src/components/playground/compiled.json
wc -c "$assets/compiler.wasm"
