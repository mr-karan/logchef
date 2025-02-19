package service

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"backend-v2/internal/sqlite"
	"backend-v2/pkg/clickhouse"
	"backend-v2/pkg/logger"
	"backend-v2/pkg/models"
	"backend-v2/pkg/querybuilder"

	"github.com/google/uuid"
)

// Service manages the application's core functionality
type Service struct {
	sqlite *sqlite.DB
	pool   *clickhouse.Pool
	log    *slog.Logger
}

// New creates a new service instance
func New(sqliteDB *sqlite.DB) *Service {
	return &Service{
		sqlite: sqliteDB,
		pool:   clickhouse.NewPool(sqliteDB),
		log:    logger.Default().With("component", "service"),
	}
}

// buildFilterQuery builds a query from filter parameters
func buildFilterQuery(params clickhouse.LogQueryParams, tableName string) string {
	// Create query builder based on mode
	opts := querybuilder.Options{
		StartTime: params.StartTime,
		EndTime:   params.EndTime,
		Limit:     params.Limit,
		Sort:      params.Sort,
	}

	builder := querybuilder.NewFilterBuilder(tableName, opts, params.Conditions)
	query, err := builder.Build()
	if err != nil {
		return ""
	}

	return query.SQL
}

// QueryLogs retrieves logs from a source with pagination and time range
func (s *Service) QueryLogs(ctx context.Context, sourceID string, params clickhouse.LogQueryParams) (*models.QueryResult, error) {
	// Get source from SQLite
	source, err := s.sqlite.GetSource(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting source: %w", err)
	}
	if source == nil {
		return nil, ErrSourceNotFound
	}

	// Get client from pool
	client, err := s.pool.GetClient(sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting client: %w", err)
	}

	// Build query based on mode
	var query string
	switch params.Mode {
	case models.QueryModeRawSQL:
		if params.RawSQL == "" {
			return nil, fmt.Errorf("raw SQL query cannot be empty")
		}
		// Use the query builder to handle the raw SQL
		builder := querybuilder.NewRawSQLBuilder(source.GetFullTableName(), params.RawSQL, params.Limit)
		builtQuery, err := builder.Build()
		if err != nil {
			return nil, fmt.Errorf("error building raw SQL query: %w", err)
		}
		query = builtQuery.SQL
	case models.QueryModeLogChefQL:
		if params.LogChefQL == "" {
			return nil, fmt.Errorf("LogchefQL query cannot be empty")
		}
		// TODO: Implement LogChefQL parsing
		return nil, fmt.Errorf("LogChefQL mode not implemented")
	case models.QueryModeFilters:
		// Build filter query
		query = buildFilterQuery(params, source.GetFullTableName())
	default:
		return nil, fmt.Errorf("unsupported query mode: %s", params.Mode)
	}

	// Execute query
	return client.Query(ctx, query)
}

// InitializeSource initializes a Clickhouse connection for a source
func (s *Service) InitializeSource(ctx context.Context, source *models.Source) error {
	return s.pool.AddSource(source)
}

// ListSources returns all sources with their connection status
func (s *Service) ListSources(ctx context.Context) ([]*models.Source, error) {
	sources, err := s.sqlite.ListSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("error listing sources: %w", err)
	}

	// Enrich with connection status
	for _, source := range sources {
		health, err := s.pool.GetHealth(source.ID)
		if err == nil {
			source.IsConnected = health.Status == models.HealthStatusHealthy
		}
	}

	return sources, nil
}

// GetSource retrieves a source by ID
func (s *Service) GetSource(ctx context.Context, id string) (*models.Source, error) {
	source, err := s.sqlite.GetSource(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("error getting source: %w", err)
	}

	if source == nil {
		return nil, nil
	}

	// Get health status
	health, err := s.pool.GetHealth(id)
	if err == nil {
		source.IsConnected = health.Status == models.HealthStatusHealthy
	}

	return source, nil
}

// CreateSource creates a new source
func (s *Service) CreateSource(ctx context.Context, autoCreateTable bool, conn models.ConnectionInfo, description string, ttlDays int, metaTSField string) (*models.Source, error) {
	source := &models.Source{
		ID:                uuid.New().String(),
		MetaIsAutoCreated: boolToInt(autoCreateTable),
		MetaTSField:       metaTSField,
		Connection:        conn,
		Description:       description,
		TTLDays:           ttlDays,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}

	// Check if source already exists
	existing, err := s.sqlite.GetSourceByName(ctx, source.Connection.Database, source.Connection.TableName)
	if err != nil {
		s.log.Error("failed to check existing source",
			"error", err,
			"database", source.Connection.Database,
			"table", source.Connection.TableName,
		)
		return nil, &ValidationError{
			Field:   "source",
			Message: "Failed to validate source configuration. Please try again.",
		}
	}
	if existing != nil {
		return nil, &ValidationError{
			Field:   "table_name",
			Message: fmt.Sprintf("A source for table %s in database %s already exists", source.Connection.TableName, source.Connection.Database),
		}
	}

	// First add source to pool to get a connection for health check
	if err := s.pool.AddSource(source); err != nil {
		s.log.Error("failed to initialize connection",
			"error", err,
			"source_id", source.ID,
		)
		return nil, &ValidationError{
			Field:   "connection",
			Message: "Failed to connect to the database. Please check your connection details.",
		}
	}

	// Get client for health check
	client, err := s.pool.GetClient(source.ID)
	if err != nil {
		s.log.Error("failed to get client",
			"error", err,
			"source_id", source.ID,
		)
		return nil, &ValidationError{
			Field:   "connection",
			Message: "Failed to establish database connection. Please verify your credentials.",
		}
	}

	// If not auto-creating table, verify it exists
	if !autoCreateTable {
		query := fmt.Sprintf("SELECT 1 FROM %s.%s LIMIT 1", source.Connection.Database, source.Connection.TableName)
		if _, err := client.Query(ctx, query); err != nil {
			s.log.Error("failed to verify table exists",
				"error", err,
				"source_id", source.ID,
				"database", source.Connection.Database,
				"table", source.Connection.TableName,
			)
			return nil, &ValidationError{
				Field:   "table_name",
				Message: fmt.Sprintf("Table %s not found in database %s", source.Connection.TableName, source.Connection.Database),
			}
		}
	}

	// Save to SQLite
	if err := s.sqlite.CreateSource(ctx, source); err != nil {
		s.log.Error("failed to save source",
			"error", err,
			"source_id", source.ID,
		)
		return nil, &ValidationError{
			Field:   "source",
			Message: "Failed to create source. Please try again.",
		}
	}

	// Create table if needed
	if autoCreateTable {
		// Create table using the schema
		schema := models.OTELLogsTableSchema
		schema = strings.ReplaceAll(schema, "{{database_name}}", source.Connection.Database)
		schema = strings.ReplaceAll(schema, "{{table_name}}", source.Connection.TableName)
		if source.TTLDays > 0 {
			schema = strings.ReplaceAll(schema, "{{ttl_day}}", strconv.Itoa(source.TTLDays))
		} else {
			schema = strings.ReplaceAll(schema, "TTL toDateTime(timestamp) + INTERVAL {{ttl_day}} DAY", "")
		}

		if _, err := client.Query(ctx, schema); err != nil {
			// Try to clean up SQLite entry on error
			_ = s.sqlite.DeleteSource(ctx, source.ID)
			s.log.Error("failed to create table",
				"error", err,
				"source_id", source.ID,
			)
			return nil, &ValidationError{
				Field:   "table_name",
				Message: "Failed to create table. Please try again.",
			}
		}
	}

	return source, nil
}

// Helper function to convert bool to int
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// DeleteSource deletes a source
func (s *Service) DeleteSource(ctx context.Context, id string) error {
	// First remove from Clickhouse to prevent any new operations
	if err := s.pool.RemoveSource(id); err != nil {
		return fmt.Errorf("error removing Clickhouse connection: %w", err)
	}

	// Then remove from SQLite
	if err := s.sqlite.DeleteSource(ctx, id); err != nil {
		return fmt.Errorf("error removing from SQLite: %w", err)
	}

	return nil
}

// Close closes all connections
func (s *Service) Close() error {
	var lastErr error

	if err := s.pool.Close(); err != nil {
		lastErr = fmt.Errorf("error closing Clickhouse connections: %w", err)
	}

	return lastErr
}

// GetTimeSeries retrieves time series data for log counts
func (s *Service) GetTimeSeries(ctx context.Context, sourceID string, params clickhouse.TimeSeriesParams) (*clickhouse.TimeSeriesResult, error) {
	// Get source from SQLite
	source, err := s.sqlite.GetSource(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting source: %w", err)
	}
	if source == nil {
		return nil, ErrSourceNotFound
	}

	// Get client from pool
	client, err := s.pool.GetClient(sourceID)
	if err != nil {
		return nil, fmt.Errorf("error getting client: %w", err)
	}

	// Set table name in params
	params.Table = source.GetFullTableName()

	// Get time series data
	return client.GetTimeSeries(ctx, params)
}

// User management methods

// ListUsers returns all users
func (s *Service) ListUsers(ctx context.Context) ([]*models.User, error) {
	return s.sqlite.ListUsers(ctx)
}

// GetUser retrieves a user by ID
func (s *Service) GetUser(ctx context.Context, id string) (*models.User, error) {
	return s.sqlite.GetUser(ctx, id)
}

// CreateUser creates a new user
func (s *Service) CreateUser(ctx context.Context, user *models.User) error {
	// Validate user role
	if user.Role != models.UserRoleAdmin && user.Role != models.UserRoleMember {
		return &ValidationError{
			Field:   "Role",
			Message: "invalid role",
		}
	}

	// Validate user status
	if user.Status != models.UserStatusActive && user.Status != models.UserStatusInactive {
		return &ValidationError{
			Field:   "Status",
			Message: "invalid status",
		}
	}

	// Check if email already exists
	existing, err := s.sqlite.GetUserByEmail(ctx, user.Email)
	if err != nil {
		return fmt.Errorf("error checking existing user: %w", err)
	}
	if existing != nil {
		return &ValidationError{
			Field:   "Email",
			Message: "email already exists",
		}
	}

	// Generate UUID if not provided
	if user.ID == "" {
		user.ID = uuid.New().String()
	}

	// Set timestamps
	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now

	return s.sqlite.CreateUser(ctx, user)
}

// UpdateUser updates a user's information
func (s *Service) UpdateUser(ctx context.Context, user *models.User) error {
	// Validate user exists
	existing, err := s.sqlite.GetUser(ctx, user.ID)
	if err != nil {
		return fmt.Errorf("error checking existing user: %w", err)
	}
	if existing == nil {
		return ErrUserNotFound
	}

	// Validate role if changing
	if user.Role != "" && user.Role != models.UserRoleAdmin && user.Role != models.UserRoleMember {
		return &ValidationError{
			Field:   "Role",
			Message: "invalid role",
		}
	}

	// Validate status if changing
	if user.Status != "" && user.Status != models.UserStatusActive && user.Status != models.UserStatusInactive {
		return &ValidationError{
			Field:   "Status",
			Message: "invalid status",
		}
	}

	// If changing to inactive and user is admin, ensure there's at least one other active admin
	if user.Status == models.UserStatusInactive && existing.Role == models.UserRoleAdmin {
		count, err := s.sqlite.CountAdminUsers(ctx)
		if err != nil {
			return fmt.Errorf("error counting admin users: %w", err)
		}
		if count <= 1 {
			return &ValidationError{
				Field:   "Status",
				Message: "cannot deactivate last admin user",
			}
		}
	}

	// Update timestamp
	user.UpdatedAt = time.Now()

	return s.sqlite.UpdateUser(ctx, user)
}

// DeleteUser deletes a user
func (s *Service) DeleteUser(ctx context.Context, id string) error {
	// Validate user exists
	existing, err := s.sqlite.GetUser(ctx, id)
	if err != nil {
		return fmt.Errorf("error checking existing user: %w", err)
	}
	if existing == nil {
		return ErrUserNotFound
	}

	// If user is admin, ensure there's at least one other active admin
	if existing.Role == models.UserRoleAdmin {
		count, err := s.sqlite.CountAdminUsers(ctx)
		if err != nil {
			return fmt.Errorf("error counting admin users: %w", err)
		}
		if count <= 1 {
			return &ValidationError{
				Field:   "ID",
				Message: "cannot delete last admin user",
			}
		}
	}

	return s.sqlite.DeleteUser(ctx, id)
}

// Team management methods

// ListTeams returns all teams
func (s *Service) ListTeams(ctx context.Context) ([]*models.Team, error) {
	teams, err := s.sqlite.ListTeams(ctx)
	if err != nil {
		s.log.Error("failed to list teams",
			"error", err,
		)
		return nil, ErrInvalidRequest
	}
	return teams, nil
}

// GetTeam retrieves a team by ID
func (s *Service) GetTeam(ctx context.Context, id string) (*models.Team, error) {
	team, err := s.sqlite.GetTeam(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("error getting team: %w", err)
	}
	if team == nil {
		return nil, ErrTeamNotFound
	}
	return team, nil
}

// CreateTeam creates a new team
func (s *Service) CreateTeam(ctx context.Context, team *models.Team) error {
	// Validate team name
	if team.Name == "" {
		return &ValidationError{
			Field:   "Name",
			Message: "team name is required",
		}
	}

	// Generate UUID if not provided
	if team.ID == "" {
		team.ID = uuid.New().String()
	}

	if err := s.sqlite.CreateTeam(ctx, team); err != nil {
		s.log.Error("failed to create team",
			"error", err,
			"team_name", team.Name,
		)
		if strings.Contains(err.Error(), "already exists") {
			return &ValidationError{
				Field:   "Name",
				Message: "team name already exists",
			}
		}
		return ErrInvalidRequest
	}

	return nil
}

// UpdateTeam updates a team's information
func (s *Service) UpdateTeam(ctx context.Context, team *models.Team) error {
	// Validate team exists
	existing, err := s.sqlite.GetTeam(ctx, team.ID)
	if err != nil {
		return fmt.Errorf("error checking existing team: %w", err)
	}
	if existing == nil {
		return ErrTeamNotFound
	}

	// Validate team name
	if team.Name == "" {
		return &ValidationError{
			Field:   "Name",
			Message: "team name is required",
		}
	}

	// Update timestamp
	team.UpdatedAt = time.Now()

	return s.sqlite.UpdateTeam(ctx, team)
}

// DeleteTeam deletes a team
func (s *Service) DeleteTeam(ctx context.Context, id string) error {
	// Validate team exists
	existing, err := s.sqlite.GetTeam(ctx, id)
	if err != nil {
		s.log.Error("error checking existing team",
			"error", err,
			"team_id", id,
		)
		return ErrTeamNotFound
	}
	if existing == nil {
		return ErrTeamNotFound
	}

	// Check if team has members
	members, err := s.sqlite.ListTeamMembers(ctx, id)
	if err != nil {
		s.log.Error("error checking team members",
			"error", err,
			"team_id", id,
		)
		return &ValidationError{
			Field:   "ID",
			Message: "Cannot delete team at this time",
		}
	}
	if len(members) > 0 {
		return &ValidationError{
			Field:   "ID",
			Message: "Cannot delete team with active members",
		}
	}

	if err := s.sqlite.DeleteTeam(ctx, id); err != nil {
		s.log.Error("error deleting team",
			"error", err,
			"team_id", id,
		)
		return &ValidationError{
			Field:   "ID",
			Message: "Failed to delete team",
		}
	}

	return nil
}

// Team member methods

// ListTeamMembers returns all members of a team
func (s *Service) ListTeamMembers(ctx context.Context, teamID string) ([]*models.TeamMember, error) {
	// Validate team exists
	team, err := s.sqlite.GetTeam(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("error checking team: %w", err)
	}
	if team == nil {
		return nil, ErrTeamNotFound
	}

	return s.sqlite.ListTeamMembers(ctx, teamID)
}

// AddTeamMember adds a user to a team
func (s *Service) AddTeamMember(ctx context.Context, teamID, userID, role string) error {
	// Validate team exists
	team, err := s.sqlite.GetTeam(ctx, teamID)
	if err != nil {
		return fmt.Errorf("error checking team: %w", err)
	}
	if team == nil {
		return ErrTeamNotFound
	}

	// Validate user exists
	user, err := s.sqlite.GetUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("error checking user: %w", err)
	}
	if user == nil {
		return ErrUserNotFound
	}

	// Validate role
	if role != "admin" && role != "member" {
		return &ValidationError{
			Field:   "Role",
			Message: "invalid role",
		}
	}

	return s.sqlite.AddTeamMember(ctx, teamID, userID, role)
}

// RemoveTeamMember removes a user from a team
func (s *Service) RemoveTeamMember(ctx context.Context, teamID, userID string) error {
	// Validate team exists
	team, err := s.sqlite.GetTeam(ctx, teamID)
	if err != nil {
		return fmt.Errorf("error checking team: %w", err)
	}
	if team == nil {
		return ErrTeamNotFound
	}

	// Validate user exists
	user, err := s.sqlite.GetUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("error checking user: %w", err)
	}
	if user == nil {
		return ErrUserNotFound
	}

	return s.sqlite.RemoveTeamMember(ctx, teamID, userID)
}

// Team source methods

// ListTeamSources returns all sources for a team
func (s *Service) ListTeamSources(ctx context.Context, teamID string) ([]*models.Source, error) {
	// Validate team exists
	team, err := s.sqlite.GetTeam(ctx, teamID)
	if err != nil {
		s.log.Error("failed to check team existence",
			"error", err,
			"team_id", teamID,
		)
		return nil, ErrTeamNotFound
	}
	if team == nil {
		return nil, ErrTeamNotFound
	}

	return s.sqlite.ListTeamSources(ctx, teamID)
}

// AddTeamSource adds a source to a team
func (s *Service) AddTeamSource(ctx context.Context, teamID, sourceID string) error {
	// Validate team exists
	team, err := s.sqlite.GetTeam(ctx, teamID)
	if err != nil {
		s.log.Error("failed to check team existence",
			"error", err,
			"team_id", teamID,
		)
		return ErrTeamNotFound
	}
	if team == nil {
		return ErrTeamNotFound
	}

	// Validate source exists
	source, err := s.sqlite.GetSource(ctx, sourceID)
	if err != nil {
		s.log.Error("failed to check source existence",
			"error", err,
			"source_id", sourceID,
		)
		return ErrSourceNotFound
	}
	if source == nil {
		return ErrSourceNotFound
	}

	// Add source to team
	if err := s.sqlite.AddTeamSource(ctx, teamID, sourceID); err != nil {
		s.log.Error("failed to add source to team",
			"error", err,
			"team_id", teamID,
			"source_id", sourceID,
		)
		return &ValidationError{
			Field:   "source_id",
			Message: "Failed to add source to team",
		}
	}

	return nil
}

// RemoveTeamSource removes a source from a team
func (s *Service) RemoveTeamSource(ctx context.Context, teamID, sourceID string) error {
	// Validate team exists
	team, err := s.sqlite.GetTeam(ctx, teamID)
	if err != nil {
		s.log.Error("failed to check team existence",
			"error", err,
			"team_id", teamID,
		)
		return ErrTeamNotFound
	}
	if team == nil {
		return ErrTeamNotFound
	}

	// Validate source exists
	source, err := s.sqlite.GetSource(ctx, sourceID)
	if err != nil {
		s.log.Error("failed to check source existence",
			"error", err,
			"source_id", sourceID,
		)
		return ErrSourceNotFound
	}
	if source == nil {
		return ErrSourceNotFound
	}

	// Remove source from team
	if err := s.sqlite.RemoveTeamSource(ctx, teamID, sourceID); err != nil {
		s.log.Error("failed to remove source from team",
			"error", err,
			"team_id", teamID,
			"source_id", sourceID,
		)
		return &ValidationError{
			Field:   "source_id",
			Message: "Failed to remove source from team",
		}
	}

	return nil
}

// ListSourceTeams returns all teams that have access to a source
func (s *Service) ListSourceTeams(ctx context.Context, sourceID string) ([]*models.Team, error) {
	// Validate source exists
	source, err := s.sqlite.GetSource(ctx, sourceID)
	if err != nil {
		s.log.Error("failed to check source existence",
			"error", err,
			"source_id", sourceID,
		)
		return nil, ErrSourceNotFound
	}
	if source == nil {
		return nil, ErrSourceNotFound
	}

	return s.sqlite.ListSourceTeams(ctx, sourceID)
}

// InitAdminUsers initializes admin users from the provided email list
func (s *Service) InitAdminUsers(ctx context.Context, adminEmails []string) error {
	for _, email := range adminEmails {
		// Check if user already exists
		existing, err := s.sqlite.GetUserByEmail(ctx, email)
		if err != nil {
			return fmt.Errorf("error checking existing user: %w", err)
		}
		if existing != nil {
			// If user exists but is not admin, promote them
			if existing.Role != models.UserRoleAdmin {
				existing.Role = models.UserRoleAdmin
				if err := s.sqlite.UpdateUser(ctx, existing); err != nil {
					return fmt.Errorf("error promoting user to admin: %w", err)
				}
				s.log.Info("promoted existing user to admin", "email", email)
			}
			continue
		}

		// Create new admin user
		user := &models.User{
			ID:       uuid.New().String(),
			Email:    email,
			FullName: "Admin User", // This will be updated on first login via OIDC
			Role:     models.UserRoleAdmin,
			Status:   models.UserStatusActive,
		}

		if err := s.sqlite.CreateUser(ctx, user); err != nil {
			return fmt.Errorf("error creating admin user: %w", err)
		}
		s.log.Info("created new admin user", "email", email)
	}

	return nil
}
