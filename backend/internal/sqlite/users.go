package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"backend-v2/pkg/models"
)

// User methods

// CreateUser creates a new user
func (db *DB) CreateUser(ctx context.Context, user *models.User) error {
	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now
	if user.Status == "" {
		user.Status = models.UserStatusActive
	}

	result, err := db.queries.CreateUser.ExecContext(ctx,
		user.ID, user.Email, user.FullName, user.Role, user.Status,
		user.LastLoginAt, user.CreatedAt, user.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("expected 1 row to be affected, got %d", rows)
	}

	return nil
}

// GetUser gets a user by ID
func (db *DB) GetUser(ctx context.Context, id string) (*models.User, error) {
	var user models.User
	err := db.queries.GetUser.GetContext(ctx, &user, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return &user, nil
}

// GetUserByEmail gets a user by email
func (db *DB) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	var user models.User
	err := db.queries.GetUserByEmail.GetContext(ctx, &user, email)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user by email: %w", err)
	}
	return &user, nil
}

// UpdateUser updates a user
func (db *DB) UpdateUser(ctx context.Context, user *models.User) error {
	user.UpdatedAt = time.Now()
	result, err := db.queries.UpdateUser.ExecContext(ctx,
		user.Email, user.FullName, user.Role, user.Status,
		user.LastLoginAt, user.LastActiveAt, user.UpdatedAt, user.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}

// ListUsers lists all users
func (db *DB) ListUsers(ctx context.Context) ([]*models.User, error) {
	var users []*models.User
	err := db.queries.ListUsers.SelectContext(ctx, &users)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	return users, nil
}

// CountAdminUsers counts the number of admin users
func (db *DB) CountAdminUsers(ctx context.Context) (int, error) {
	var count int
	err := db.queries.CountAdminUsers.GetContext(ctx, &count, models.UserRoleAdmin, models.UserStatusActive)
	if err != nil {
		return 0, fmt.Errorf("failed to count admin users: %w", err)
	}
	return count, nil
}

// DeleteUser deletes a user by ID
func (db *DB) DeleteUser(ctx context.Context, id string) error {
	result, err := db.queries.DeleteUser.ExecContext(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("expected 1 row to be affected, got %d", rows)
	}

	return nil
}
