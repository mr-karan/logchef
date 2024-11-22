## Backend

- Clickhouse seed DB. Use gomigrate to manage it.
- Add migrations data in Psql
- Write hello world?
- Build a specific DSL
- Build a frontend
- Build a CH proxy layer for connection management/users
- 2 different schemas
    - HTTP
    - App
    - Possibly support custom schema + custom SQL
- Try out Grafana
- ksuid for UUID
- PrimeVue seems to be a good choice. Couple with Tailwind and off you go. Also explore shadcn/vue but you’ll need to do a lot of work.
- Start with API first approach
    - OAuth2
    - Basic Auth
    - Grafana article on how to write http services
    - Checkout fibre in go. Or echo
- Design a Vector config file which will be the http server
    - The user can also give a custom file, the Clickhouse spec can be shared with the user.
    - Ingestion is all handled with Vector sink file
- Show examples to ingest with `curl`
- UI Pages
    - User
        - Signup
        - Login
        - Logout
        - Profile
    - Dashboard
        - Saved reports
    - Management
        - Audit Trail
        - Add a Project (Clickhouse connection string)
        - Add users to the project
    - Alerts
- Need PostgreSQL/sqlite for managing users etc
    - Checkout sql-c
- HTTP
    - go-fibre/echo

## Cursor Prompts

### Overview
```
Okay, I am going to give some more context on what I need

- I am planning to use Vector as the agent to collect the logs. The user will be
required to run Vector to send logs to Clickhouse DB directly.
- I am going to send you all the relevant blog posts on how people use Clickhouse
for logs. You have to review them and come up with a schema for the logs.
- After we've setup Clickhouse, we should write Go program using echo framework
and start writing HTTP handlers to collect logs.
- The idea is that we will have a "Sources" tab in the UI where users can create new sources.
- Each source means a Clickhouse Table. So basically we will have a table for each source.
- We will have a "Queries" tab in the UI where users can run SQL queries on the data.
- In future we will implement RBAC to allow users to query only their own data.
- The schema should be OTEL compatible so we can have a generic yet performant
logging solution.

```

## Resources
https://docs.permify.co/permify-overview/intro


## Logchef - Product Requirements Document

Version 1.0 | Created: November 2024



### 1. Product Overview

Logchef is a modern log management and analysis platform designed for developers and teams. It combines the power of Clickhouse for high-performance log storage with an intuitive Vue.js frontend, offering powerful querying capabilities and visualization tools.



#### 1.1 Mission Statement

To provide a fast, user-friendly, and powerful log management solution that makes log analysis accessible and efficient for development teams.



#### 1.2 Core Technologies

- Frontend: Vue.js with Shadcn components and Tailwind CSS

- Backend: Golang

- Database: Clickhouse

- API Framework: Echo v5



### 2. User Management & Authentication



#### 2.1 User System

- Multi-tenant user system with roles and permissions

- User roles:

- Admin: Full system access

- Operator: Can view and query logs, create alerts

- Viewer: Can only view logs and dashboards

- User groups for organizing team members

- User profile management



#### 2.2 Authentication & Authorization

- Secure authentication system

- JWT-based session management

- Role-based access control (RBAC)

- API key management for programmatic access



### 3. Namespace Management



#### 3.1 Namespace Creation

- HTTP API endpoint for namespace creation

- Each namespace maps to a Clickhouse table

- Configurable retention periods per namespace

- Namespace-level access control



#### 3.2 Schema Design




### 4. Log Ingestion & Storage



#### 4.1 Log Ingestion APIs

- Raw log endpoint (/v1/logs/raw)

- Structured JSON endpoint (/v1/logs/json)

- Batch ingestion support

- Circuit breaker implementation for reliability



#### 4.2 Performance Requirements

- Support for >10,000 log entries per second

- Sub-second query response for most queries

- Efficient compression and storage

- Automatic partitioning and cleanup



### 5. Query Interface



#### 5.1 SQL Query Interface

- Direct SQL query support

- Query history

- Query templates

- Query sharing between team members



#### 5.2 Visual Query Builder

- Custom query language similar to LogQL

- Grammar specification for query language

- Components:

- Label matchers

- Time range selector

- Filter expressions

- Aggregation operations

- Translation layer to convert custom queries to SQL



### 6. Visualization & UI Features



#### 6.1 Dashboard

- Real-time log stream view

- Log volume histogram

- Quick filters

- Saved searches

- Dark/light theme support



#### 6.2 Charts & Graphs

- Time series visualization

- Log volume bar charts

- Distribution graphs

- Custom dashboard creation

- Export capabilities



#### 6.3 Alert Management

- Alert rule creation interface

- Condition-based alerts

- Threshold alerts

- Pattern matching

- Absence alerts

- Notification channels:

- Email

- Webhook

- (Future: Slack, Discord)



#### 6.4 Export Features

- CSV export of log data

- Dashboard export/import

- Query result sharing



### 7. Technical Requirements



#### 7.1 Performance Metrics

- Page load time < 2 seconds

- Query response time < 1 second for most queries

- Support for concurrent users

- Efficient memory usage



#### 7.2 Security Requirements

- TLS encryption

- Input sanitization

- Rate limiting

- Audit logging



### 8. Development Phases



#### Phase 1: Core Infrastructure

- [ ] Basic user authentication

- [ ] Namespace management

- [ ] Log ingestion APIs

- [ ] Basic query interface



#### Phase 2: Query & Visualization

- [ ] Advanced query builder

- [ ] Chart components

- [ ] Dashboard creation

- [ ] CSV export



#### Phase 3: Advanced Features

- [ ] Alert system

- [ ] User groups & permissions

- [ ] Advanced visualizations

- [ ] Query optimizations



### 9. Future Considerations

- Distributed deployment support

- Kubernetes integration

- Log forwarding agents

- Machine learning for log analysis

- Real-time streaming analytics