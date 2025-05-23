---
title: User Management
description: Understanding Sources, Teams, and access control in LogChef
---

LogChef implements a team-based access control system that helps organize and secure your log data. This guide explains the core concepts of user management and how to effectively use them.

## Sources

A Source in LogChef represents a distinct log stream that maps directly to a table in ClickHouse. Think of Sources as individual channels of log data that you can query independently.

### Key Aspects of Sources

- Each Source corresponds to a specific ClickHouse table
- Sources can represent different applications, services, or environments
- Sources have their own schema and configuration
- Access to Sources is controlled through Team assignments

### Example Sources

```
app-production-logs   → Production application logs
nginx-access-logs     → Web server access logs
kubernetes-events     → Kubernetes cluster events
```

## Teams

Teams are the primary mechanism for managing access control in LogChef. They create logical groupings of users and determine which Sources they can access.

![Logchef Teams View](/screenshots/logchef_teams.png)

### How Teams Work

- Users are assigned to one or more Teams
- Sources are associated with specific Teams
- Users can only access Sources that belong to their Teams
- Teams help maintain data isolation and security

![Logchef Users View](/screenshots/logchef_users.png)

### Example Team Structure

```
Infrastructure Team   → Access to system logs, metrics
Application Team     → Access to application logs
Security Team        → Access to audit logs, security events
DevOps Team         → Access to deployment logs, monitoring
```

## Access Control Flow

1. When a user logs in, LogChef identifies their Team memberships
2. The UI only displays Sources associated with the user's Teams
3. All queries are automatically filtered based on Team permissions
4. Users cannot access or query Sources outside their Team's scope

## Best Practices

1. **Source Organization**

   - Use clear, consistent naming for Sources
   - Document the schema and purpose of each Source
   - Consider environment and application boundaries when creating Sources

2. **Team Management**

   - Create Teams based on functional responsibilities
   - Regularly audit Team memberships
   - Follow the principle of least privilege
   - Consider creating read-only Teams for auditors or external users

3. **Access Patterns**
   - Group related Sources under the same Team
   - Use separate Sources for sensitive data
   - Consider creating cross-functional Teams for specific projects
