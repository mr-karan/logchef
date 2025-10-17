---
title: Roadmap
description: Upcoming features and future plans for LogChef
---

## Recently Released

### Alerting System ✅

LogChef now includes a comprehensive alerting system integrated with Prometheus Alertmanager:

- ✅ SQL-based alert conditions with ClickHouse query support
- ✅ Alertmanager integration for battle-tested alert routing
- ✅ Multiple severity levels (info, warning, critical)
- ✅ Team and source-specific alerts
- ✅ Automatic retry logic with exponential backoff
- ✅ Delivery failure tracking and alert history
- ✅ Rich metadata including custom labels and annotations
- ✅ Configurable evaluation intervals and lookback windows

[Read the full alerting documentation →](/features/alerting)

---

## Upcoming Features

Here's what we're planning to add to LogChef in future releases:

## Integration Features

### HTTP API

- REST API for managing sources
- Query execution endpoints
- User and team management
- Authentication and access control
- Detailed request/response examples

### Client Libraries

- Go client library
- Python client library
- JavaScript/TypeScript client (for browser and Node.js)
- Type-safe API clients with query builders
- Authentication helpers

## Analytics Features

### Visualizations

- Time series analytics for error rates and metrics
- Interactive dashboards
- Rich chart types (line, bar, heat maps)
- Dynamic filtering and drill-down capabilities

## Get Involved

We welcome community feedback and contributions! If you're interested in any of these features:

1. Star our [GitHub repository](https://github.com/mr-karan/logchef)
2. Open issues with feature requests or suggestions
3. Join discussions about implementation details
