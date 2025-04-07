---
title: Architecture Diagram
description: Visual representation of LogChef's architecture and data flow
---

# Architecture Diagram

The following diagram illustrates the core data flow in LogChef:

```mermaid
graph LR
    User((User)) --> |Writes Query| UI[Web UI]
    UI --> |Query| Backend[Backend Service]
    Backend --> |Metadata Lookup| SQLite[(SQLite)]
    Backend --> |Execute Query| CH[(ClickHouse)]
    CH --> |Results| Backend
    Backend --> |Render| UI

    Vector[Vector] --> |Ingest Logs| CH

    style User fill:#f9f9f9,stroke:#333,stroke-width:2px
    style Backend fill:#e6f3ff,stroke:#333,stroke-width:2px
    style CH fill:#f0fff0,stroke:#333,stroke-width:2px
```

## Flow Description

1. **Query Flow**

   - User writes a log query in the Web UI
   - Backend validates and processes the query
   - Results are fetched from ClickHouse and rendered in UI

2. **Data Storage**
   - ClickHouse: Stores and indexes log data
   - SQLite: Manages user permissions and metadata
   - Vector: Handles log ingestion

This simple architecture ensures fast log querying and efficient data management while maintaining a clean user experience.
