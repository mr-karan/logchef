package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// User methods

// CreateUser inserts a new user record into the database.
// It sets default status if necessary and handles potential unique email constraint errors.
// Populates the user ID and timestamps on the input model upon success.
func (db *DB) CreateUser(ctx context.Context, user *models.User) error {

	// Default status if not provided by caller.
	if user.Status == "" {
		user.Status = models.UserStatusActive
	}

	// Map domain model fields to sqlc parameters, handling nullable times.
	var lastLoginTime sql.NullTime
	if user.LastLoginAt != nil {
		lastLoginTime = sql.NullTime{Time: *user.LastLoginAt, Valid: true}
	}
	// Note: lastActiveTime is not typically set on creation.

	params := sqlc.CreateUserParams{
		Email:       user.Email,
		FullName:    user.FullName,
		Role:        string(user.Role),
		Status:      string(user.Status),
		LastLoginAt: lastLoginTime,
	}

	id, err := db.queries.CreateUser(ctx, params)
	if err != nil {
		if isUniqueConstraintSQLiteError(err, "users", "email") {
			return handleUniqueConstraintError(err, "users", "email", user.Email)
		}
		db.log.Error("failed to create user record in db", "error", err, "email", user.Email)
		return fmt.Errorf("failed to create user: %w", err)
	}

	// Set the auto-generated ID.
	user.ID = models.UserID(id)

	// Fetch the created record to get accurate timestamps.
	userRow, err := db.queries.GetUser(ctx, id) // Use the generated ID.
	if err != nil {
		db.log.Error("failed to get newly created user record", "error", err, "assigned_id", id)
		// Continue successfully, but timestamps might be inaccurate.
		return nil
	}

	// Update input model with DB-generated timestamps.
	newUser := mapUserRowToModel(userRow) // mapUserRowToModel defined in utility.go
	user.CreatedAt = newUser.CreatedAt
	user.UpdatedAt = newUser.UpdatedAt

	return nil
}

// GetUser retrieves a single user by their ID.
// Returns core.ErrUserNotFound if not found.
func (db *DB) GetUser(ctx context.Context, id models.UserID) (*models.User, error) {

	userRow, err := db.queries.GetUser(ctx, int64(id))
	if err != nil {
		return nil, handleNotFoundError(err, fmt.Sprintf("getting user id %d", id))
	}

	// Map and return domain model.
	return mapUserRowToModel(userRow), nil
}

// GetUserByEmail retrieves a single user by their email address.
// Returns core.ErrUserNotFound if not found.
func (db *DB) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {

	userRow, err := db.queries.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, handleNotFoundError(err, fmt.Sprintf("getting user email %s", email))
	}

	// Map and return domain model.
	return mapUserRowToModel(userRow), nil
}

// UpdateUser updates an existing user record.
// Automatically sets the updated_at timestamp.
func (db *DB) UpdateUser(ctx context.Context, user *models.User) error {

	// Map domain model fields to sqlc parameters, handling nullable times.
	var lastLoginTime, lastActiveTime sql.NullTime
	if user.LastLoginAt != nil {
		lastLoginTime = sql.NullTime{Time: *user.LastLoginAt, Valid: true}
	}
	if user.LastActiveAt != nil {
		lastActiveTime = sql.NullTime{Time: *user.LastActiveAt, Valid: true}
	}

	params := sqlc.UpdateUserParams{
		Email:        user.Email,
		FullName:     user.FullName,
		Role:         string(user.Role),
		Status:       string(user.Status),
		LastLoginAt:  lastLoginTime,
		LastActiveAt: lastActiveTime,
		UpdatedAt:    user.UpdatedAt, // Pass the timestamp explicitly.
		ID:           int64(user.ID),
	}

	err := db.queries.UpdateUser(ctx, params)
	if err != nil {
		// Check for potential unique constraint violation on email if it was updated.
		if IsUniqueConstraintError(err) && strings.Contains(err.Error(), "email") {
			return handleUniqueConstraintError(err, "users", "email", user.Email)
		}
		db.log.Error("failed to update user record in db", "error", err, "user_id", user.ID)
		return fmt.Errorf("failed to update user: %w", err)
	}

	return nil
}

// ListUsers retrieves all user records, ordered by creation date.
func (db *DB) ListUsers(ctx context.Context) ([]*models.User, error) {

	userRows, err := db.queries.ListUsers(ctx)
	if err != nil {
		db.log.Error("failed to list users from db", "error", err)
		return nil, fmt.Errorf("failed to list users: %w", err)
	}

	// Map each row to the domain model.
	users := make([]*models.User, 0, len(userRows))
	for i := range userRows {
		mappedUser := mapUserRowToModel(userRows[i])
		if mappedUser != nil {
			users = append(users, mappedUser)
		}
	}

	return users, nil
}

// CountAdminUsers counts active users with the admin role.
func (db *DB) CountAdminUsers(ctx context.Context) (int, error) {

	count, err := db.queries.CountAdminUsers(ctx, sqlc.CountAdminUsersParams{
		Role:   string(models.UserRoleAdmin),
		Status: string(models.UserStatusActive),
	})
	if err != nil {
		db.log.Error("failed to count admin users in db", "error", err)
		return 0, fmt.Errorf("failed to count admin users: %w", err)
	}

	return int(count), nil
}

// DeleteUser removes a user record by ID.
func (db *DB) DeleteUser(ctx context.Context, id models.UserID) error {

	err := db.queries.DeleteUser(ctx, int64(id))
	if err != nil {
		db.log.Error("failed to delete user record from db", "error", err, "user_id", id)
		// Consider if specific error mapping (e.g., for foreign key constraints) is needed.
		return fmt.Errorf("failed to delete user: %w", err)
	}

	return nil
}

// Helper function to map sqlc.User to models.User
func mapUserRowToModel(row sqlc.User) *models.User {
	var lastLoginAt, lastActiveAt *time.Time
	if row.LastLoginAt.Valid {
		lastLoginAt = &row.LastLoginAt.Time
	}
	if row.LastActiveAt.Valid {
		lastActiveAt = &row.LastActiveAt.Time
	}

	return &models.User{
		ID:           models.UserID(row.ID),
		Email:        row.Email,
		FullName:     row.FullName,
		Role:         models.UserRole(row.Role),
		Status:       models.UserStatus(row.Status),
		LastLoginAt:  lastLoginAt,
		LastActiveAt: lastActiveAt,
		Timestamps: models.Timestamps{
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
	}
}
