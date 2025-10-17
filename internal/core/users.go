package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	// "strconv"
	"time"

	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

// --- User/Shared Error Definitions ---
// Keep shared errors here for now, or move to a dedicated errors.go later.

var (
	// ErrUserNotFound is returned when a user is not found.
	ErrUserNotFound = errors.New("user not found")
	// ErrCannotDeleteLastAdmin is returned when attempting to delete or deactivate the last admin.
	ErrCannotDeleteLastAdmin = errors.New("cannot delete or deactivate the last admin user")
	// ErrUserAlreadyExists is returned when trying to create a user that already exists.
	ErrUserAlreadyExists = errors.New("user already exists")
	// ErrTeamAlreadyExists is returned when trying to create a team that already exists.
	ErrTeamAlreadyExists = errors.New("team already exists")
	// ErrInvalidRole is returned for invalid user or team roles.
	ErrInvalidRole = errors.New("invalid role specified")
	// ErrInvalidStatus is returned for invalid user status.
	ErrInvalidStatus = errors.New("invalid status specified")

	// Define team/source errors here temporarily for IsNotFoundError, or reference from other files.
	// Ideally, these would live in teams.go and source.go respectively.
	ErrTeamNotFound = errors.New("team not found")
)

// IsNotFoundError checks if the error is a known not found error.
// TODO: Refactor to check errors defined in respective packages (users, teams, source) if errors are split.
func IsNotFoundError(err error) bool {
	return errors.Is(err, ErrUserNotFound) || errors.Is(err, ErrTeamNotFound) || errors.Is(err, sql.ErrNoRows) || errors.Is(err, models.ErrNotFound) || errors.Is(err, models.ErrUserNotFound) || errors.Is(err, models.ErrTeamNotFound)
}

// --- User Validation Functions ---

// validateUserCreation validates parameters for creating a new user.
func validateUserCreation(email, fullName string, role models.UserRole) error {
	if email == "" {
		return &ValidationError{Field: "email", Message: "email is required"}
	}
	if !isValidEmail(email) { // Assumes isValidEmail is in validation.go
		return &ValidationError{Field: "email", Message: "invalid email format"}
	}
	if fullName == "" {
		return &ValidationError{Field: "fullName", Message: "full name is required"}
	}
	if len(fullName) < 2 || len(fullName) > 100 {
		return &ValidationError{Field: "fullName", Message: "full name must be between 2 and 100 characters"}
	}
	if !isValidFullName(fullName) { // Assumes isValidFullName is in validation.go
		return &ValidationError{Field: "fullName", Message: "full name contains invalid characters"}
	}
	if role != models.UserRoleAdmin && role != models.UserRoleMember {
		return &ValidationError{Field: "role", Message: "role must be either 'admin' or 'member'"}
	}
	return nil
}

// validateUserUpdate validates parameters for updating a user.
// It checks only the fields provided in updateData.
func validateUserUpdate(updateData models.User) error {
	// Full Name validation (if provided)
	if updateData.FullName != "" {
		if len(updateData.FullName) < 2 || len(updateData.FullName) > 100 {
			return &ValidationError{Field: "fullName", Message: "full name must be between 2 and 100 characters"}
		}
		if !isValidFullName(updateData.FullName) { // Assumes isValidFullName is in validation.go
			return &ValidationError{Field: "fullName", Message: "full name contains invalid characters"}
		}
	}

	// Role validation (if provided)
	if updateData.Role != "" {
		if updateData.Role != models.UserRoleAdmin && updateData.Role != models.UserRoleMember {
			return &ValidationError{Field: "role", Message: "role must be either 'admin' or 'member'"}
		}
	}

	// Status validation (if provided)
	if updateData.Status != "" {
		if updateData.Status != models.UserStatusActive && updateData.Status != models.UserStatusInactive {
			return &ValidationError{Field: "status", Message: "status must be either 'active' or 'inactive'"}
		}
	}

	return nil
}

// --- User Management Functions ---

// ListUsers returns all users from the database.
func ListUsers(ctx context.Context, db *sqlite.DB) ([]*models.User, error) {
	return db.ListUsers(ctx)
}

// GetUser retrieves a specific user by their ID.
func GetUser(ctx context.Context, db *sqlite.DB, id models.UserID) (*models.User, error) {
	user, err := db.GetUser(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, models.ErrUserNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("error getting user from db: %w", err)
	}
	return user, nil
}

// GetUserByEmail retrieves a specific user by their email address.
func GetUserByEmail(ctx context.Context, db *sqlite.DB, email string) (*models.User, error) {
	user, err := db.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, models.ErrUserNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("error getting user by email from db: %w", err)
	}
	return user, nil
}

// CreateUser creates a new user in the database.
func CreateUser(ctx context.Context, db *sqlite.DB, log *slog.Logger, email, fullName string, role models.UserRole, status models.UserStatus) (*models.User, error) {
	// Default role if empty
	if role == "" {
		role = models.UserRoleMember
	}
	// Default status if empty
	if status == "" {
		status = models.UserStatusActive
	}

	// Validate input parameters
	if err := validateUserCreation(email, fullName, role); err != nil {
		return nil, err
	}
	// Validate status separately as it's not part of create validation func
	if status != models.UserStatusActive && status != models.UserStatusInactive {
		return nil, &ValidationError{Field: "status", Message: "status must be either 'active' or 'inactive'"}
	}

	// Check if user already exists
	_, err := db.GetUserByEmail(ctx, email)
	if err == nil {
		return nil, fmt.Errorf("%w: user with email %s", ErrUserAlreadyExists, email)
	}

	// Only proceed if it's a "not found" error, which is expected
	if !sqlite.IsNotFoundError(err) && !sqlite.IsUserNotFoundError(err) {
		log.Error("error checking if user exists", "error", err, "email", email)
		return nil, fmt.Errorf("error checking if user exists: %w", err)
	}

	// Create new user model
	now := time.Now().UTC() // Use UTC time
	user := &models.User{
		Email:    email,
		FullName: fullName,
		Role:     role,
		Status:   status,
		Timestamps: models.Timestamps{
			CreatedAt: now,
			UpdatedAt: now, // Use UTC time
		},
	}

	// Save to database
	if err := db.CreateUser(ctx, user); err != nil {
		log.Error("error creating user in db", "error", err, "email", email)
		return nil, fmt.Errorf("error creating user: %w", err)
	}

	log.Info("user created successfully", "user_id", user.ID, "email", user.Email)
	// The user object now contains the ID and timestamps assigned by the DB
	return user, nil
}

// UpdateUser updates an existing user's information.
// Only non-empty fields in the `updateData` struct are applied.
func UpdateUser(ctx context.Context, db *sqlite.DB, log *slog.Logger, userID models.UserID, updateData models.User) error {
	// Validate the fields provided in updateData first
	if err := validateUserUpdate(updateData); err != nil {
		return err
	}

	// Get existing user
	existing, err := db.GetUser(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, models.ErrUserNotFound) {
			return ErrUserNotFound
		}
		log.Error("failed to get existing user for update", "error", err, "user_id", userID)
		return fmt.Errorf("error getting user for update: %w", err)
	}

	updated := false
	// Apply updates from updateData
	if updateData.FullName != "" && updateData.FullName != existing.FullName {
		existing.FullName = updateData.FullName
		updated = true
	}
	if updateData.Role != "" && updateData.Role != existing.Role {
		if updateData.Role != models.UserRoleAdmin && updateData.Role != models.UserRoleMember {
			return fmt.Errorf("%w: must be '%s' or '%s'", ErrInvalidRole, models.UserRoleAdmin, models.UserRoleMember)
		}
		existing.Role = updateData.Role
		updated = true
	}
	if updateData.Status != "" && updateData.Status != existing.Status {
		if updateData.Status != models.UserStatusActive && updateData.Status != models.UserStatusInactive {
			return fmt.Errorf("%w: must be '%s' or '%s'", ErrInvalidStatus, models.UserStatusActive, models.UserStatusInactive)
		}

		// Check if deactivating the last admin
		if updateData.Status == models.UserStatusInactive && existing.Role == models.UserRoleAdmin {
			count, err := db.CountAdminUsers(ctx) // Assuming CountAdminUsers counts *active* admins
			if err != nil {
				log.Error("failed to count admin users during update", "error", err, "user_id", userID)
				return fmt.Errorf("error counting admin users: %w", err)
			}
			if count <= 1 {
				return ErrCannotDeleteLastAdmin
			}
		}
		existing.Status = updateData.Status
		updated = true
	}
	// Only update LastLoginAt if provided in updateData
	if updateData.LastLoginAt != nil {
		// Ensure the provided time is treated as UTC if updating
		loginTimeUTC := updateData.LastLoginAt.UTC()
		if existing.LastLoginAt == nil || !loginTimeUTC.Equal(existing.LastLoginAt.UTC()) {
			existing.LastLoginAt = &loginTimeUTC
			updated = true
		}
	}

	// If no changes, return early
	if !updated {
		log.Debug("no update needed for user", "user_id", userID)
		return nil
	}

	// Explicitly set UpdatedAt to UTC time before saving
	existing.UpdatedAt = time.Now().UTC()

	// Update timestamp is handled by the DB layer (UpdateUser method)
	if err := db.UpdateUser(ctx, existing); err != nil {
		log.Error("failed to update user in db", "error", err, "user_id", userID)
		return fmt.Errorf("error updating user: %w", err)
	}

	log.Info("user updated successfully", "user_id", userID)
	return nil
}

// DeleteUser deletes a user from the database.
func DeleteUser(ctx context.Context, db *sqlite.DB, log *slog.Logger, id models.UserID) error {
	// Validate user exists
	existing, err := db.GetUser(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, models.ErrUserNotFound) {
			return ErrUserNotFound
		}
		log.Error("failed to get user for deletion check", "error", err, "user_id", id)
		return fmt.Errorf("error checking existing user: %w", err)
	}

	// Check if deleting the last admin
	if existing.Role == models.UserRoleAdmin {
		// Need to ensure CountAdminUsers counts *active* admins if status check isn't done before delete
		count, err := db.CountAdminUsers(ctx)
		if err != nil {
			log.Error("failed to count admin users during delete", "error", err, "user_id", id)
			return fmt.Errorf("error counting admin users: %w", err)
		}
		if count <= 1 {
			return ErrCannotDeleteLastAdmin
		}
	}

	if err := db.DeleteUser(ctx, id); err != nil {
		log.Error("failed to delete user from db", "error", err, "user_id", id)
		return fmt.Errorf("error deleting user: %w", err)
	}

	// TODO: Consider deleting user sessions as well?
	// if err := db.DeleteUserSessions(ctx, id); err != nil { ... }

	log.Info("user deleted successfully", "user_id", id)
	return nil
}

// InitAdminUsers ensures that specified admin users exist and are configured correctly.
func InitAdminUsers(ctx context.Context, db *sqlite.DB, log *slog.Logger, adminEmails []string) error {
	if len(adminEmails) == 0 {
		log.Warn("no admin emails configured, skipping admin user initialization")
		return nil
	}

	log.Info("initializing admin users", "emails", adminEmails)
	var setupErrors []error

	for _, email := range adminEmails {
		existing, err := db.GetUserByEmail(ctx, email)
		if err != nil {
			// Log the error type for debugging
			log.Debug("error checking admin user", "email", email, "error", err)

			// Check if it's a "not found" error, which means we need to create the user
			if sqlite.IsNotFoundError(err) || sqlite.IsUserNotFoundError(err) {
				// User doesn't exist, create a new admin user
				log.Info("admin user not found, creating new admin user", "email", email)
				newUser, createErr := CreateUser(ctx, db, log, email, "Admin User", models.UserRoleAdmin, models.UserStatusActive)
				if createErr != nil {
					errMsg := fmt.Sprintf("failed to create admin user %s: %v", email, createErr)
					log.Error(errMsg)
					setupErrors = append(setupErrors, errors.New(errMsg))
				} else {
					log.Info("created new admin user successfully", "email", email, "user_id", newUser.ID)
				}
				continue // Move to next email
			}

			// If it's a different error (not "not found"), log and continue
			errMsg := fmt.Sprintf("failed to check existing admin user %s: %v", email, err)
			log.Error(errMsg)
			setupErrors = append(setupErrors, errors.New(errMsg))
			continue // Try next email
		}

		// User exists, ensure it's an active admin
		if existing != nil {
			// Ensure existing user is an active admin
			needsUpdate := false
			if existing.Role != models.UserRoleAdmin {
				existing.Role = models.UserRoleAdmin
				needsUpdate = true
			}
			if existing.Status != models.UserStatusActive {
				existing.Status = models.UserStatusActive
				needsUpdate = true
			}

			if needsUpdate {
				if err := db.UpdateUser(ctx, existing); err != nil {
					errMsg := fmt.Sprintf("failed to update admin user %s: %v", email, err)
					log.Error(errMsg)
					setupErrors = append(setupErrors, errors.New(errMsg))
				} else {
					log.Info("updated existing user to active admin", "email", email, "user_id", existing.ID)
				}
			} else {
				log.Debug("admin user already configured correctly", "email", email, "user_id", existing.ID)
			}
		}
	}

	// After iterating, check if any admin users exist at all
	count, err := db.CountAdminUsers(ctx)
	if err != nil {
		errMsg := fmt.Sprintf("failed to count admin users after initialization: %v", err)
		log.Error(errMsg)
		setupErrors = append(setupErrors, errors.New(errMsg))
	} else if count == 0 {
		errMsg := "initialization finished, but no active admin users found in the database"
		log.Error(errMsg)
		setupErrors = append(setupErrors, errors.New(errMsg))
	} else {
		log.Info("admin users initialized successfully", "count", count)
	}

	if len(setupErrors) > 0 {
		// Combine errors into a single error message
		combinedError := "errors during admin user initialization:"
		for _, e := range setupErrors {
			combinedError += "\n - " + e.Error()
		}
		return errors.New(combinedError)
	}

	log.Info("admin user initialization complete")
	return nil
}
