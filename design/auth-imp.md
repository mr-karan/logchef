# Authentication & Authorization Implementation Plan

## 1. Configuration Changes

Add to `config.toml`:

```toml
[oidc]
provider_url = "https://accounts.google.com"  # Example
client_id = ""
client_secret = ""
redirect_uri = "http://localhost:8080/api/v1/auth/callback"
scopes = ["openid", "email", "profile"]

[auth]
admin_email = "admin@example.com"
session_duration = "8h"
max_concurrent_sessions = 1
```

## 2. Database Schema

### Users Table

```sql
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    email TEXT UNIQUE NOT NULL,
    full_name TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('admin', 'member')),
    last_login_at DATETIME,
    last_active_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
```

### Sessions Table

```sql
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    expires_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
```

### Data Sources Table

```sql
CREATE TABLE IF NOT EXISTS data_sources (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    schema_type TEXT NOT NULL CHECK (schema_type IN ('managed', 'unmanaged')),
    host TEXT NOT NULL,
    username TEXT NOT NULL,
    password TEXT NOT NULL,
    database TEXT NOT NULL,
    table_name TEXT NOT NULL,
    created_by TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE CASCADE,
    UNIQUE(database, table_name)
);
```

### Teams Table

```sql
CREATE TABLE IF NOT EXISTS teams (
    id TEXT PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    description TEXT,
    created_by TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE CASCADE
);
```

### Team Members Table

```sql
CREATE TABLE IF NOT EXISTS team_members (
    team_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('admin', 'member')),
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (team_id, user_id),
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
```

### Spaces Table

```sql
CREATE TABLE IF NOT EXISTS spaces (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    created_by TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE CASCADE
);
```

### Space Data Sources Table

```sql
CREATE TABLE IF NOT EXISTS space_data_sources (
    space_id TEXT NOT NULL,
    data_source_id TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (space_id, data_source_id),
    FOREIGN KEY (space_id) REFERENCES spaces(id) ON DELETE CASCADE,
    FOREIGN KEY (data_source_id) REFERENCES data_sources(id) ON DELETE CASCADE
);
```

### Space Team Access Table

```sql
CREATE TABLE IF NOT EXISTS space_team_access (
    space_id TEXT NOT NULL,
    team_id TEXT NOT NULL,
    permission TEXT NOT NULL CHECK (permission IN ('read', 'write', 'admin')),
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (space_id, team_id),
    FOREIGN KEY (space_id) REFERENCES spaces(id) ON DELETE CASCADE,
    FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE
);
```

### Queries Table

```sql
CREATE TABLE IF NOT EXISTS queries (
    id TEXT PRIMARY KEY,
    space_id TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    query_content TEXT NOT NULL,
    created_by TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (space_id) REFERENCES spaces(id) ON DELETE CASCADE,
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE CASCADE
);
```

## 3. Error Codes

```go
// pkg/errors/auth.go
package errors

type AuthErrorCode string

const (
    // OIDC Errors
    ErrOIDCProviderNotConfigured AuthErrorCode = "OIDC_PROVIDER_NOT_CONFIGURED"
    ErrOIDCInvalidState          AuthErrorCode = "OIDC_INVALID_STATE"
    ErrOIDCInvalidToken          AuthErrorCode = "OIDC_INVALID_TOKEN"
    ErrOIDCEmailNotVerified      AuthErrorCode = "OIDC_EMAIL_NOT_VERIFIED"

    // Session Errors
    ErrSessionNotFound           AuthErrorCode = "SESSION_NOT_FOUND"
    ErrSessionExpired           AuthErrorCode = "SESSION_EXPIRED"
    ErrTooManySessions          AuthErrorCode = "TOO_MANY_SESSIONS"

    // User Errors
    ErrUserNotFound             AuthErrorCode = "USER_NOT_FOUND"
    ErrUserNotAuthorized        AuthErrorCode = "USER_NOT_AUTHORIZED"
    ErrAdminNotFound            AuthErrorCode = "ADMIN_NOT_FOUND"

    // Team Errors
    ErrTeamNotFound             AuthErrorCode = "TEAM_NOT_FOUND"
    ErrTeamAccessDenied         AuthErrorCode = "TEAM_ACCESS_DENIED"

    // Space Errors
    ErrSpaceNotFound            AuthErrorCode = "SPACE_NOT_FOUND"
    ErrSpaceAccessDenied        AuthErrorCode = "SPACE_ACCESS_DENIED"

    // Permission Errors
    ErrInsufficientPermissions  AuthErrorCode = "INSUFFICIENT_PERMISSIONS"
    ErrInvalidPermissionLevel   AuthErrorCode = "INVALID_PERMISSION_LEVEL"
)

type AuthError struct {
    Code    AuthErrorCode
    Message string
    Details map[string]interface{}
}
```

## 4. Implementation Phases

### Phase 1: Core Auth Infrastructure

1. Update SQLite schema with new tables
2. Implement OIDC provider integration
3. Implement session management
4. Create auth middleware for Fiber

### Phase 2: User & Team Management

1. Implement user creation/management
2. Implement team creation/management
3. Implement team membership management
4. Add user activity tracking

### Phase 3: Spaces & Data Sources

1. Implement data source CRUD operations
2. Implement space CRUD operations
3. Implement space-team access management
4. Implement space-data source management

### Phase 4: Queries & Permissions

1. Implement query CRUD operations
2. Implement permission validation
3. Add permission validation middleware
4. Implement query execution with permission checks

## 5. API Endpoints

### Authentication

```
POST   /api/v1/auth/login          # Initiate OIDC login
GET    /api/v1/auth/callback       # OIDC callback handler
POST   /api/v1/auth/logout         # Logout (revoke session)
GET    /api/v1/auth/session        # Get current session info
```

### User Management (Admin only)

```
GET    /api/v1/users               # List users
POST   /api/v1/users               # Create user
GET    /api/v1/users/:id          # Get user details
DELETE /api/v1/users/:id          # Delete user
```

### Team Management

```
GET    /api/v1/teams                    # List teams
POST   /api/v1/teams                    # Create team
GET    /api/v1/teams/:id               # Get team details
PUT    /api/v1/teams/:id               # Update team
DELETE /api/v1/teams/:id               # Delete team
GET    /api/v1/teams/:id/members       # List team members
POST   /api/v1/teams/:id/members       # Add team member
DELETE /api/v1/teams/:id/members/:uid  # Remove team member
PUT    /api/v1/teams/:id/members/:uid  # Update member role
```

### Data Sources (Admin only)

```
GET    /api/v1/datasources              # List data sources
POST   /api/v1/datasources              # Create data source
GET    /api/v1/datasources/:id         # Get data source details
PUT    /api/v1/datasources/:id         # Update data source
DELETE /api/v1/datasources/:id         # Delete data source
```

### Spaces

```
GET    /api/v1/spaces                    # List spaces
POST   /api/v1/spaces                    # Create space
GET    /api/v1/spaces/:id               # Get space details
PUT    /api/v1/spaces/:id               # Update space
DELETE /api/v1/spaces/:id               # Delete space
GET    /api/v1/spaces/:id/datasources   # List space data sources
POST   /api/v1/spaces/:id/datasources   # Add data source to space
DELETE /api/v1/spaces/:id/datasources/:dsid # Remove data source
GET    /api/v1/spaces/:id/teams         # List space team access
PUT    /api/v1/spaces/:id/teams/:tid    # Update team access
```

### Queries

```
GET    /api/v1/spaces/:id/queries       # List queries in space
POST   /api/v1/spaces/:id/queries       # Create query
GET    /api/v1/queries/:id              # Get query details
PUT    /api/v1/queries/:id              # Update query
DELETE /api/v1/queries/:id              # Delete query
POST   /api/v1/queries/:id/execute      # Execute query
```

## 6. Implementation Notes

### Session Management

- Use SQLite for session storage
- Implement periodic cleanup of expired sessions
- Track last_active_at on each authenticated request
- Implement session middleware for Fiber

### OIDC Integration

- Use coreos/go-oidc library
- Validate email domains
- Handle token refresh
- Store minimal user info (email, name)

### Permission Checking

- Implement middleware for role checking
- Check team membership and space access
- Cache permissions in session for performance
- Validate data source access for query execution

### Error Handling

- Use structured errors with codes
- Include request ID in responses
- Log detailed errors server-side

## 7. Migration Strategy

1. Add new tables without modifying existing ones
2. Add foreign key columns as nullable initially
3. Update application code to use new auth system
4. Add admin user check on startup

## 8. Testing Strategy

1. Unit tests for:

   - Permission validation
   - Session management
   - Error handling
   - Team access validation

2. Integration tests for:

   - OIDC flow
   - API endpoints
   - Permission enforcement
   - Query execution with permissions

3. Manual testing checklist for:
   - OIDC provider integration
   - Team management flows
   - Space access controls
   - Query permissions
