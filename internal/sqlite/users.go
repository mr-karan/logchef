package sqlite

import (
	"context"
	"fmt"
	"time"

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

	result, err := db.queries.CreateUser.ExecContext(ctx,
		user.Email, user.FullName, user.Role, user.Status, user.LastLoginAt,
	)
	if err != nil {
		if isUniqueConstraintError(err, "users", "email") {
			return fmt.Errorf("user with email %s already exists", user.Email)
		}
		db.log.Error("failed to create user", "error", err, "user_id", user.ID)
		return fmt.Errorf("failed to create user: %w", err)
	}

	if err := checkRowsAffected(result, "create user"); err != nil {
		db.log.Error("unexpected rows affected", "error", err, "user_id", user.ID)
		return err
	}

	return nil
}

// GetUser gets a user by ID
func (db *DB) GetUser(ctx context.Context, id models.UserID) (*models.User, error) {
	db.log.Debug("getting user", "user_id", id)

	var user models.User
	err := db.queries.GetUser.GetContext(ctx, &user, id)
	if err != nil {
		return nil, handleNotFoundError(err, "failed to get user")
	}
	return &user, nil
}

// GetUserByEmail gets a user by email
func (db *DB) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	db.log.Debug("getting user by email", "email", email)

	var user models.User
	err := db.queries.GetUserByEmail.GetContext(ctx, &user, email)
	if err != nil {
		return nil, handleNotFoundError(err, "failed to get user by email")
	}
	return &user, nil
}

// UpdateUser updates a user
func (db *DB) UpdateUser(ctx context.Context, user *models.User) error {
	db.log.Debug("updating user", "user_id", user.ID, "email", user.Email)

	user.UpdatedAt = time.Now()
	result, err := db.queries.UpdateUser.ExecContext(ctx,
		user.Email, user.FullName, user.Role, user.Status,
		user.LastLoginAt, user.LastActiveAt, user.UpdatedAt, user.ID,
	)
	if err != nil {
		if isUniqueConstraintError(err, "users", "email") {
			return fmt.Errorf("user with email %s already exists", user.Email)
		}
		db.log.Error("failed to update user", "error", err, "user_id", user.ID)
		return fmt.Errorf("failed to update user: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		db.log.Error("failed to get rows affected", "error", err, "user_id", user.ID)
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		db.log.Warn("user not found", "user_id", user.ID)
		return fmt.Errorf("user not found")
	}

	return nil
}

// ListUsers lists all users
func (db *DB) ListUsers(ctx context.Context) ([]*models.User, error) {
	db.log.Debug("listing users")

	var users []*models.User
	err := db.queries.ListUsers.SelectContext(ctx, &users)
	if err != nil {
		db.log.Error("failed to list users", "error", err)
		return nil, fmt.Errorf("failed to list users: %w", err)
	}

	db.log.Debug("users listed", "count", len(users))
	return users, nil
}

// CountAdminUsers counts the number of admin users
func (db *DB) CountAdminUsers(ctx context.Context) (int, error) {
	db.log.Debug("counting admin users")

	var count int
	count64, err := db.queries.CountAdminUsers(ctx, sqlc.CountAdminUsersParams{
		Role:   string(models.UserRoleAdmin),
		Status: string(models.UserStatusActive),
	})
	count := int(count64)
		Role:   string(models.UserRoleAdmin),
		Status: string(models.UserStatusActive),
	})
	if err != nil {
		db.log.Error("failed to count admin users", "error", err)
		return 0, fmt.Errorf("failed to count admin users: %w", err)
	}

	db.log.Debug("admin users counted", "count", count)
	return count, nil
}

// DeleteUser deletes a user by ID
func (db *DB) DeleteUser(ctx context.Context, id models.UserID) error {
	db.log.Debug("deleting user", "user_id", id)

	result, err := db.queries.DeleteUser.ExecContext(ctx, id)
	if err != nil {
		db.log.Error("failed to delete user", "error", err, "user_id", id)
		return fmt.Errorf("failed to delete user: %w", err)
	}

	if err := checkRowsAffected(result, "delete user"); err != nil {
		db.log.Error("unexpected rows affected", "error", err, "user_id", id)
		return err
	}

	return nil
}
