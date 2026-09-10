# Logchef Frontend

Vue 3 + TypeScript frontend.

## Tech Stack

- **Framework**: Vue 3 with `<script setup>` SFCs
- **Build**: [Vite 8](https://vite.dev/) with Rolldown
- **Package Manager**: [Bun](https://bun.sh)
- **Type Checking**: vue-tsc with the TypeScript 7 native bridge
- **Testing**: Vitest

## TypeScript compiler

The `typescript` dependency aliases `typescript-native-bridge`, pinned to
`6.0.3-bridge.16.tsgo.7.0.2`. This runs the TypeScript 7.0.2 checker while
preserving the JavaScript compiler API used by `vue-tsc` for Vue templates.
Stock TypeScript 7.0 does not provide that API.

The bridge requires a supported native binary. Linux builds use glibc, so the
frontend Docker build uses Debian. Alpine/musl cannot run the bridge.
Recheck Vue tooling compatibility before replacing the alias with stock
TypeScript. See the [Vue support issue](https://github.com/vuejs/language-tools/issues/5381).

## Getting Started

```bash
# Install dependencies
bun install

# Start dev server
bun run dev

# Type check
bun run typecheck

# Run tests
bun run test

# Production build
bun run build
```

## Scripts

| Command | Description |
|---------|-------------|
| `bun run dev` | Start dev server with HMR |
| `bun run build` | Production build |
| `bun run build:analyze` | Build with bundle analysis |
| `bun run preview` | Preview production build |
| `bun run typecheck` | Run TypeScript checks |
| `bun run test` | Run tests |
| `bun run test:watch` | Run tests in watch mode |

## Project Structure

```
src/
├── api/          # API client modules
├── components/   # Reusable Vue components
├── composables/  # Vue composition functions
├── layouts/      # App layout components
├── lib/          # Utilities and constants
├── router/       # Vue Router config
├── services/     # Business logic services
├── stores/       # Pinia stores
└── views/        # Page components
```

## IDE Setup

Recommended: [VSCode](https://code.visualstudio.com/) + [Vue - Official](https://marketplace.visualstudio.com/items?itemName=Vue.volar)
