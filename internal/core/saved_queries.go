package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

var relativeTimeRegex = regexp.MustCompile(`^\d+[smhdw]$`)

func isValidRelativeTimeFormat(s string) bool {
	return relativeTimeRegex.MatchString(s)
}

// --- Saved Query Error Definitions ---

var (
	ErrQueryNotFound                    = fmt.Errorf("saved query not found")
	ErrQueryTypeRequired                = fmt.Errorf("query type is required")
	ErrInvalidQueryType                 = fmt.Errorf("invalid query configuration")
	ErrInvalidQueryContent              = fmt.Errorf("invalid query content format or values")
	ErrUnsupportedSavedQueryDefinition  = fmt.Errorf("saved query configuration is not supported for this source")
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

	hasRelativeTime := queryContent.TimeRange.Relative != ""
	hasAbsoluteTime := queryContent.TimeRange.Absolute.Start != 0 || queryContent.TimeRange.Absolute.End != 0

	if hasRelativeTime && hasAbsoluteTime {
		return fmt.Errorf("%w: cannot specify both relative and absolute time range", ErrInvalidQueryContent)
	}

	if hasRelativeTime {
		if !isValidRelativeTimeFormat(queryContent.TimeRange.Relative) {
			return fmt.Errorf("%w: invalid relative time format (expected e.g. '15m', '1h', '7d')", ErrInvalidQueryContent)
		}
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

	// Add more specific content validation based on queryContent.Version if needed

	return nil
}

// --- Saved Query Management Functions ---

func resolveSavedQueryMetadata(ctx context.Context, ds *datasource.Service, sourceID models.SourceID, queryType models.SavedQueryType, queryLanguage models.QueryLanguage, editorMode models.SavedQueryEditorMode) (models.SavedQueryType, models.QueryLanguage, models.SavedQueryEditorMode, error) {
	normalizedLanguage, normalizedMode, err := models.ResolveSavedQueryMetadata(queryType, queryLanguage, editorMode)
	if err != nil {
		return "", "", "", fmt.Errorf("%w: %v", ErrInvalidQueryType, err)
	}

	if ds == nil {
		return "", "", "", fmt.Errorf("datasource service is required")
	}
	if err := ds.ValidateSavedQuerySupport(ctx, sourceID, normalizedLanguage, normalizedMode); err != nil {
		return "", "", "", fmt.Errorf("%w: %v", ErrUnsupportedSavedQueryDefinition, err)
	}

	return models.LegacySavedQueryTypeFromLanguage(normalizedLanguage), normalizedLanguage, normalizedMode, nil
}

// ListQueriesForTeamAndSource retrieves all saved queries associated with a specific team and source.
func ListQueriesForTeamAndSource(ctx context.Context, db *sqlite.DB, log *slog.Logger, teamID models.TeamID, sourceID models.SourceID) ([]*models.SavedTeamQuery, error) {

	// Optional: Validate team and source existence first?
	// _, err := GetTeam(ctx, db, teamID) ...
	// _, err := GetSource(ctx, db, chDB, log, sourceID) ...

	queries, err := db.ListQueriesByTeamAndSource(ctx, teamID, sourceID)
	if err != nil {
		log.Error("failed to list saved queries from db", "error", err, "team_id", teamID, "source_id", sourceID)
		return nil, fmt.Errorf("error listing saved queries: %w", err)
	}

	return queries, nil
}

// CreateTeamSourceQuery creates a new saved query for a team and source.
func CreateTeamSourceQuery(ctx context.Context, db *sqlite.DB, ds *datasource.Service, log *slog.Logger, teamID models.TeamID, sourceID models.SourceID, req *models.CreateTeamQueryRequest) (*models.SavedTeamQuery, error) {
	if req == nil {
		return nil, ErrInvalidQueryContent
	}
	if req.QueryType == "" && req.QueryLanguage == "" {
		return nil, ErrQueryTypeRequired
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidQueryContent)
	}
	if strings.TrimSpace(req.QueryContent) == "" {
		return nil, fmt.Errorf("%w: query content is required", ErrInvalidQueryContent)
	}
	name := strings.TrimSpace(req.Name)
	description := strings.TrimSpace(req.Description)

	legacyQueryType, queryLanguage, editorMode, err := resolveSavedQueryMetadata(ctx, ds, sourceID, req.QueryType, req.QueryLanguage, req.EditorMode)
	if err != nil {
		return nil, err
	}

	// Validate Query Content JSON
	if err := ValidateSavedQueryContent(req.QueryContent); err != nil {
		log.Warn("invalid saved query content provided", "error", err, "team_id", teamID, "source_id", sourceID, "name", name)
		return nil, err // Return the specific validation error
	}

	dbQuery := &models.TeamQuery{
		TeamID:        teamID,
		SourceID:      sourceID,
		Name:          name,
		Description:   description,
		QueryContent:  req.QueryContent,
		QueryType:     legacyQueryType,
		QueryLanguage: queryLanguage,
		EditorMode:    editorMode,
	}

	if err := db.CreateTeamSourceQuery(ctx, dbQuery); err != nil {
		log.Error("failed to create saved query in db", "error", err, "team_id", teamID, "source_id", sourceID, "name", name)
		return nil, fmt.Errorf("error creating saved query: %w", err)
	}

	createdQuery := &models.SavedTeamQuery{
		ID:            dbQuery.ID,
		TeamID:        teamID,
		SourceID:      sourceID,
		Name:          name,
		Description:   description,
		QueryType:     legacyQueryType,
		QueryLanguage: queryLanguage,
		EditorMode:    editorMode,
		QueryContent:  req.QueryContent,
		CreatedAt:     dbQuery.CreatedAt,
		UpdatedAt:     dbQuery.UpdatedAt,
	}

	log.Debug("saved query created", "query_id", createdQuery.ID, "team_id", teamID, "source_id", sourceID)
	return createdQuery, nil
}

// GetTeamSourceQuery retrieves a specific saved query by its ID, team ID, and source ID.
func GetTeamSourceQuery(ctx context.Context, db *sqlite.DB, log *slog.Logger, teamID models.TeamID, sourceID models.SourceID, queryID int) (*models.SavedTeamQuery, error) {

	query, err := db.GetTeamSourceQuery(ctx, teamID, sourceID, queryID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			log.Warn("saved query not found", "query_id", queryID, "team_id", teamID, "source_id", sourceID)
			return nil, ErrQueryNotFound
		}
		log.Error("failed to get saved query from db", "error", err, "query_id", queryID, "team_id", teamID, "source_id", sourceID)
		return nil, fmt.Errorf("failed to get query: %w", err)
	}

	return query, nil
}

// UpdateTeamSourceQuery updates an existing saved query.
func UpdateTeamSourceQuery(ctx context.Context, db *sqlite.DB, ds *datasource.Service, log *slog.Logger, teamID models.TeamID, sourceID models.SourceID, queryID int, req *models.UpdateTeamQueryRequest) (*models.SavedTeamQuery, error) {
	if req == nil {
		return nil, ErrInvalidQueryContent
	}

	existingQuery, err := GetTeamSourceQuery(ctx, db, log, teamID, sourceID, queryID)
	if err != nil {
		return nil, err
	}

	name := existingQuery.Name
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, fmt.Errorf("%w: name is required", ErrInvalidQueryContent)
		}
	}
	description := existingQuery.Description
	if req.Description != nil {
		description = *req.Description
	}
	queryContentJSON := existingQuery.QueryContent
	if req.QueryContent != nil {
		queryContentJSON = strings.TrimSpace(*req.QueryContent)
		if queryContentJSON == "" {
			return nil, fmt.Errorf("%w: query content is required", ErrInvalidQueryContent)
		}
	}
	if queryContentJSON != "" {
		if err := ValidateSavedQueryContent(queryContentJSON); err != nil {
			log.Warn("invalid saved query content provided for update", "error", err, "query_id", queryID)
			return nil, err
		}
	}

	queryType := existingQuery.QueryType
	if req.QueryType != nil {
		queryType = *req.QueryType
	}
	queryLanguage := existingQuery.QueryLanguage
	if req.QueryLanguage != nil {
		queryLanguage = *req.QueryLanguage
	}
	editorMode := existingQuery.EditorMode
	if req.EditorMode != nil {
		editorMode = *req.EditorMode
	}

	legacyQueryType, normalizedLanguage, normalizedMode, err := resolveSavedQueryMetadata(ctx, ds, sourceID, queryType, queryLanguage, editorMode)
	if err != nil {
		return nil, err
	}

	err = db.UpdateTeamSourceQuery(ctx, teamID, sourceID, queryID, name, description, string(legacyQueryType), string(normalizedLanguage), string(normalizedMode), queryContentJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			log.Warn("saved query not found for update", "query_id", queryID, "team_id", teamID, "source_id", sourceID)
			return nil, ErrQueryNotFound
		}
		log.Error("failed to update saved query in db", "error", err, "query_id", queryID, "team_id", teamID, "source_id", sourceID)
		return nil, fmt.Errorf("failed to update query: %w", err)
	}

	updatedQuery, err := GetTeamSourceQuery(ctx, db, log, teamID, sourceID, queryID)
	if err != nil {
		log.Error("failed to fetch updated saved query after update", "error", err, "query_id", queryID)
		return nil, fmt.Errorf("query updated but failed to fetch result: %w", err)
	}

	log.Debug("saved query updated", "query_id", queryID)
	return updatedQuery, nil
}

// DeleteTeamSourceQuery deletes a specific saved query.
func DeleteTeamSourceQuery(ctx context.Context, db *sqlite.DB, log *slog.Logger, teamID models.TeamID, sourceID models.SourceID, queryID int) error {

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

	log.Debug("saved query deleted", "query_id", queryID)
	return nil
}
