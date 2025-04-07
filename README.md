# Logchef

A modern, high-performance log analytics platform that runs as a single binary.

Logchef combines the power of Clickhouse for high-speed log storage with an intuitive frontend, offering powerful querying capabilities and visualization tools for development teams of all sizes.

## Key Features

- **Schema Neutral** - Query logs from any Clickhouse database/table instead of being rigid on schemas
- **High Performance** - Built for speed with optimized queries and efficient storage
- **Low Resource Usage** - Minimal footprint with maximum capabilities
- **Single Binary Deployment** - Easy to install and manage
- **Powerful Query Interface** - SQL-based queries with visual builder
- **User Management** - Multi-tenant with roles and permissions
- **Real-time Visualizations** - Dashboards, charts, and graphs. (_Coming Soon!_)
- **Alert System** - Configure alerts based on log conditions (_Coming Soon!_)

## Demo

## Screenshots

## Installation

### Docker

```shell
# Download the compose file
curl -LO https://github.com/mr-karan/logchef/raw/master/docker-compose.yml

# Run the services
docker compose up -d
```

Visit `http://localhost:8125`

### Binary

- Download the [latest release](https://github.com/mr-karan/logchef/releases) and extract the binary
- Run `./logchef` and visit `http://localhost:8125`

## Log Ingestion

Logchef works with [Vector](https://vector.dev/) as the agent to collect and forward logs to your Clickhouse database.

Example Vector config:

```toml
# Vector config example will be provided here
```

You can also ingest logs directly via HTTP:

```bash
curl -X POST http://localhost:8080/api/v1/logs \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"message": "Test log entry", "level": "info"}'
```

## Tech Stack

- **Backend**: Go with Echo framework
- **Database**: Clickhouse for logs, PostgreSQL for user management
- **Frontend**: Vue.js with PrimeVue/Tailwind CSS

## Developers

Logchef is open source software. If you're interested in contributing:

1. Clone the repository
2. Set up your development environment with Go and Node.js
3. Run `make dev` to start the development server

## License

Logchef is licensed under the AGPLv3 License.
