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
- **API Documentation** - Complete Swagger/OpenAPI documentation for all endpoints

## Project Structure

- `/cmd` - Contains the main server executable
- `/internal` - Internal Go packages
- `/pkg` - Public Go packages 
- `/docs` - Documentation site built with Astro and Starlight
- `/website` - Marketing landing page
- `/frontend` - Vue.js frontend application

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

## Building the Website and Docs

### Documentation

The documentation is built using Astro with Starlight:

```bash
cd docs
npm install
npm run build
```

Output will be in `/docs/dist/` directory.

### Landing Page

The landing page is a simple HTML/CSS site in the `/website` directory.

### Deployment Strategy

For deployment, copy both into your web server:

```bash
# Example deployment script
cp -r website/* /var/www/logchef/
cp -r docs/dist/* /var/www/logchef/docs/
```

## API Documentation

Logchef provides comprehensive API documentation using Swagger/OpenAPI.

Once the server is running, you can access the Swagger UI at:

```
http://localhost:8125/swagger/
```

The API documentation includes all available endpoints, authentication requirements, request/response schemas, and example usage.

If you're developing against the API, this documentation is the most up-to-date reference.

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

### Generating API Documentation

To update the Swagger/OpenAPI documentation after making changes to the API:

```shell
just swagger-gen
```

This will parse the API annotations in the code and regenerate the Swagger documentation.

## License

Logchef is licensed under the AGPLv3 License.