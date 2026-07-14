---
title: "Teams, Sources & Access Control in Logchef"
description: Understanding Sources, Teams, and access control in Logchef
---

Logchef implements a team-based access control system that helps organize and secure your log data. This guide explains the core concepts of user management and how to effectively use them.

## Authentication

Users sign in via **OIDC/SSO**, or via built-in **email+password** authentication
if your instance has it enabled. See [Local authentication](/getting-started/configuration/#local-authentication-run-without-oidc)
to run Logchef without an external identity provider. Both can be enabled at once.

By default an OIDC login only works for users that already exist in Logchef.
With [SSO auto-provisioning](/getting-started/configuration/#sso-auto-provisioning-jit-user-creation)
enabled, a first-time login from an allowed email domain creates the user
automatically as a regular member, optionally joining a set of default teams.

## Sources

A Source in Logchef represents a distinct datasource-backed log scope that users can query independently. Depending on the backend, a source may map to a ClickHouse table or to a VictoriaLogs connection plus optional tenant/scope boundaries.

### Key Aspects of Sources

- Each Source belongs to a specific datasource backend
- ClickHouse sources map to a specific `database.table`
- VictoriaLogs sources map to a base URL plus optional tenant and scope configuration
- Sources can represent different applications, services, or environments
- Sources have their own schema and configuration
- Access to Sources is controlled through Team assignments

### Example Sources

```
app-production-logs   → Production application logs in ClickHouse
nginx-access-logs     → Web server access logs in VictoriaLogs
kubernetes-events     → Cluster events scoped to a specific source
```

## Teams

Teams are the primary mechanism for managing access control in Logchef. They create logical groupings of users and determine which Sources they can access.

![Teams management page listing teams and their assigned sources](/screenshots/logchef_teams.png)

### How Teams Work

- Users are assigned to one or more Teams
- Sources are associated with specific Teams
- Users can only access Sources that belong to their Teams
- Teams help maintain data isolation and security

![Team member list showing each user's role within the team](/screenshots/logchef_users.png)

### Example Team Structure

```
Infrastructure Team   → Access to system logs, metrics
Application Team     → Access to application logs
Security Team        → Access to audit logs, security events
DevOps Team         → Access to deployment logs, monitoring
```

## Access Control Flow

1. When a user logs in, Logchef identifies their Team memberships
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

## Next steps

- Manage teams and sources as code with [Declarative Provisioning](/getting-started/provisioning)
- Set up non-login automation access with [Service Tokens](/features/service-tokens)
- Share queries across a team with [Collections](/features/collections)
