package sqlite

import (
	"context"
	"fmt"
	"time"

	"backend-v2/pkg/models"
)

// User methods

// CreateUser creates a new user in the database
func (d *DB) CreateUser(ctx context.Context, user *models.User) error {
	now := time.Now()
	result, err := d.queries.CreateUser.ExecContext(ctx,
		user.ID,
		user.Email,
		user.FullName,
		user.Role,
		now, // created_at
		now, // updated_at
	)
	if err != nil {
		if isUniqueConstraintError(err, "users", "email") {
			return fmt.Errorf("user with email %s already exists", user.Email)
		}
		return fmt.Errorf("error creating user: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("error getting rows affected: %w", err)
	}

	if rowsAffected != 1 {
		return fmt.Errorf("expected 1 row to be affected, got %d", rowsAffected)
	}

	return nil
}

// GetUser retrieves a user by ID
func (d *DB) GetUser(ctx context.Context, userID string) (*models.User, error) {
	var user models.User
	err := d.queries.GetUser.GetContext(ctx, &user, userID)
	if err != nil {
		return nil, fmt.Errorf("error getting user: %w", err)
	}
	return &user, nil
}

// GetUserByEmail retrieves a user by email
func (d *DB) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	var user models.User
	err := d.queries.GetUserByEmail.GetContext(ctx, &user, email)
	if err != nil {
		return nil, fmt.Errorf("error getting user by email: %w", err)
	}
	return &user, nil
}

// UpdateUser updates an existing user
func (d *DB) UpdateUser(ctx context.Context, user *models.User) error {
	result, err := d.queries.UpdateUser.ExecContext(ctx,
		user.Email,
		user.FullName,
		user.Role,
		user.LastLoginAt,
		user.LastActiveAt,
		time.Now(),
		user.ID,
	)
	if err != nil {
		if isUniqueConstraintError(err, "users", "email") {
			return fmt.Errorf("user with email %s already exists", user.Email)
		}
		return fmt.Errorf("error updating user: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("error getting rows affected: %w", err)
	}

	if rowsAffected != 1 {
		return fmt.Errorf("expected 1 row to be affected, got %d", rowsAffected)
	}

	return nil
}

// ListUsers returns all users
func (d *DB) ListUsers(ctx context.Context) ([]*models.User, error) {
	var users []*models.User
	err := d.queries.ListUsers.SelectContext(ctx, &users)
	if err != nil {
		return nil, fmt.Errorf("error listing users: %w", err)
	}
	return users, nil
}
