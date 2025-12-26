# LogChef Frontend

Vue 3 + TypeScript frontend built with modern, high-performance tooling.

## Tech Stack

- **Framework**: Vue 3 with `<script setup>` SFCs
- **Build**: [rolldown-vite](https://github.com/vitejs/rolldown-vite) (10-30x faster than Rollup)
- **Package Manager**: [Bun](https://bun.sh) (15-30x faster installs)
- **Type Checking**: vue-tsc
- **Testing**: Vitest

## Performance

| Metric | Time |
|--------|------|
| Install | ~8s |
| Dev server start | ~1s |
| Production build | ~2.3s |

## Getting Started

```bash
# Install dependencies
bun install

# Start dev server (rolldown-vite)
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
