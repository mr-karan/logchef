package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"

	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

var relativeTimeRegex = regexp.MustCompile(`^\d+[smhdw]$`)

func isValidRelativeTimeFormat(s string) bool {
	return relativeTimeRegex.MatchString(s)
}

// --- Saved Query Error Definitions ---

var (
	ErrQueryNotFound       = fmt.Errorf("saved query not found")
	ErrQueryTypeRequired   = fmt.Errorf("query type is required")
	ErrInvalidQueryType    = fmt.Errorf("invalid query type: must be 'logchefql' or 'sql'")
	ErrInvalidQueryContent = fmt.Errorf("invalid query content format or values")
	ErrSavedQueryForbidden = fmt.Errorf("not allowed to access this saved query")
)

// --- Saved Query Content Validation ---

// ValidateSavedQueryContent validates the JSON structure and basic rules of the query content.
func ValidateSavedQueryContent(contentJSON string) error {
	if contentJSON == "" {
		return nil
	}

	var queryContent models.SavedQueryContent
	if err := json.Unmarshal([]byte(contentJSON), &queryContent); err != nil {
		return fmt.Errorf("%w: failed to parse JSON: %v", ErrInvalidQueryContent, err)
	}

	if queryContent.Version <= 0 {
		return fmt.Errorf("%w: version must be positive", ErrInvalidQueryContent)
	}
	if queryContent.Content == "" {
		return fmt.Errorf("%w: query content cannot be empty", ErrInvalidQueryContent)
	}
	if queryContent.Limit <= 0 {
		return fmt.Errorf("%w: limit must be positive", ErrInvalidQueryContent)
	}

	hasRelativeTime := queryContent.TimeRange.Relative != ""
	hasAbsoluteTime := queryContent.TimeRange.Absolute.Start != 0 || queryContent.TimeRange.Absolute.End != 0

	if hasRelativeTime && hasAbsoluteTime {
		return fmt.Errorf("%w: cannot specify both relative and absolute time range", ErrInvalidQueryContent)
	}

	if hasRelativeTime && !isValidRelativeTimeFormat(queryContent.TimeRange.Relative) {
		return fmt.Errorf("%w: invalid relative time format (expected e.g. '15m', '1h', '7d')", ErrInvalidQueryContent)
	}

	if hasAbsoluteTime {
		if queryContent.TimeRange.Absolute.Start <= 0 {
			return fmt.Errorf("%w: absolute start time must be positive", ErrInvalidQueryContent)
		}
		if queryContent.TimeRange.Absolute.End <= 0 {
			return fmt.Errorf("%w: absolute end time must be positive", ErrInvalidQueryContent)
		}
		if queryContent.TimeRange.Absolute.End < queryContent.TimeRange.Absolute.Start {
			return fmt.Errorf("%w: absolute end time must be after start time", ErrInvalidQueryContent)
		}
	}

	return nil
}

// validateSavedQueryFields runs the shared type+content checks used by create and update.
func validateSavedQueryFields(queryType, queryContentJSON string, requireType bool) error {
	if requireType && queryType == "" {
		return ErrQueryTypeRequired
	}
	if queryType != "" {
		t := models.SavedQueryType(queryType)
		if t != models.SavedQueryTypeLogchefQL && t != models.SavedQueryTypeSQL {
			return ErrInvalidQueryType
		}
	}
	if queryContentJSON != "" {
		if err := ValidateSavedQueryContent(queryContentJSON); err != nil {
			return err
		}
	}
	return nil
}

// CreateSavedQuery persists a new saved query owned by the supplied creator.
func CreateSavedQuery(ctx context.Context, db *sqlite.DB, log *slog.Logger, sourceID models.SourceID, name, description, queryContentJSON, queryType string, createdBy models.UserID) (*models.SavedQuery, error) {
	if err := validateSavedQueryFields(queryType, queryContentJSON, true); err != nil {
		log.Warn("invalid saved query create payload", "error", err, "source_id", sourceID, "name", name)
		return nil, err
	}

	owner := createdBy
	created, err := db.CreateSavedQuery(ctx, sourceID, name, description, queryType, queryContentJSON, &owner)
	if err != nil {
		log.Error("failed to create saved query", "error", err, "source_id", sourceID, "name", name)
		return nil, fmt.Errorf("error creating saved query: %w", err)
	}

	log.Debug("saved query created", "query_id", created.ID, "source_id", sourceID, "created_by", createdBy)
	return created, nil
}

// GetSavedQuery retrieves a saved query by id.
func GetSavedQuery(ctx context.Context, db *sqlite.DB, log *slog.Logger, queryID int) (*models.SavedQuery, error) {
	q, err := db.GetSavedQuery(ctx, queryID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || sqlite.IsNotFoundError(err) {
			return nil, ErrQueryNotFound
		}
		log.Error("failed to get saved query", "error", err, "query_id", queryID)
		return nil, fmt.Errorf("error getting saved query: %w", err)
	}
	return q, nil
}

// UpdateSavedQuery applies new field values to an existing saved query.
func UpdateSavedQuery(ctx context.Context, db *sqlite.DB, log *slog.Logger, queryID int, name, description, queryContentJSON, queryType string) (*models.SavedQuery, error) {
	if err := validateSavedQueryFields(queryType, queryContentJSON, false); err != nil {
		log.Warn("invalid saved query update payload", "error", err, "query_id", queryID)
		return nil, err
	}

	if err := db.UpdateSavedQuery(ctx, queryID, name, description, queryType, queryContentJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) || sqlite.IsNotFoundError(err) {
			return nil, ErrQueryNotFound
		}
		log.Error("failed to update saved query", "error", err, "query_id", queryID)
		return nil, fmt.Errorf("error updating saved query: %w", err)
	}

	updated, err := GetSavedQuery(ctx, db, log, queryID)
	if err != nil {
		return nil, fmt.Errorf("query updated but failed to fetch result: %w", err)
	}
	return updated, nil
}

// DeleteSavedQuery removes a saved query.
func DeleteSavedQuery(ctx context.Context, db *sqlite.DB, log *slog.Logger, queryID int) error {
	if err := db.DeleteSavedQuery(ctx, queryID); err != nil {
		log.Error("failed to delete saved query", "error", err, "query_id", queryID)
		return fmt.Errorf("error deleting saved query: %w", err)
	}
	return nil
}

// ListSavedQueriesForUser returns every saved query the user can see (cross-team, source-mediated).
func ListSavedQueriesForUser(ctx context.Context, db *sqlite.DB, log *slog.Logger, userID models.UserID) ([]*models.SavedQuery, error) {
	queries, err := db.ListSavedQueriesForUser(ctx, userID)
	if err != nil {
		log.Error("failed to list saved queries for user", "error", err, "user_id", userID)
		return nil, fmt.Errorf("error listing saved queries: %w", err)
	}
	return queries, nil
}

// ListSavedQueriesForUserBySource returns saved queries for one source, gated by user access.
func ListSavedQueriesForUserBySource(ctx context.Context, db *sqlite.DB, log *slog.Logger, userID models.UserID, sourceID models.SourceID) ([]*models.SavedQuery, error) {
	queries, err := db.ListSavedQueriesForUserBySource(ctx, userID, sourceID)
	if err != nil {
		log.Error("failed to list saved queries for user+source", "error", err, "user_id", userID, "source_id", sourceID)
		return nil, fmt.Errorf("error listing saved queries: %w", err)
	}
	return queries, nil
}

// ToggleSavedQueryBookmark flips the bookmark flag and returns the new value.
func ToggleSavedQueryBookmark(ctx context.Context, db *sqlite.DB, log *slog.Logger, queryID int) (bool, error) {
	status, err := db.ToggleSavedQueryBookmark(ctx, queryID)
	if err != nil {
		log.Error("failed to toggle saved query bookmark", "error", err, "query_id", queryID)
		return false, fmt.Errorf("error toggling saved query bookmark: %w", err)
	}
	return status, nil
}

// UserCanEditSavedQuery returns true if the user is the creator or a global admin.
// Legacy queries (CreatedBy == nil) are editable only by global admins.
func UserCanEditSavedQuery(query *models.SavedQuery, user *models.User) bool {
	if user == nil || query == nil {
		return false
	}
	if user.Role == models.UserRoleAdmin {
		return true
	}
	if query.CreatedBy != nil && *query.CreatedBy == user.ID {
		return true
	}
	return false
}
