<a href="https://zerodha.tech"><img src="https://zerodha.tech/static/images/github-badge.svg" /></a>

<p align="center"><img src="LOGCHEF.svg" alt="Logchef Logo" /></p>

<p align="center">A modern, single binary, high-performance log analytics platform</p>

<p align="center">
  <a href="https://demo.logchef.app"><strong>Try Demo</strong></a> ·
  <a href="https://logchef.app"><strong>Read Documentation</strong></a> ·
  <a href="https://mrkaran.dev/posts/announcing-logchef/"><strong>Announcement Blog Post</strong></a>
</p>

<p align="center">
  <img alt="LogChef Log Explorer" src="docs/public/screenshots/hero-light.png">
</p>

LogChef is a lightweight, powerful log analytics platform designed for efficient log management and analysis. It operates as a single binary, utilizing ClickHouse for high-performance log storage and querying. LogChef provides an intuitive interface for exploring log data, making it suitable for development teams seeking a robust and scalable solution.

## Features

- **Query-first log exploration**: Fast filtering with both LogChefQL and ClickHouse SQL.
- **AI Query Assistant**: Turn natural language into SQL instantly.
- **Real-time alerting**: Schedule rules and route alerts via Alertmanager.
- **OIDC + RBAC included**: SSO and team-based access out of the box.
- **Schema-agnostic**: Point at any ClickHouse table without migrations.
- **Single binary**: One executable, no runtime dependencies.
- **Comprehensive metrics**: Prometheus metrics for usage and performance.
- **MCP integration**: Model Context Protocol server for AI assistants ([logchef-mcp](https://github.com/mr-karan/logchef-mcp)).

## Quick Start

### Docker

```shell
# Download the Docker Compose file
curl -LO https://raw.githubusercontent.com/mr-karan/logchef/refs/heads/main/deployment/docker/docker-compose.yml

# Start the services
docker compose up -d
```

Access the Logchef interface at `http://localhost:8125`.

## Documentation

For comprehensive documentation, including setup guides, configuration options, and API references, please visit [logchef.app](https://logchef.app).

## Contributing

We welcome contributions! To get started:

1. **Development Setup**: See our [Development Setup Guide](https://logchef.app/contributing/setup) or use the Nix flake:
   ```bash
   nix develop
   just sqlc-generate
   just dev-docker
   just build
   ```

2. **Read the Guidelines**: Check [CONTRIBUTING.md](./CONTRIBUTING.md) for detailed contribution guidelines

3. **Find an Issue**: Look for issues labeled `good first issue` or `help wanted`

4. **Make Your Changes**: Follow our coding standards and run `just check` before submitting

For questions or help, open an issue or start a discussion on GitHub.

## Screenshots

![AI Query Assistant](docs/public/screenshots/ai-light.png)

![Alerting](docs/public/screenshots/alerts-light.png)

![Compact view](docs/public/screenshots/compact-light.png)

![Field exploration](docs/public/screenshots/sidebar-light.png)

## License

LogChef is distributed under the terms of the AGPLv3 License.

### Credits

The Logchef logo was designed by [Namisha Katira](https://www.behance.net/katiranimi015d).
