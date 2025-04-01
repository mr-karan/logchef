package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mr-karan/logchef/internal/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// User methods

// CreateUser creates a new user
func (db *DB) CreateUser(ctx context.Context, user *models.User) error {
	db.log.Debug("creating user", "user_id", user.ID, "email", user.Email)

	// Set default status if not provided
	if user.Status == "" {
		user.Status = models.UserStatusActive
	}

	// Convert *time.Time to sql.NullTime
	var lastLoginTime sql.NullTime
	if user.LastLoginAt != nil {
		lastLoginTime = sql.NullTime{
			Time:  *user.LastLoginAt,
			Valid: true,
		}
	}

	id, err := db.queries.CreateUser(ctx, sqlc.CreateUserParams{
		Email:       user.Email,
		FullName:    user.FullName,
		Role:        string(user.Role),
		Status:      string(user.Status),
		LastLoginAt: lastLoginTime,
	})
	if err != nil {
		if isUniqueConstraintError(err, "users", "email") {
			return fmt.Errorf("user with email %s already exists", user.Email)
		}
		db.log.Error("failed to create user", "error", err, "user_id", user.ID)
		return fmt.Errorf("failed to create user: %w", err)
	}

	// Set the auto-generated ID
	user.ID = models.UserID(id)

	// Get the user to fetch the timestamps
	userRow, err := db.queries.GetUser(ctx, int64(id))
	if err != nil {
		db.log.Error("failed to get created user", "error", err)
		return fmt.Errorf("error getting created user: %w", err)
	}

	// Update the user with the database values
	newUser := mapUserRowToModel(userRow)
	user.CreatedAt = newUser.CreatedAt
	user.UpdatedAt = newUser.UpdatedAt

	return nil
}

// GetUser gets a user by ID
func (db *DB) GetUser(ctx context.Context, id models.UserID) (*models.User, error) {
	db.log.Debug("getting user", "user_id", id)

	userRow, err := db.queries.GetUser(ctx, int64(id))
	if err != nil {
		return nil, handleNotFoundError(err, "failed to get user")
	}

	user := mapUserRowToModel(userRow)
	return user, nil
}

// GetUserByEmail gets a user by email
func (db *DB) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	db.log.Debug("getting user by email", "email", email)

	userRow, err := db.queries.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, handleNotFoundError(err, "failed to get user by email")
	}

	user := mapUserRowToModel(userRow)
	return user, nil
}

// UpdateUser updates a user
func (db *DB) UpdateUser(ctx context.Context, user *models.User) error {
	db.log.Debug("updating user", "user_id", user.ID, "email", user.Email)

	user.UpdatedAt = time.Now()

	// Convert *time.Time fields to sql.NullTime
	var lastLoginTime, lastActiveTime sql.NullTime
	if user.LastLoginAt != nil {
		lastLoginTime = sql.NullTime{
			Time:  *user.LastLoginAt,
			Valid: true,
		}
	}
	if user.LastActiveAt != nil {
		lastActiveTime = sql.NullTime{
			Time:  *user.LastActiveAt,
			Valid: true,
		}
	}

	err := db.queries.UpdateUser(ctx, sqlc.UpdateUserParams{
		Email:        user.Email,
		FullName:     user.FullName,
		Role:         string(user.Role),
		Status:       string(user.Status),
		LastLoginAt:  lastLoginTime,
		LastActiveAt: lastActiveTime,
		UpdatedAt:    user.UpdatedAt,
		ID:           int64(user.ID),
	})
	if err != nil {
		if isUniqueConstraintError(err, "users", "email") {
			return fmt.Errorf("user with email %s already exists", user.Email)
		}
		db.log.Error("failed to update user", "error", err, "user_id", user.ID)
		return fmt.Errorf("failed to update user: %w", err)
	}

	return nil
}

// ListUsers lists all users
func (db *DB) ListUsers(ctx context.Context) ([]*models.User, error) {
	db.log.Debug("listing users")

	userRows, err := db.queries.ListUsers(ctx)
	if err != nil {
		db.log.Error("failed to list users", "error", err)
		return nil, fmt.Errorf("failed to list users: %w", err)
	}

	users := make([]*models.User, len(userRows))
	for i, row := range userRows {
		users[i] = mapUserRowToModel(row)
	}

	db.log.Debug("users listed", "count", len(users))
	return users, nil
}

// CountAdminUsers counts the number of admin users
func (db *DB) CountAdminUsers(ctx context.Context) (int, error) {
	db.log.Debug("counting admin users")

	count, err := db.queries.CountAdminUsers(ctx, sqlc.CountAdminUsersParams{
		Role:   string(models.UserRoleAdmin),
		Status: string(models.UserStatusActive),
	})
	if err != nil {
		db.log.Error("failed to count admin users", "error", err)
		return 0, fmt.Errorf("failed to count admin users: %w", err)
	}

	db.log.Debug("admin users counted", "count", count)
	return int(count), nil
}

// DeleteUser deletes a user by ID
func (db *DB) DeleteUser(ctx context.Context, id models.UserID) error {
	db.log.Debug("deleting user", "user_id", id)

	err := db.queries.DeleteUser(ctx, int64(id))
	if err != nil {
		db.log.Error("failed to delete user", "error", err, "user_id", id)
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
