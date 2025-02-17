# User Management Design

## Overview

This document outlines the user management features for LogChef, including user creation, team management, and access control.

## User Model

```typescript
interface User {
  id: string; // UUID
  email: string; // Email address (unique)
  fullName: string; // Display name
  role: "admin" | "member"; // User role
  status: "active" | "inactive"; // User status
  lastLoginAt?: Date; // Last successful login
  createdAt: Date; // When user was created
  updatedAt: Date; // Last update timestamp
}
```

## Team Model

```typescript
interface Team {
  id: string; // UUID
  name: string; // Team name
  description: string; // Team description
  createdBy: string; // User ID who created the team
  createdAt: Date; // When team was created
  updatedAt: Date; // Last update timestamp
}

interface TeamMember {
  teamId: string; // Team UUID
  userId: string; // User UUID
  role: "owner" | "member"; // Role in team
  addedAt: Date; // When user was added to team
}
```

## Authentication Flow

1. User signs up/logs in via OIDC (Dex)
2. Backend receives OIDC callback with user info
3. If email exists in our DB:
   - Validate email matches OIDC email
   - Check if user is active
   - If inactive, deny access
   - If active, proceed with login
4. If email doesn't exist:
   - Deny access if user creation is admin-only
   - Create user with default member role if allowed

## API Endpoints

### User Management (Admin Only)

```
POST   /api/v1/users              Create new user
GET    /api/v1/users              List all users
GET    /api/v1/users/:id          Get user details
PUT    /api/v1/users/:id          Update user
PUT    /api/v1/users/:id/status   Update user status
DELETE /api/v1/users/:id          Delete user (soft delete)
```

### Team Management (Admin Only)

```
POST   /api/v1/teams              Create new team
GET    /api/v1/teams              List all teams
GET    /api/v1/teams/:id          Get team details
PUT    /api/v1/teams/:id          Update team
DELETE /api/v1/teams/:id          Delete team

POST   /api/v1/teams/:id/members  Add team member
GET    /api/v1/teams/:id/members  List team members
DELETE /api/v1/teams/:id/members/:userId  Remove team member
```

## Implementation Roadmap

### Phase 1: Core User Management

1. Backend:

   - [ ] Update User model with status field
   - [ ] Add admin middleware for protected routes
   - [ ] Implement user CRUD endpoints
   - [ ] Update OIDC flow to check user status
   - [ ] Add user status validation in auth middleware

2. Frontend:
   - [ ] Create Users page with list/grid view
   - [ ] Implement user creation form
   - [ ] Add user status toggle
   - [ ] Add role selection for admins
   - [ ] Show last login and status in user list

### Phase 2: Team Management

1. Backend:

   - [ ] Implement Team and TeamMember models
   - [ ] Add team CRUD endpoints
   - [ ] Add team member management endpoints
   - [ ] Add team-related permissions

2. Frontend:
   - [ ] Create Teams page with list view
   - [ ] Implement team creation form
   - [ ] Add team member management UI
   - [ ] Show team members and roles

### Phase 3: UI/UX Improvements

1. Frontend:
   - [ ] Add bulk actions for users
   - [ ] Implement search and filters
   - [ ] Add sorting options
   - [ ] Create user activity timeline
   - [ ] Add team member invitation flow

## UI Components Needed

1. User Management:

   - UserList component
   - UserForm component
   - UserStatus component
   - UserRoleSelect component

2. Team Management:
   - TeamList component
   - TeamForm component
   - TeamMemberList component
   - TeamMemberForm component

## Security Considerations

1. All user management routes require admin role
2. User status changes must be logged
3. Cannot delete/deactivate last admin user
4. Team ownership transfers must be explicit
5. Audit logging for all user/team changes

## Database Changes

1. Add to users table:

   - status field (ENUM: active, inactive)
   - updated_at timestamp
   - created_at timestamp

2. New teams table:

   - id (UUID)
   - name (VARCHAR)
   - description (TEXT)
   - created_by (UUID, FK to users)
   - created_at timestamp
   - updated_at timestamp

3. New team_members table:
   - team_id (UUID, FK to teams)
   - user_id (UUID, FK to users)
   - role (VARCHAR)
   - added_at timestamp
