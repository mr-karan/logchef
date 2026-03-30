# Contributing to Logchef

Thank you for your interest in contributing to Logchef! We welcome contributions from the community.

## Getting Started

### Development Setup

The easiest way to get started is using our Nix flake:

```bash
# Clone the repository
git clone https://github.com/mr-karan/logchef.git
cd logchef

# Enter the development environment
nix develop
```

For detailed setup instructions including manual installation options, see our [Development Setup Guide](https://logchef.app/contributing/setup).

### Quick Start

```bash
# Generate database code
just sqlc-generate

# Start development infrastructure (ClickHouse, Dex OIDC)
just dev-docker

# Build the application
just build

# Run with development config
just CONFIG=dev/config.toml run
```

Access Logchef at `http://localhost:8125`

## Development Workflow

### Making Changes

1. **Fork and clone** the repository
2. **Create a feature branch**: `git checkout -b feature/your-feature-name`
3. **Make your changes** following our coding standards
4. **Run checks**: `just check` (runs format, vet, lint, sqlc, tests)
5. **Commit your changes** with clear, descriptive messages
6. **Push to your fork** and create a pull request

### Code Quality

Before submitting a PR, ensure:

```bash
# All checks pass
just check

# Tests pass with coverage
just test

# Code is formatted
just fmt

# No linting issues
just lint
```

### Working with Database Changes

If your changes involve database modifications:

1. Create migration files in `internal/sqlite/migrations/`
2. Update queries in `internal/sqlite/queries.sql`
3. Regenerate SQLC code: `just sqlc-generate`
4. Update models in `pkg/models/` if needed

### Frontend Development

```bash
cd frontend/

# Development server
pnpm dev

# Type checking
pnpm typecheck

# Build
pnpm build
```

## Contribution Guidelines

### Code Style

**Backend (Go):**
- Follow standard Go conventions and idioms
- Use `gofmt` for formatting (automated via `just fmt`)
- Write meaningful variable and function names
- Add comments for exported functions and complex logic
- Keep functions focused and reasonably sized

**Frontend (Vue/TypeScript):**
- Follow TypeScript best practices
- Use Composition API for Vue components
- Properly type all props, emits, and composables
- Use Pinia stores for state management
- Follow existing component patterns in `src/components/ui/`

### Commit Messages

Write clear, concise commit messages:

- Use present tense ("Add feature" not "Added feature")
- Use imperative mood ("Move cursor to..." not "Moves cursor to...")
- First line should be 50 characters or less
- Reference issues and pull requests when relevant

Examples:
```
feat: add AI-powered query suggestions
fix: resolve race condition in connection pooling
docs: update Nix setup instructions
refactor: simplify user authentication flow
```

### Pull Request Process

1. **Title**: Use a clear, descriptive title
2. **Description**: Explain what changes you made and why
3. **Testing**: Describe how you tested your changes
4. **Screenshots**: Include screenshots for UI changes
5. **Documentation**: Update docs if adding/changing features
6. **Breaking Changes**: Clearly mark any breaking changes

### Testing

- Write tests for new functionality
- Ensure existing tests pass
- Maintain or improve code coverage
- Test edge cases and error conditions

Run tests:
```bash
# With coverage
just test

# Without coverage (faster)
just test-short
```

## Project Structure

### Backend (Go)
- `internal/app/` - Application bootstrap and dependency injection
- `internal/server/` - HTTP handlers, middleware, routing
- `internal/core/` - Business logic (users, teams, sources, logs)
- `internal/clickhouse/` - ClickHouse client and connection management
- `internal/sqlite/` - SQLite metadata storage with SQLC
- `internal/auth/` - OIDC authentication
- `internal/config/` - Configuration management

### Frontend (Vue 3 + TypeScript)
- `src/views/` - Page-level components
- `src/components/` - Reusable UI components (Radix Vue + Tailwind)
- `src/stores/` - Pinia state management
- `src/api/` - API client functions
- `src/services/` - Business logic and query processing

## Architecture Patterns

### Backend Patterns
- **Dependency Injection**: App struct contains all dependencies
- **SQLC Integration**: Type-safe SQL queries
- **Connection Pooling**: ClickHouse Manager maintains pools per source
- **Middleware Chain**: Authentication, CORS, logging
- **Error Handling**: Standardized responses via `server/response.go`

### Frontend Patterns
- **Pinia Stores**: Centralized state management
- **Composables**: Reusable logic in `src/composables/`
- **API Layer**: Centralized with error handling
- **Component Architecture**: Shadcn/ui-style with TypeScript

## Resources

- **Documentation**: https://logchef.app
- **Development Setup**: https://logchef.app/contributing/setup
- **Architecture**: https://logchef.app/core/architecture
- **Roadmap**: https://logchef.app/contributing/roadmap
- **Development Patterns**: See [CLAUDE.md](./CLAUDE.md)

## Getting Help

- **Issues**: Open an issue on GitHub for bugs or feature requests
- **Discussions**: Use GitHub Discussions for questions and ideas
- **Documentation**: Check the docs at https://logchef.app

## License

By contributing to Logchef, you agree that your contributions will be licensed under the AGPLv3 License.

## Recognition

Contributors will be recognized in our release notes and README. Thank you for helping make Logchef better!
