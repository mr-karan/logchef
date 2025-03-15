package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

// TeamQueryService handles operations on team queries
type TeamQueryService struct {
	db  *sqlite.DB
	log *slog.Logger
}

// NewTeamQueryService creates a new TeamQueryService
func NewTeamQueryService(db *sqlite.DB, log *slog.Logger) *TeamQueryService {
	return &TeamQueryService{
		db:  db,
		log: log.With("component", "team_query_service"),
	}
}

// ValidateQueryContent validates the query content is well-formed
func (s *TeamQueryService) ValidateQueryContent(content string) error {
	var queryContent models.SavedQueryContent
	err := json.Unmarshal([]byte(content), &queryContent)
	if err != nil {
		return fmt.Errorf("invalid query content: %w", err)
	}

	// Validate version
	if queryContent.Version <= 0 {
		return fmt.Errorf("invalid query content: version must be positive")
	}

	// Validate tab
	if queryContent.ActiveTab != models.SavedQueryTabFilters &&
		queryContent.ActiveTab != models.SavedQueryTabRawSQL {
		return fmt.Errorf("invalid query content: unknown active tab %q", queryContent.ActiveTab)
	}

	// Validate query type
	if queryContent.QueryType != models.SavedQueryTypeLogchefQL &&
		queryContent.QueryType != models.SavedQueryTypeSQL {
		return fmt.Errorf("invalid query content: unknown query type %q", queryContent.QueryType)
	}

	// Validate that appropriate content is provided based on query type
	if queryContent.QueryType == models.SavedQueryTypeLogchefQL && queryContent.LogchefQLContent == "" {
		return fmt.Errorf("invalid query content: LogchefQL content is required for LogchefQL query type")
	}

	if queryContent.QueryType == models.SavedQueryTypeSQL && queryContent.RawSQL == "" {
		return fmt.Errorf("invalid query content: SQL content is required for SQL query type")
	}

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

	// Validate limit
	if queryContent.Limit <= 0 {
		return fmt.Errorf("invalid query content: limit must be positive")
	}

	return nil
}

// CreateTeamQuery creates a new team query
func (s *TeamQueryService) CreateTeamQuery(ctx context.Context, teamID models.TeamID, req models.CreateTeamQueryRequest) (*models.SavedTeamQuery, error) {
	if err := s.ValidateQueryContent(req.QueryContent); err != nil {
		return nil, err
	}

	// Create a TeamQuery object
	query := &models.TeamQuery{
		TeamID:       teamID,
		SourceID:     models.SourceID(req.SourceID),
		Name:         req.Name,
		Description:  req.Description,
		QueryType:    req.QueryType,
		QueryContent: req.QueryContent,
	}

	// Create the query using the db method
	if err := s.db.CreateTeamQuery(ctx, query); err != nil {
		return nil, err
	}

	// Return the created query as a SavedTeamQuery
	return &models.SavedTeamQuery{
		ID:           query.ID,
		TeamID:       teamID,
		SourceID:     req.SourceID,
		Name:         req.Name,
		Description:  req.Description,
		QueryContent: req.QueryContent,
		CreatedAt:    query.Timestamps.CreatedAt,
		UpdatedAt:    query.Timestamps.UpdatedAt,
	}, nil
}

// GetTeamQuery retrieves a team query by ID
func (s *TeamQueryService) GetTeamQuery(ctx context.Context, id int) (*models.SavedTeamQuery, error) {
	return s.db.GetSavedTeamQuery(ctx, id)
}

// GetTeamQueryWithAccess retrieves a team query if user has access
func (s *TeamQueryService) GetTeamQueryWithAccess(ctx context.Context, id int, userID models.UserID) (*models.SavedTeamQuery, error) {
	query, err := s.db.GetTeamQueryWithAccess(ctx, id, userID)
	if err != nil {
		s.log.Error("error getting team query with access", "error", err)
		return nil, ErrTeamQueryNotFound
	}
	if query == nil {
		return nil, ErrTeamQueryNotFound
	}
	return query, nil
}

// UpdateTeamQuery updates an existing team query
func (s *TeamQueryService) UpdateTeamQuery(ctx context.Context, id int, req models.UpdateTeamQueryRequest) (*models.SavedTeamQuery, error) {
	if req.QueryContent != "" {
		if err := s.ValidateQueryContent(req.QueryContent); err != nil {
			return nil, err
		}
	}

	return s.db.UpdateSavedTeamQuery(ctx, id, req)
}

// DeleteTeamQuery deletes a team query
func (s *TeamQueryService) DeleteTeamQuery(ctx context.Context, id int) error {
	return s.db.DeleteTeamQuery(ctx, id)
}

// ListTeamQueries retrieves all team queries for a team
func (s *TeamQueryService) ListTeamQueries(ctx context.Context, teamID models.TeamID) ([]*models.SavedTeamQuery, error) {
	return s.db.ListSavedTeamQueries(ctx, teamID)
}

// ListQueriesForUserAndTeam retrieves all queries for a team that a user has access to
func (s *TeamQueryService) ListQueriesForUserAndTeam(ctx context.Context, userID models.UserID, teamID models.TeamID) ([]*models.SavedTeamQuery, error) {
	return s.db.ListQueriesForUserAndTeam(ctx, userID, teamID)
}

// ListQueriesForUser retrieves all queries that a user has access to
func (s *TeamQueryService) ListQueriesForUser(ctx context.Context, userID models.UserID) ([]*models.SavedTeamQuery, error) {
	return s.db.ListQueriesForUser(ctx, userID)
}

// ListQueriesForSource retrieves all queries for a specific source
func (s *TeamQueryService) ListQueriesForSource(ctx context.Context, sourceID models.SourceID) ([]*models.SavedTeamQuery, error) {
	return s.db.ListQueriesBySource(ctx, sourceID)
}

// ListQueriesForUserBySource retrieves all queries for a specific source that a user has access to
func (s *TeamQueryService) ListQueriesForUserBySource(ctx context.Context, userID models.UserID, sourceID models.SourceID) ([]*models.SavedTeamQuery, error) {
	return s.db.ListQueriesForUserBySource(ctx, userID, sourceID)
}

// ListQueriesByTeamAndSource retrieves all queries for a specific team and source
func (s *TeamQueryService) ListQueriesByTeamAndSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) ([]*models.SavedTeamQuery, error) {
	return s.db.ListQueriesByTeamAndSource(ctx, teamID, sourceID)
}

// ListQueriesForUserBySourceAndTeam retrieves all queries for a specific user, source, and team
func (s *TeamQueryService) ListQueriesForUserBySourceAndTeam(ctx context.Context, userID models.UserID, sourceID models.SourceID, teamID models.TeamID) ([]*models.SavedTeamQuery, error) {
	// Check if the user is a member of the team
	member, err := s.db.GetTeamMember(ctx, teamID, userID)
	if err != nil || member == nil {
		// If there's an error or user is not a member, return empty list
		return []*models.SavedTeamQuery{}, nil
	}

	// Then get queries for the team and source
	return s.db.ListQueriesByTeamAndSource(ctx, teamID, sourceID)
}

// ListQueriesForTeamAndSource is an alias for ListQueriesByTeamAndSource to maintain compatibility
func (s *TeamQueryService) ListQueriesForTeamAndSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) ([]*models.SavedTeamQuery, error) {
	return s.ListQueriesByTeamAndSource(ctx, teamID, sourceID)
}

// CreateTeamSourceQuery creates a new query for a team and source
func (s *TeamQueryService) CreateTeamSourceQuery(ctx context.Context, teamID models.TeamID, sourceID models.SourceID, name, description, queryContent string, createdBy models.UserID) (*models.SavedTeamQuery, error) {
	// Parse the query content to determine the query type
	var queryType models.SavedQueryType = models.SavedQueryTypeSQL // Default to SQL

	// Try to parse the query content to extract the query type
	var content models.SavedQueryContent
	if err := json.Unmarshal([]byte(queryContent), &content); err == nil {
		// If parsing succeeds, use the query type from the content
		if content.QueryType != "" {
			queryType = content.QueryType
		}
	}

	// Create a request to use the existing functionality
	req := models.CreateTeamQueryRequest{
		Name:         name,
		Description:  description,
		SourceID:     sourceID,
		QueryType:    queryType,
		QueryContent: queryContent,
	}

	// Create the query using the team query service
	return s.CreateTeamQuery(ctx, teamID, req)
}
