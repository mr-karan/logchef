package saved_queries

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

// ErrQueryNotFound is returned when a query is not found
var ErrQueryNotFound = fmt.Errorf("saved query not found")

// ErrQueryTypeRequired is returned when the query type is missing
var ErrQueryTypeRequired = fmt.Errorf("query type is required")

// Service handles operations on team queries
type Service struct {
	db  *sqlite.DB
	log *slog.Logger
}

// New creates a new TeamQueryService
func New(db *sqlite.DB, log *slog.Logger) *Service {
	return &Service{
		db:  db,
		log: log.With("component", "saved_query_service"),
	}
}

// ValidateQueryContent validates the query content is well-formed
func (s *Service) ValidateQueryContent(content string) error {
	// Parse content
	var queryContent models.SavedQueryContent
	err := json.Unmarshal([]byte(content), &queryContent)
	if err != nil {
		return fmt.Errorf("invalid query content: %w", err)
	}

	// Validate version
	if queryContent.Version <= 0 {
		return fmt.Errorf("invalid query content: version must be positive")
	}

	// Validate content
	if queryContent.Content == "" {
		return fmt.Errorf("invalid query content: query content cannot be empty")
	}

	// Check if TimeRange is not null (when timeRange has values)
	emptyTimeRange := models.SavedQueryTimeRange{}
	if queryContent.TimeRange != emptyTimeRange {
		// Validate time range
		if queryContent.TimeRange.Absolute.Start <= 0 {
			return fmt.Errorf("invalid query content: start time must be positive")
		}
		if queryContent.TimeRange.Absolute.End <= 0 {
			return fmt.Errorf("invalid query content: end time must be positive")
		}
		if queryContent.TimeRange.Absolute.End < queryContent.TimeRange.Absolute.Start {
			return fmt.Errorf("invalid query content: end time must be after start time")
		}
	}

	// Validate limit
	if queryContent.Limit <= 0 {
		return fmt.Errorf("invalid query content: limit must be positive")
	}

	return nil
}

// ListQueriesForTeamAndSource retrieves all queries for a specific team and source
func (s *Service) ListQueriesForTeamAndSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) ([]*models.SavedTeamQuery, error) {
	// Call the specific DB method
	return s.db.ListQueriesByTeamAndSource(ctx, teamID, sourceID)
}

// CreateTeamSourceQuery creates a new query for a team and source
func (s *Service) CreateTeamSourceQuery(ctx context.Context, teamID models.TeamID, sourceID models.SourceID, name, description, queryContent, queryType string, createdBy models.UserID) (*models.SavedTeamQuery, error) {
	// Validate query type is provided
	if queryType == "" {
		return nil, ErrQueryTypeRequired
	}

	if err := s.ValidateQueryContent(queryContent); err != nil {
		return nil, err
	}

	// Validate query type
	if queryType != string(models.SavedQueryTypeLogchefQL) && queryType != string(models.SavedQueryTypeSQL) {
		return nil, fmt.Errorf("invalid query type: must be 'logchefql' or 'sql'")
	}

	// Create a TeamQuery object
	query := &models.TeamQuery{
		TeamID:       teamID,
		SourceID:     sourceID,
		Name:         name,
		Description:  description,
		QueryContent: queryContent,
		QueryType:    models.SavedQueryType(queryType),
		// CreatedBy:    createdBy,
	}

	// Create the query using the specific db method
	if err := s.db.CreateTeamSourceQuery(ctx, query); err != nil {
		return nil, err
	}

	// Return the created query as a SavedTeamQuery
	return &models.SavedTeamQuery{
		ID:           query.ID,
		TeamID:       teamID,
		SourceID:     sourceID,
		Name:         name,
		Description:  description,
		QueryType:    models.SavedQueryType(queryType),
		QueryContent: queryContent,
		CreatedAt:    query.Timestamps.CreatedAt,
		UpdatedAt:    query.Timestamps.UpdatedAt,
		// TODO: Add CreatedBy if it exists in SavedTeamQuery
	}, nil
}

// GetTeamSourceQuery retrieves a specific query for a team and source
func (s *Service) GetTeamSourceQuery(ctx context.Context, teamID models.TeamID, sourceID models.SourceID, queryID int) (*models.SavedTeamQuery, error) {
	s.log.Info("getting team source query", "teamID", teamID, "sourceID", sourceID, "queryID", queryID)
	// Call the database method
	query, err := s.db.GetTeamSourceQuery(ctx, teamID, sourceID, queryID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrQueryNotFound
		}
		s.log.Error("failed to get team source query from db", "error", err, "queryID", queryID, "teamID", teamID, "sourceID", sourceID)
		return nil, fmt.Errorf("failed to get query: %w", err)
	}
	return query, nil
}

// UpdateTeamSourceQuery updates a specific query for a team and source
func (s *Service) UpdateTeamSourceQuery(ctx context.Context, teamID models.TeamID, sourceID models.SourceID, queryID int, name, description, queryContent, queryType string) (*models.SavedTeamQuery, error) {
	s.log.Info("updating team source query", "teamID", teamID, "sourceID", sourceID, "queryID", queryID)
	if err := s.ValidateQueryContent(queryContent); err != nil {
		return nil, err
	}

	// Validate query type if provided
	if queryType != "" && queryType != string(models.SavedQueryTypeLogchefQL) && queryType != string(models.SavedQueryTypeSQL) {
		return nil, fmt.Errorf("invalid query type: must be 'logchefql' or 'sql'")
	}

	// Call the database method
	err := s.db.UpdateTeamSourceQuery(ctx, teamID, sourceID, queryID, name, description, queryType, queryContent)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrQueryNotFound
		}
		s.log.Error("failed to update team source query in db", "error", err, "queryID", queryID, "teamID", teamID, "sourceID", sourceID)
		return nil, fmt.Errorf("failed to update query: %w", err)
	}

	// After successful update, fetch and return the updated query
	return s.GetTeamSourceQuery(ctx, teamID, sourceID, queryID)
}

// DeleteTeamSourceQuery deletes a specific query for a team and source
func (s *Service) DeleteTeamSourceQuery(ctx context.Context, teamID models.TeamID, sourceID models.SourceID, queryID int) error {
	s.log.Info("deleting team source query", "teamID", teamID, "sourceID", sourceID, "queryID", queryID)
	// Call the database method
	err := s.db.DeleteTeamSourceQuery(ctx, teamID, sourceID, queryID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrQueryNotFound
		}
		s.log.Error("failed to delete team source query from db", "error", err, "queryID", queryID, "teamID", teamID, "sourceID", sourceID)
		return fmt.Errorf("failed to delete query: %w", err)
	}
	return nil
}
