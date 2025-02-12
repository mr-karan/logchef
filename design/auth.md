# Product Requirements Document (PRD)

**Project Name:** Log Analytics App – Auth & Authorization Module
**Tech Stack:**

- **Frontend:** Vue.js
- **Backend:** Go
- **Database:** SQLite
- **OIDC Library:** [github.com/coreos/go-oidc/v3/oidc](https://github.com/coreos/go-oidc/v3/oidc)

---

## 1. Overview

This document specifies the design and implementation of an authentication and authorization module for a log analytics application. The module covers:

- User and group management
- Two distinct roles: **Member** and **Admin**
- Group membership management
- OIDC integration for external identity providers
- Hierarchical permission management for “Collections” (sub-units within Groups) where saved queries reside with granular Read/Write access.

The goal is to create a secure, maintainable, and extensible auth system that cleanly separates concerns between authentication (identity verification via OIDC) and authorization (role- and resource-based permissions).

---

## 2. Functional Requirements

### 2.1. User Management

- **User Attributes:**
  - Unique ID
  - Username (or email)
  - Full Name
  - Role (User or Admin)
  - OIDC-related fields (e.g., OIDC Subject/ID, provider details)
- **Capabilities:**
  - Registration (via OIDC login flow)
  - Profile viewing and editing (for non-sensitive fields)
- **Lifecycle:**
  - Creation (via OIDC sign-up/login process)
  - Update
  - Deletion (Admins only)

### 2.2. Group Management

- **Group Attributes:**
  - Group ID
  - Name and Description
- **Capabilities:**
  - Create, edit, delete groups (Admins can create and manage groups)
  - Add or remove users from groups
  - View group membership

### 2.3. Role Management

- **Roles:**
  - **Admin:** Full access to all aspects (user, group, collection management)
  - **Member:** Limited to viewing and interacting with resources as permitted by group membership and collection-level permissions

### 2.4. Collection & Saved Query Management

- **Collections:**
  - A “Collection” is a sub-resource within a Group.
  - **Attributes:** Collection ID, Group ID (parent), Name, Description.
- **Permissions on Collections:**
  - **Read:** Ability to view saved queries
  - **Write:** Ability to create, modify, and delete saved queries
  - Permissions are granted per user at the collection level.
- **Saved Queries:**
  - **Attributes:** Query ID, Collection ID (parent), Query name, Query content (the actual log query), metadata (timestamps, created_by)
  - Capabilities include creating, reading, updating, and deleting queries subject to the user’s permissions on the parent collection.

### 2.5. OIDC Integration

- **Library:** Use [github.com/coreos/go-oidc/v3/oidc](https://github.com/coreos/go-oidc/v3/oidc)
- **Flow:**
  - When a user attempts to log in, they are redirected to an OIDC provider (e.g., Google, GitHub, etc.).
  - The backend validates the returned token and extracts user information.
  - On successful validation, a local user record is created or updated.
- **Session Management:**
  - Use session cookies (issued by the backend after OIDC verification) to maintain session state.

---

## 3. Non-Functional Requirements

- **Security:**
  - All endpoints must enforce HTTPS (except for the OIDC provider callback).
  - Sensitive data (tokens, passwords if any, etc.) should be securely stored.
  - Use industry-standard encryption for tokens and secure handling of user sessions.
- **Performance:**
  - Lightweight SQLite database for quick read/write operations.
- **Extensibility:**
  - Modular design to add new roles or permissions if needed.
  - Easily extend OIDC providers if business needs change.
- **Maintainability:**
  - Code separation between authentication logic (OIDC handling), business logic (user/group management), and API routes.
- **Scalability:**
  - While SQLite is used initially, design data models and interfaces so that migrating to a more scalable DBMS in the future is feasible.

---

## 4. Technical Architecture

### 4.1. High-Level Architecture Diagram

```plaintext
                +--------------------+
                |    Vue Frontend    |
                | (User Interfaces)  |
                +---------+----------+
                          |
                          | RESTful API calls (HTTPS)
                          v
                +--------------------+
                |      Go Backend    |
                |  - API Endpoints   |
                |  - Auth Middleware |
                |  - Business Logic  |
                +---------+----------+
                          |
                          | Database driver (SQLite)
                          v
                +--------------------+
                |    SQLite DB     |
                | - Users          |
                | - Groups         |
                | - Collections    |
                | - Saved Queries  |
                | - Permissions    |
                +--------------------+
```

### 4.2. Component Breakdown

- **Frontend (Vue):**

  - **Pages/Components:** Login page, Dashboard, Group Management UI, Collection & Saved Query editor.
  - **Responsibilities:**
    - Handle user interactions.
    - Redirect to OIDC provider for authentication.
    - Manage JWT/session token after login.
    - Call backend REST API endpoints to fetch and manipulate data.

- **Backend (Go):**

  - **Modules/Packages:**
    - **OIDC Module:**
      - Integrates with the [coreos/go-oidc](https://github.com/coreos/go-oidc) library.
      - Provides endpoints for login and callback.
    - **Auth Middleware:**
      - Validates session cookies on protected endpoints.
      - Extracts user context (role, group membership).
    - **API Handlers:**
      - **User Handlers:** CRUD for user data.
      - **Group Handlers:** Endpoints for group creation, membership management.
      - **Collection Handlers:** Endpoints for creating, updating, and managing collections.
      - **Saved Query Handlers:** Endpoints for managing saved queries.
    - **Database Layer:**
      - SQLite connection and ORM/SQL helpers.
      - Data models for Users, Groups, Collections, Saved Queries, and Permissions.
  - **Security Considerations:**
    - Input validation and sanitization.
    - Rate limiting and logging of auth-related events.

- **Database (SQLite):**
  - **Schema Design:** Tables for Users, Groups, GroupMembership, Collections, CollectionPermissions, and SavedQueries.
  - **Migration Scripts:** For schema creation and versioning.

---

## 5. Data Model & Database Schema

### 5.1. Users Table

| Column       | Type         | Notes                                    |
| ------------ | ------------ | ---------------------------------------- |
| id           | INTEGER (PK) | Auto-increment                           |
| oidc_subject | TEXT         | Unique identifier from the OIDC provider |
| email        | TEXT         | Unique email address                     |
| full_name    | TEXT         | User’s full name                         |
| role         | TEXT         | Enum: 'User', 'Admin'                    |
| created_at   | DATETIME     | Timestamp                                |
| updated_at   | DATETIME     | Timestamp                                |

### 5.2. Groups Table

| Column      | Type         | Notes                |
| ----------- | ------------ | -------------------- |
| id          | INTEGER (PK) | Auto-increment       |
| name        | TEXT         | Group name           |
| description | TEXT         | Optional description |
| created_at  | DATETIME     | Timestamp            |

### 5.3. GroupMembership Table

| Column    | Type         | Notes                                                                        |
| --------- | ------------ | ---------------------------------------------------------------------------- |
| id        | INTEGER (PK) | Auto-increment                                                               |
| user_id   | INTEGER      | Foreign key to Users.id                                                      |
| group_id  | INTEGER      | Foreign key to Groups.id                                                     |
| role      | TEXT         | (Optional: if you wish to allow per-group roles; otherwise, use global role) |
| joined_at | DATETIME     | Timestamp                                                                    |

### 5.4. Collections Table

| Column      | Type         | Notes                    |
| ----------- | ------------ | ------------------------ |
| id          | INTEGER (PK) | Auto-increment           |
| group_id    | INTEGER      | Foreign key to Groups.id |
| name        | TEXT         | Collection name          |
| description | TEXT         | Optional description     |
| created_at  | DATETIME     | Timestamp                |

### 5.5. CollectionPermissions Table

| Column        | Type         | Notes                         |
| ------------- | ------------ | ----------------------------- |
| id            | INTEGER (PK) | Auto-increment                |
| collection_id | INTEGER      | Foreign key to Collections.id |
| user_id       | INTEGER      | Foreign key to Users.id       |
| permission    | TEXT         | Enum: 'read', 'write'         |
| granted_at    | DATETIME     | Timestamp                     |

### 5.6. SavedQueries Table

| Column        | Type         | Notes                         |
| ------------- | ------------ | ----------------------------- |
| id            | INTEGER (PK) | Auto-increment                |
| collection_id | INTEGER      | Foreign key to Collections.id |
| name          | TEXT         | Query name                    |
| query_content | TEXT         | The actual query string       |
| created_by    | INTEGER      | User ID who created the query |
| created_at    | DATETIME     | Timestamp                     |
| updated_at    | DATETIME     | Timestamp                     |

---

## 6. API Endpoints & Flows

### 6.1. Authentication Endpoints

- **GET /login**

  - **Purpose:** Redirect the user to the OIDC provider.
  - **Flow:**
    1. User clicks “Login” on the frontend.
    2. Frontend redirects the user to `/login` on the backend.
    3. Backend constructs an OIDC authentication URL (with appropriate scopes and state) and redirects the user.

- **GET /auth/callback**
  - **Purpose:** Handle the OIDC provider callback.
  - **Flow:**
    1. OIDC provider redirects back with an authorization code.
    2. Backend uses the OIDC library to verify the code and fetch tokens.
    3. Backend extracts user information and checks/creates a user record in SQLite.
    4. Issue a JWT or session cookie and redirect the user to the dashboard.

### 6.2. User Endpoints

- **GET /api/users**
  - Returns a list of users (Admins only).
- **GET /api/users/{id}**
  - Returns details of a specific user.
- **PUT /api/users/{id}**
  - Update user information (Admins or the user themself for non-sensitive fields).
- **DELETE /api/users/{id}**
  - Delete a user (Admins only).

### 6.3. Group Endpoints

- **GET /api/groups**
  - List all groups (filtered by user membership if applicable).
- **POST /api/groups**
  - Create a new group (Admins only).
- **PUT /api/groups/{id}**
  - Update group details (Admins only).
- **DELETE /api/groups/{id}**
  - Delete a group (Admins only).
- **POST /api/groups/{id}/members**
  - Add a user to the group.
- **DELETE /api/groups/{id}/members/{user_id}**
  - Remove a user from the group.

### 6.4. Collection Endpoints

- **GET /api/groups/{group_id}/collections**
  - List collections within a group (accessible to group members).
- **POST /api/groups/{group_id}/collections**
  - Create a collection (Admins or authorized users within the group).
- **PUT /api/collections/{collection_id}**
  - Update collection details.
- **DELETE /api/collections/{collection_id}**
  - Delete a collection.

### 6.5. Collection Permissions Endpoints

- **GET /api/collections/{collection_id}/permissions**
  - List all permissions for a collection.
- **POST /api/collections/{collection_id}/permissions**
  - Grant a permission (read/write) to a user.
- **DELETE /api/collections/{collection_id}/permissions/{user_id}**
  - Revoke a user’s permission.

### 6.6. Saved Query Endpoints

- **GET /api/collections/{collection_id}/queries**
  - List saved queries in a collection.
- **POST /api/collections/{collection_id}/queries**
  - Create a new saved query (requires write permission).
- **PUT /api/queries/{query_id}**
  - Update an existing query (requires write permission).
- **DELETE /api/queries/{query_id}**
  - Delete a saved query (requires write permission).

> **Note:** All API endpoints (except the initial OIDC endpoints) should be protected with auth middleware that validates the user’s JWT/session token and enforces role/permission checks.

---

## 7. Implementation Plan

### 7.1. Phase 1: Setup & OIDC Integration

- **Tasks:**
  - Configure the Go project structure.
  - Integrate the OIDC library:
    - Create endpoints `/login` and `/auth/callback`.
    - Validate tokens from the OIDC provider.
  - Create a middleware for token verification.
  - Update user records on first login.

### 7.2. Phase 2: Core Auth and Group Management

- **Tasks:**
  - Design and implement the database schema.
  - Develop API endpoints for Users and Groups.
  - Implement business logic for group membership management.
  - Test endpoints using Postman or similar tools.

### 7.3. Phase 3: Collection and Saved Query Management

- **Tasks:**
  - Develop models and endpoints for Collections.
  - Implement the permissions system (read/write) on collections.
  - Develop endpoints for creating, updating, and deleting saved queries.
  - Ensure that authorization checks (using middleware and role/permission checks) are enforced.

### 7.4. Phase 4: Frontend Integration

- **Tasks:**
  - Develop Vue components for:
    - Login (which redirects to the OIDC login endpoint)
    - Dashboard displaying user’s groups and collections
    - UI for managing groups, collections, and saved queries
  - Integrate frontend API calls to the backend endpoints.
  - Handle token storage (e.g., in Vuex store) and renewals.

### 7.5. Phase 5: Testing, Documentation & Deployment

- **Tasks:**
  - Write unit tests and integration tests for backend logic.
  - Document API endpoints (possibly using Swagger/OpenAPI).
  - Create deployment scripts (Dockerfile, if needed) and run end-to-end testing.

---

## 8. Security & Best Practices

- **Token Security:**
  - Use HTTPS for all communications.
  - Store tokens securely (HTTP-only cookies or secure storage).
- **Input Validation:**
  - Validate and sanitize all incoming data on the backend.
- **Logging & Auditing:**
  - Log authentication events, especially failed logins and permission errors.
- **Error Handling:**
  - Provide clear error messages without exposing sensitive details.

---

## 9. Future Considerations

- **Extensible Roles:**
  - Although only two roles are defined now, design role-checking middleware in a way that additional roles can be added without major refactoring.
- **Database Scaling:**
  - While SQLite serves well in a small-scale or development environment, the data models should be abstract enough to allow a migration to PostgreSQL/MySQL if needed.
- **UI Enhancements:**
  - Enhance the Vue frontend with real-time notifications on permission changes or auth events.

---

## 10. Summary

This PRD details a complete and secure authentication/authorization system for your log analytics application, built using Vue (frontend) and Go (backend) with SQLite as the data store. With OIDC integration, group-based access controls, and fine-grained collection permissions, the system is designed to be both user-friendly and secure while providing a clear path for future enhancements.

By following this detailed specification, development can proceed in iterative phases, ensuring each component—from OIDC integration to API endpoint security—is robustly implemented and thoroughly tested before deployment.
