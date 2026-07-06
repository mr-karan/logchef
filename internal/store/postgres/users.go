package postgres

import (
	"context"
	"fmt"

	"github.com/mr-karan/logchef/internal/store/postgres/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

func userToModel(r sqlc.User) *models.User {
	return &models.User{
		ID:           models.UserID(r.ID),
		Email:        r.Email,
		FullName:     r.FullName,
		Role:         models.UserRole(r.Role),
		Status:       models.UserStatus(r.Status),
		AccountType:  models.UserAccountType(r.AccountType),
		LastLoginAt:  tsPtr(r.LastLoginAt),
		LastActiveAt: tsPtr(r.LastActiveAt),
		Managed:      r.Managed,
		Timestamps: models.Timestamps{
			CreatedAt: r.CreatedAt.Time,
			UpdatedAt: r.UpdatedAt.Time,
		},
	}
}

// CreateUser inserts a new user, defaulting status/account_type, and populates
// the model's ID and timestamps on success.
func (s *Store) CreateUser(ctx context.Context, user *models.User) error {
	if user.Status == "" {
		user.Status = models.UserStatusActive
	}
	if user.AccountType == "" {
		user.AccountType = models.UserAccountTypeHuman
	}

	id, err := s.q.CreateUser(ctx, sqlc.CreateUserParams{
		Email:       user.Email,
		FullName:    user.FullName,
		Role:        string(user.Role),
		Status:      string(user.Status),
		LastLoginAt: tsFromPtr(user.LastLoginAt),
		AccountType: string(user.AccountType),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: user with email %s already exists", models.ErrConflict, user.Email)
		}
		s.log.Error("failed to create user record in db", "error", err, "email", user.Email)
		return fmt.Errorf("failed to create user: %w", err)
	}

	user.ID = models.UserID(id)
	if row, err := s.q.GetUser(ctx, id); err == nil {
		user.CreatedAt = row.CreatedAt.Time
		user.UpdatedAt = row.UpdatedAt.Time
	}
	return nil
}

// GetUser retrieves a user by ID. Returns models.ErrUserNotFound if absent.
func (s *Store) GetUser(ctx context.Context, id models.UserID) (*models.User, error) {
	row, err := s.q.GetUser(ctx, int64(id))
	if err != nil {
		if notFound(err) {
			return nil, models.ErrUserNotFound
		}
		return nil, fmt.Errorf("getting user id %d: %w", id, err)
	}
	return userToModel(row), nil
}

// GetUserByEmail retrieves a user by email. Returns models.ErrUserNotFound if absent.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	row, err := s.q.GetUserByEmail(ctx, email)
	if err != nil {
		if notFound(err) {
			return nil, models.ErrUserNotFound
		}
		return nil, fmt.Errorf("getting user email %s: %w", email, err)
	}
	return userToModel(row), nil
}

// UpdateUser updates an existing user record.
func (s *Store) UpdateUser(ctx context.Context, user *models.User) error {
	err := s.q.UpdateUser(ctx, sqlc.UpdateUserParams{
		Email:        user.Email,
		FullName:     user.FullName,
		Role:         string(user.Role),
		Status:       string(user.Status),
		LastLoginAt:  tsFromPtr(user.LastLoginAt),
		LastActiveAt: tsFromPtr(user.LastActiveAt),
		UpdatedAt:    ts(user.UpdatedAt),
		ID:           int64(user.ID),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: user with email %s already exists", models.ErrConflict, user.Email)
		}
		s.log.Error("failed to update user record in db", "error", err, "user_id", user.ID)
		return fmt.Errorf("failed to update user: %w", err)
	}
	return nil
}

// ListUsers retrieves all users, ordered by creation date.
func (s *Store) ListUsers(ctx context.Context) ([]*models.User, error) {
	rows, err := s.q.ListUsers(ctx)
	if err != nil {
		s.log.Error("failed to list users from db", "error", err)
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	users := make([]*models.User, 0, len(rows))
	for i := range rows {
		r := rows[i]
		users = append(users, userToModel(r))
	}
	return users, nil
}

// ListServiceAccounts retrieves all service-account users.
func (s *Store) ListServiceAccounts(ctx context.Context) ([]*models.User, error) {
	rows, err := s.q.ListServiceAccounts(ctx)
	if err != nil {
		s.log.Error("failed to list service accounts from db", "error", err)
		return nil, fmt.Errorf("failed to list service accounts: %w", err)
	}
	users := make([]*models.User, 0, len(rows))
	for i := range rows {
		r := rows[i]
		users = append(users, userToModel(r))
	}
	return users, nil
}

// CountAdminUsers counts active users with the admin role.
func (s *Store) CountAdminUsers(ctx context.Context) (int, error) {
	count, err := s.q.CountAdminUsers(ctx, sqlc.CountAdminUsersParams{
		Role:   string(models.UserRoleAdmin),
		Status: string(models.UserStatusActive),
	})
	if err != nil {
		s.log.Error("failed to count admin users in db", "error", err)
		return 0, fmt.Errorf("failed to count admin users: %w", err)
	}
	return int(count), nil
}

// DeleteUser removes a user by ID.
func (s *Store) DeleteUser(ctx context.Context, id models.UserID) error {
	if err := s.q.DeleteUser(ctx, int64(id)); err != nil {
		s.log.Error("failed to delete user record from db", "error", err, "user_id", id)
		return fmt.Errorf("failed to delete user: %w", err)
	}
	return nil
}
