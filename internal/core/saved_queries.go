package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

// --- Saved Query Error Definitions ---

var (
	ErrQueryNotFound       = fmt.Errorf("saved query not found")
	ErrQueryTypeRequired   = fmt.Errorf("query type is required")
	ErrInvalidQueryType    = fmt.Errorf("invalid query type: must be 'logchefql' or 'sql'")
	ErrInvalidQueryContent = fmt.Errorf("invalid query content format or values")
)

// --- Saved Query Content Validation ---

// ValidateSavedQueryContent validates the JSON structure and basic rules of the query content.
func ValidateSavedQueryContent(contentJSON string) error {
	if contentJSON == "" {
		// Allow empty content for potential future use cases or if validation is conditional
		return nil // Or return an error if content is always required
	}

	var queryContent models.SavedQueryContent
	if err := json.Unmarshal([]byte(contentJSON), &queryContent); err != nil {
		return fmt.Errorf("%w: failed to parse JSON: %v", ErrInvalidQueryContent, err)
	}

	// Validate required fields and values
	if queryContent.Version <= 0 {
		return fmt.Errorf("%w: version must be positive", ErrInvalidQueryContent)
	}
	if queryContent.Content == "" {
		return fmt.Errorf("%w: query content cannot be empty", ErrInvalidQueryContent)
	}
	if queryContent.Limit <= 0 {
		return fmt.Errorf("%w: limit must be positive", ErrInvalidQueryContent)
	}

	// Validate time range if present (check against the zero value of the struct)
	// The TimeRange struct only contains Absolute times.
	if queryContent.TimeRange != (models.SavedQueryTimeRange{}) {
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

	// Add more specific content validation based on queryContent.Version if needed

	return nil
}

// --- Saved Query Management Functions ---

// ListQueriesForTeamAndSource retrieves all saved queries associated with a specific team and source.
func ListQueriesForTeamAndSource(ctx context.Context, db *sqlite.DB, log *slog.Logger, teamID models.TeamID, sourceID models.SourceID) ([]*models.SavedTeamQuery, error) {
	log.Debug("listing saved queries for team and source", "team_id", teamID, "source_id", sourceID)

	// Optional: Validate team and source existence first?
	// _, err := GetTeam(ctx, db, teamID) ...
	// _, err := GetSource(ctx, db, chDB, log, sourceID) ...

	queries, err := db.ListQueriesByTeamAndSource(ctx, teamID, sourceID)
	if err != nil {
		log.Error("failed to list saved queries from db", "error", err, "team_id", teamID, "source_id", sourceID)
		return nil, fmt.Errorf("error listing saved queries: %w", err)
	}

	log.Debug("successfully listed saved queries", "team_id", teamID, "source_id", sourceID, "count", len(queries))
	return queries, nil
}

// CreateTeamSourceQuery creates a new saved query for a team and source.
func CreateTeamSourceQuery(ctx context.Context, db *sqlite.DB, log *slog.Logger, teamID models.TeamID, sourceID models.SourceID, name, description, queryContentJSON, queryType string /*, createdBy models.UserID */) (*models.SavedTeamQuery, error) {
	log.Debug("creating saved query", "team_id", teamID, "source_id", sourceID, "name", name, "type", queryType)

	// Validate Query Type
	if queryType == "" {
		return nil, ErrQueryTypeRequired
	}
	if models.SavedQueryType(queryType) != models.SavedQueryTypeLogchefQL && models.SavedQueryType(queryType) != models.SavedQueryTypeSQL {
		return nil, ErrInvalidQueryType
	}

	// Validate Query Content JSON
	if err := ValidateSavedQueryContent(queryContentJSON); err != nil {
		log.Warn("invalid saved query content provided", "error", err, "team_id", teamID, "source_id", sourceID, "name", name)
		return nil, err // Return the specific validation error
	}

	// Optional: Validate team and source existence?

	// Create TeamQuery model for DB interaction
	// Note: The DB layer takes a models.TeamQuery which might differ slightly
	// from models.SavedTeamQuery (e.g., CreatedBy might be handled differently).
	dbQuery := &models.TeamQuery{
		TeamID:       teamID,
		SourceID:     sourceID,
		Name:         name,
		Description:  description,
		QueryContent: queryContentJSON, // Store the raw JSON
		QueryType:    models.SavedQueryType(queryType),
		// CreatedBy: createdBy, // Pass if DB layer expects it
	}

	// Create in database
	if err := db.CreateTeamSourceQuery(ctx, dbQuery); err != nil {
		log.Error("failed to create saved query in db", "error", err, "team_id", teamID, "source_id", sourceID, "name", name)
		// TODO: Check for specific DB errors (e.g., constraints)?
		return nil, fmt.Errorf("error creating saved query: %w", err)
	}

	// Convert the result back to SavedTeamQuery for the caller
	// The dbQuery object should now have the ID and timestamps populated.
	createdQuery := &models.SavedTeamQuery{
		ID:           dbQuery.ID,
		TeamID:       teamID,
		SourceID:     sourceID,
		Name:         name,
		Description:  description,
		QueryType:    models.SavedQueryType(queryType),
		QueryContent: queryContentJSON,
		CreatedAt:    dbQuery.CreatedAt,
		UpdatedAt:    dbQuery.UpdatedAt,
		// CreatedByUserID: createdBy, // Map if needed
	}

	log.Info("saved query created successfully", "query_id", createdQuery.ID, "team_id", teamID, "source_id", sourceID)
	return createdQuery, nil
}

// GetTeamSourceQuery retrieves a specific saved query by its ID, team ID, and source ID.
func GetTeamSourceQuery(ctx context.Context, db *sqlite.DB, log *slog.Logger, teamID models.TeamID, sourceID models.SourceID, queryID int) (*models.SavedTeamQuery, error) {
	log.Debug("getting saved query", "query_id", queryID, "team_id", teamID, "source_id", sourceID)

	query, err := db.GetTeamSourceQuery(ctx, teamID, sourceID, queryID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			log.Warn("saved query not found", "query_id", queryID, "team_id", teamID, "source_id", sourceID)
			return nil, ErrQueryNotFound
		}
		log.Error("failed to get saved query from db", "error", err, "query_id", queryID, "team_id", teamID, "source_id", sourceID)
		return nil, fmt.Errorf("failed to get query: %w", err)
	}

	log.Debug("successfully retrieved saved query", "query_id", queryID)
	return query, nil
}

// UpdateTeamSourceQuery updates an existing saved query.
func UpdateTeamSourceQuery(ctx context.Context, db *sqlite.DB, log *slog.Logger, teamID models.TeamID, sourceID models.SourceID, queryID int, name, description, queryContentJSON, queryType string) (*models.SavedTeamQuery, error) {
	log.Debug("updating saved query", "query_id", queryID, "team_id", teamID, "source_id", sourceID)

	// Validate Query Type if provided
	if queryType != "" && models.SavedQueryType(queryType) != models.SavedQueryTypeLogchefQL && models.SavedQueryType(queryType) != models.SavedQueryTypeSQL {
		return nil, ErrInvalidQueryType
	}

	// Validate Query Content JSON if provided
	if queryContentJSON != "" {
		if err := ValidateSavedQueryContent(queryContentJSON); err != nil {
			log.Warn("invalid saved query content provided for update", "error", err, "query_id", queryID)
			return nil, err // Return the specific validation error
		}
	}

	// Call the database update method
	// The DB layer should handle partial updates based on provided fields
	err := db.UpdateTeamSourceQuery(ctx, teamID, sourceID, queryID, name, description, queryType, queryContentJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			log.Warn("saved query not found for update", "query_id", queryID, "team_id", teamID, "source_id", sourceID)
			return nil, ErrQueryNotFound
		}
		log.Error("failed to update saved query in db", "error", err, "query_id", queryID, "team_id", teamID, "source_id", sourceID)
		return nil, fmt.Errorf("failed to update query: %w", err)
	}

	// After successful update, fetch and return the updated query
	updatedQuery, err := GetTeamSourceQuery(ctx, db, log, teamID, sourceID, queryID)
	if err != nil {
		// Log error but potentially still signal success if update went through
		log.Error("failed to fetch updated saved query after update", "error", err, "query_id", queryID)
		// Return nil, nil or a marker error? For now, propagate fetch error.
		return nil, fmt.Errorf("query updated but failed to fetch result: %w", err)
	}

	log.Info("saved query updated successfully", "query_id", queryID)
	return updatedQuery, nil
}

// DeleteTeamSourceQuery deletes a specific saved query.
func DeleteTeamSourceQuery(ctx context.Context, db *sqlite.DB, log *slog.Logger, teamID models.TeamID, sourceID models.SourceID, queryID int) error {
	log.Info("deleting saved query", "query_id", queryID, "team_id", teamID, "source_id", sourceID)

	// Optional: Check if query exists first?
	// _, err := GetTeamSourceQuery(ctx, db, log, teamID, sourceID, queryID)
	// if err != nil { return err } // Handle ErrQueryNotFound appropriately

	err := db.DeleteTeamSourceQuery(ctx, teamID, sourceID, queryID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Depending on desired behavior, this might not be an error
			log.Warn("saved query not found for deletion", "query_id", queryID, "team_id", teamID, "source_id", sourceID)
			return ErrQueryNotFound // Or return nil if idempotent delete is ok
		}
		log.Error("failed to delete saved query from db", "error", err, "query_id", queryID, "team_id", teamID, "source_id", sourceID)
		return fmt.Errorf("failed to delete query: %w", err)
	}

	log.Info("saved query deleted successfully", "query_id", queryID)
	return nil
}

// CreateTeamSourceBookmarkedQuery creates a new bookmarked saved query for a team and source.
func CreateTeamSourceBookmarkedQuery(ctx context.Context, db *sqlite.DB, log *slog.Logger, teamID models.TeamID, sourceID models.SourceID, name, description, queryContentJSON, queryType string, isBookmarked bool) (*models.SavedTeamQuery, error) {
	log.Debug("creating bookmarked saved query", "team_id", teamID, "source_id", sourceID, "name", name, "type", queryType, "is_bookmarked", isBookmarked)

	// Validate Query Type
	if queryType == "" {
		return nil, ErrQueryTypeRequired
	}
	if models.SavedQueryType(queryType) != models.SavedQueryTypeLogchefQL && models.SavedQueryType(queryType) != models.SavedQueryTypeSQL {
		return nil, ErrInvalidQueryType
	}

	// Validate Query Content JSON
	if err := ValidateSavedQueryContent(queryContentJSON); err != nil {
		log.Warn("invalid saved query content provided", "error", err, "team_id", teamID, "source_id", sourceID, "name", name)
		return nil, err // Return the specific validation error
	}

	// Optional: Validate team and source existence?

	// Create TeamQuery model for DB interaction
	// Note: The DB layer takes a models.TeamQuery which might differ slightly
	// from models.SavedTeamQuery (e.g., CreatedBy might be handled differently).
	dbQuery := &models.BookmarkedTeamQuery{
		TeamID:       teamID,
		SourceID:     sourceID,
		Name:         name,
		Description:  description,
		QueryContent: queryContentJSON, // Store the raw JSON
		QueryType:    models.SavedQueryType(queryType),
		IsBookmarked: isBookmarked,
		// CreatedBy: createdBy, // Pass if DB layer expects it
	}

	// Create in database
	if err := db.CreateTeamSourceBookmarkedQuery(ctx, dbQuery); err != nil {
		log.Error("failed to create bookmarked saved query in db", "error", err, "team_id", teamID, "source_id", sourceID, "name", name)
		// TODO: Check for specific DB errors (e.g., constraints)?
		return nil, fmt.Errorf("error creating bookmarked saved query: %w", err)
	}

	// Convert the result back to SavedTeamQuery for the caller
	// The dbQuery object should now have the ID and timestamps populated.
	createdQuery := &models.SavedTeamQuery{
		ID:           dbQuery.ID,
		TeamID:       teamID,
		SourceID:     sourceID,
		Name:         name,
		Description:  description,
		QueryType:    models.SavedQueryType(queryType),
		QueryContent: queryContentJSON,
		CreatedAt:    dbQuery.CreatedAt,
		UpdatedAt:    dbQuery.UpdatedAt,
		// CreatedByUserID: createdBy, // Map if needed
	}

	log.Info("saved query created successfully", "query_id", createdQuery.ID, "team_id", teamID, "source_id", sourceID)
	return createdQuery, nil
}