package core

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
	// Assuming validation logic will be moved/available here or called directly
	// "github.com/mr-karan/logchef/internal/source" // Import for validation types if needed temporarily
)

// ErrSourceNotFound is returned when a source is not found
var ErrSourceNotFound = fmt.Errorf("source not found")
var ErrSourceAlreadyExists = fmt.Errorf("source already exists")

// --- Source Validation Functions ---

// validateSourceCreation validates source creation parameters.
func validateSourceCreation(name string, conn models.ConnectionInfo, description string, ttlDays int, metaTSField string, metaSeverityField string) error {
	// Validate source name
	if name == "" {
		return &ValidationError{Field: "name", Message: "source name is required"}
	}
	if !isValidSourceName(name) {
		return &ValidationError{Field: "name", Message: "source name must not exceed 50 characters and can only contain letters, numbers, spaces, hyphens, and underscores"}
	}

	// Validate connection info (reusing ValidateConnection logic)
	if err := validateConnection(conn); err != nil {
		// Cast to *ValidationError to potentially update the Field
		if validationErr, ok := err.(*ValidationError); ok {
			validationErr.Field = "connection." + validationErr.Field // Prepend field context
			return validationErr
		}
		return err // Return original error if cast fails
	}

	// Table name is mandatory for source creation
	if conn.TableName == "" {
		return &ValidationError{Field: "connection.tableName", Message: "table name is required"}
	}

	if len(description) > 500 {
		return &ValidationError{Field: "description", Message: "description must not exceed 500 characters"}
	}

	if ttlDays < -1 {
		return &ValidationError{Field: "ttlDays", Message: "TTL days must be -1 (no TTL) or a non-negative number"}
	}

	if metaTSField == "" {
		return &ValidationError{Field: "metaTSField", Message: "meta timestamp field is required"}
	}
	if !isValidColumnName(metaTSField) {
		return &ValidationError{Field: "metaTSField", Message: "meta timestamp field contains invalid characters"}
	}

	// Severity field is optional, but if provided, it must be valid
	if metaSeverityField != "" && !isValidColumnName(metaSeverityField) {
		return &ValidationError{Field: "metaSeverityField", Message: "meta severity field contains invalid characters"}
	}

	return nil
}

// validateSourceUpdate validates source update parameters.
func validateSourceUpdate(description string, ttlDays int) error {
	// Description can be empty, but check length if provided
	if len(description) > 500 {
		return &ValidationError{Field: "description", Message: "description must not exceed 500 characters"}
	}
	if ttlDays < -1 {
		return &ValidationError{Field: "ttlDays", Message: "TTL days must be -1 (no TTL) or a non-negative number"}
	}
	return nil
}

// validateConnection validates connection parameters for a connection test.
func validateConnection(conn models.ConnectionInfo) error {
	// Validate host
	if conn.Host == "" {
		return &ValidationError{Field: "host", Message: "host is required"}
	}

	// Parse host and port
	_, portStr, err := net.SplitHostPort(conn.Host)
	if err != nil {
		// Allow hosts without explicit port (e.g., service names in Docker/k8s)
		// We assume ClickHouse client handles default port (9000)
		// Check if it's a missing port error specifically
		if strings.Contains(err.Error(), "missing port in address") {
			// Potentially log a warning, but allow it for now
		} else {
			return &ValidationError{Field: "host", Message: "invalid host format", Err: err}
		}
	} else {
		// Validate port is a number if present
		port, err := strconv.Atoi(portStr)
		if err != nil || port <= 0 || port > 65535 {
			return &ValidationError{Field: "host", Message: "port must be between 1 and 65535"}
		}
	}

	// Username and Password validation
	if conn.Username != "" && conn.Password == "" {
		return &ValidationError{Field: "password", Message: "password is required when username is provided"}
	}

	// Validate database name
	if conn.Database == "" {
		return &ValidationError{Field: "database", Message: "database is required"}
	}
	if !isValidTableName(conn.Database) {
		return &ValidationError{Field: "database", Message: "database name contains invalid characters"}
	}

	// Validate table name if provided
	if conn.TableName != "" && !isValidTableName(conn.TableName) {
		return &ValidationError{Field: "tableName", Message: "table name contains invalid characters"}
	}

	return nil
}

// validateColumnTypes validates that the timestamp and severity columns exist and have compatible types in ClickHouse.
func validateColumnTypes(ctx context.Context, client *clickhouse.Client, log *slog.Logger, database, tableName, tsField, severityField string) error {
	if client == nil {
		return &ValidationError{Field: "connection", Message: "Internal error: Invalid database client provided for validation"}
	}

	// --- Timestamp Field Validation ---
	tsQuery := fmt.Sprintf(
		`SELECT type FROM system.columns WHERE database = '%s' AND table = '%s' AND name = '%s'`,
		database, tableName, tsField,
	)
	tsResult, err := client.Query(ctx, tsQuery)
	if err != nil {
		log.Error("failed to query timestamp column type during validation", "error", err, "database", database, "table", tableName, "ts_field", tsField)
		return &ValidationError{Field: "metaTSField", Message: "Failed to query timestamp column type", Err: err}
	}
	if len(tsResult.Logs) == 0 {
		return &ValidationError{Field: "metaTSField", Message: fmt.Sprintf("Timestamp field '%s' not found in table '%s.%s'", tsField, database, tableName)}
	}
	tsType, ok := tsResult.Logs[0]["type"].(string)
	if !ok {
		return &ValidationError{Field: "metaTSField", Message: fmt.Sprintf("Failed to determine type of timestamp field '%s'", tsField)}
	}
	if !strings.HasPrefix(tsType, "DateTime") {
		return &ValidationError{Field: "metaTSField", Message: fmt.Sprintf("Timestamp field '%s' must be DateTime or DateTime64, found %s", tsField, tsType)}
	}

	// --- Severity Field Validation (if provided) ---
	if severityField != "" {
		sevQuery := fmt.Sprintf(
			`SELECT type FROM system.columns WHERE database = '%s' AND table = '%s' AND name = '%s'`,
			database, tableName, severityField,
		)
		sevResult, err := client.Query(ctx, sevQuery)
		if err != nil {
			log.Error("failed to query severity column type during validation", "error", err, "database", database, "table", tableName, "severity_field", severityField)
			return &ValidationError{Field: "metaSeverityField", Message: "Failed to query severity column type", Err: err}
		}
		if len(sevResult.Logs) == 0 {
			return &ValidationError{Field: "metaSeverityField", Message: fmt.Sprintf("Severity field '%s' not found in table '%s.%s'", severityField, database, tableName)}
		}
		sevType, ok := sevResult.Logs[0]["type"].(string)
		if !ok {
			return &ValidationError{Field: "metaSeverityField", Message: fmt.Sprintf("Failed to determine type of severity field '%s'", severityField)}
		}
		if sevType != "String" && !strings.Contains(sevType, "LowCardinality(String)") {
			return &ValidationError{Field: "metaSeverityField", Message: fmt.Sprintf("Severity field '%s' must be String or LowCardinality(String), found %s", severityField, sevType)}
		}
	}

	return nil
}

// validateSourceConfig checks if a source with the same database and table name already exists.
// This is used during source creation to prevent duplicates.
func validateSourceConfig(ctx context.Context, db *sqlite.DB, log *slog.Logger, database, tableName string) error {
	existingSource, err := db.GetSourceByName(ctx, database, tableName)
	if err != nil {
		// If source doesn't exist, that's the desired state for creation.
		if sqlite.IsNotFoundError(err) || sqlite.IsSourceNotFoundError(err) {
			return nil
		}
		// Log unexpected DB errors.
		log.Error("error checking for existing source by name during validation", "error", err, "database", database, "table", tableName)
		return fmt.Errorf("database error checking for existing source: %w", err)
	}

	// If source exists, return a specific conflict error.
	if existingSource != nil {
		return fmt.Errorf("source for database '%s' and table '%s' already exists (ID: %d): %w", database, tableName, existingSource.ID, ErrSourceAlreadyExists)
	}

	return nil // Should technically be unreachable if GetSourceByName behaves correctly
}

// --- Helper Functions (potentially move to a shared utility file later) ---

// checkTableExists attempts a simple query to verify connectivity and table existence.
func checkTableExists(ctx context.Context, client *clickhouse.Client, log *slog.Logger, database, table string) bool {
	if client == nil || database == "" || table == "" {
		return false
	}

	// Use a short timeout for this check
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second) // 3-second timeout for the check
	defer cancel()

	query := fmt.Sprintf("SELECT 1 FROM `%s`.`%s` LIMIT 1", database, table)
	_, err := client.Query(checkCtx, query)
	if err != nil {
		// Log the error for debugging, but don't treat it as a service failure here.
		log.Debug("table existence check failed",
			"database", database,
			"table", table,
			"query", query,
			"error", err.Error(), // Use Error() for concise logging
		)
		return false
	}
	return true
}

// --- Source Management Functions ---

// GetSourcesWithDetails retrieves multiple sources with their full details including schema
// This is more efficient than calling GetSource multiple times for a list of sources
func GetSourcesWithDetails(ctx context.Context, db *sqlite.DB, chDB *clickhouse.Manager, log *slog.Logger, sourceIDs []models.SourceID) ([]*models.Source, error) {
	sources := make([]*models.Source, 0, len(sourceIDs))

	for _, id := range sourceIDs {
		source, err := db.GetSource(ctx, id)
		if err != nil {
			log.Warn("failed to get source", "source_id", id, "error", err)
			continue // Skip this source but continue processing others
		}

		if source == nil {
			log.Warn("source not found", "source_id", id)
			continue
		}

		// Attempt to get the client. If this fails, the source is not connected.
		client, err := chDB.GetConnection(source.ID) // Use GetConnection
		if err != nil {
			log.Debug("failed to get client for source, marking as disconnected",
				"source_id", source.ID,
				"error", err)
			source.IsConnected = false
		} else {
			// Perform a quick check to see if the table is queryable
			source.IsConnected = checkTableExists(ctx, client, log, source.Connection.Database, source.Connection.TableName)

			// Only fetch schema details if source is connected
			if source.IsConnected {
				// Get the comprehensive table schema information
				tableInfo, err := client.GetTableSchema(ctx, source.Connection.Database, source.Connection.TableName)
				if err != nil {
					log.Warn("failed to get table schema",
						"source_id", source.ID,
						"error", err,
					)
				} else {
					// Store basic column information
					source.Columns = tableInfo.Columns

					// Store CREATE statement
					source.Schema = tableInfo.CreateQuery

					// Store enhanced schema information
					source.Engine = tableInfo.Engine
					source.EngineParams = tableInfo.EngineParams
					source.SortKeys = tableInfo.SortKeys

					// Log enhanced schema information
					log.Debug("retrieved table information",
						"source_id", source.ID,
						"column_count", len(tableInfo.Columns),
						"engine", tableInfo.Engine,
						"engine_params", tableInfo.EngineParams,
						"extended_cols", len(tableInfo.ExtColumns), // Assuming ExtColumns exists on TableInfo
						"sort_keys", tableInfo.SortKeys,
					)
				}
			}
		}

		sources = append(sources, source)
	}

	return sources, nil
}

// ListSources returns all sources with their connection status and schema details
func ListSources(ctx context.Context, db *sqlite.DB, chDB *clickhouse.Manager, log *slog.Logger) ([]*models.Source, error) {
	// First get the basic source records from the database
	sources, err := db.ListSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("error listing sources: %w", err)
	}

	// Extract source IDs
	sourceIDs := make([]models.SourceID, 0, len(sources))
	for _, source := range sources {
		sourceIDs = append(sourceIDs, source.ID)
	}

	// Fetch detailed information for all sources
	return GetSourcesWithDetails(ctx, db, chDB, log, sourceIDs)
}

// GetSource retrieves a source by ID including connection status and schema
func GetSource(ctx context.Context, db *sqlite.DB, chDB *clickhouse.Manager, log *slog.Logger, id models.SourceID) (*models.Source, error) {
	source, err := db.GetSource(ctx, id)
	if err != nil {
		// Handle specific not found error from DB layer if possible, otherwise wrap
		if sqlite.IsNotFoundError(err) || sqlite.IsSourceNotFoundError(err) {
			return nil, ErrSourceNotFound
		}
		return nil, fmt.Errorf("error getting source from db: %w", err)
	}

	if source == nil {
		// This case might be handled by the DB layer returning ErrNotFound
		return nil, ErrSourceNotFound
	}

	// Attempt to get the client. If this fails, the source is not connected.
	client, err := chDB.GetConnection(source.ID) // Use GetConnection
	if err != nil {
		log.Debug("failed to get client for source, marking as disconnected",
			"source_id", source.ID,
			"error", err)
		source.IsConnected = false
	} else {
		// Perform a quick check to see if the table is queryable
		source.IsConnected = checkTableExists(ctx, client, log, source.Connection.Database, source.Connection.TableName)

		// Fetch the table schema and CREATE statement only if the source is connected
		if source.IsConnected {
			// Get the table schema (including all metadata)
			tableInfo, err := client.GetTableSchema(ctx, source.Connection.Database, source.Connection.TableName)
			if err != nil {
				log.Warn("failed to get table schema",
					"source_id", source.ID,
					"error", err,
				)
			} else {
				// Store basic columns in the source object
				source.Columns = tableInfo.Columns
				// Store CREATE statement from tableInfo
				source.Schema = tableInfo.CreateQuery
				// Store enhanced schema information
				source.Engine = tableInfo.Engine
				source.EngineParams = tableInfo.EngineParams
				source.SortKeys = tableInfo.SortKeys

				// Log comprehensive table information for debugging
				log.Debug("retrieved table information",
					"source_id", source.ID,
					"column_count", len(tableInfo.Columns),
					"engine", tableInfo.Engine,
					"engine_params", tableInfo.EngineParams,
					"extended_cols", len(tableInfo.ExtColumns), // Assuming ExtColumns exists on TableInfo
					"sort_keys", tableInfo.SortKeys,
					"schema_length", len(tableInfo.CreateQuery),
				)
			}
		}
	}

	log.Debug("source connection status",
		"source_id", source.ID,
		"name", source.Name,
		"database", source.Connection.Database,
		"table", source.Connection.TableName,
		"is_connected", source.IsConnected)

	return source, nil
}

// CreateSource creates a new source, validates connection, and optionally creates the table.
func CreateSource(ctx context.Context, db *sqlite.DB, chDB *clickhouse.Manager, log *slog.Logger, name string, autoCreateTable bool, conn models.ConnectionInfo, description string, ttlDays int, metaTSField string, metaSeverityField string, customSchema string) (*models.Source, error) {
	// 1. Validate input parameters
	if err := validateSourceCreation(name, conn, description, ttlDays, metaTSField, metaSeverityField); err != nil {
		return nil, err
	}

	// 2. Check if source already exists in SQLite (using validateSourceConfig)
	if err := validateSourceConfig(ctx, db, log, conn.Database, conn.TableName); err != nil {
		// This returns ErrSourceAlreadyExists if it exists
		return nil, err
	}

	// 3. Create temporary client to validate ClickHouse connection and potentially create table
	tempSourceForValidation := &models.Source{Connection: conn} // Minimal source for validation
	tempClient, err := chDB.CreateTemporaryClient(tempSourceForValidation)
	if err != nil {
		log.Error("failed to initialize temporary connection during source creation", "error", err, "host", conn.Host, "database", conn.Database)
		return nil, &ValidationError{Field: "connection", Message: "Failed to connect to the database", Err: err}
	}
	defer tempClient.Close()

	// 4. If not auto-creating table, verify it exists and column types are compatible
	if !autoCreateTable {
		// Check table existence (simple query)
		if !checkTableExists(ctx, tempClient, log, conn.Database, conn.TableName) {
			return nil, &ValidationError{Field: "connection.tableName", Message: fmt.Sprintf("Table '%s.%s' not found", conn.Database, conn.TableName)}
		}
		// Validate crucial column types (Timestamp, Severity if provided)
		if err := validateColumnTypes(ctx, tempClient, log, conn.Database, conn.TableName, metaTSField, metaSeverityField); err != nil {
			return nil, err // Return the detailed validation error
		}
	}

	// 5. Create table in ClickHouse if autoCreateTable is true
	if autoCreateTable {
		schemaToExecute := customSchema
		if schemaToExecute == "" {
			schemaToExecute = models.OTELLogsTableSchema // Assuming this constant exists
			schemaToExecute = strings.ReplaceAll(schemaToExecute, "{{database_name}}", conn.Database)
			schemaToExecute = strings.ReplaceAll(schemaToExecute, "{{table_name}}", conn.TableName)
			if ttlDays >= 0 { // Apply TTL only if non-negative (-1 means no TTL)
				schemaToExecute = strings.ReplaceAll(schemaToExecute, "{{ttl_day}}", strconv.Itoa(ttlDays))
			} else {
				// Remove TTL clause entirely if ttlDays is -1
				schemaToExecute = strings.ReplaceAll(schemaToExecute, " TTL toDateTime(timestamp) + INTERVAL {{ttl_day}} DAY", "")
			}
		}
		log.Info("auto creating table", "database", conn.Database, "table", conn.TableName)
		if _, err := tempClient.Query(ctx, schemaToExecute); err != nil {
			log.Error("failed to auto-create table in clickhouse", "error", err, "database", conn.Database, "table", conn.TableName)
			return nil, &ValidationError{Field: "connection.tableName", Message: "Failed to create table in ClickHouse", Err: err}
		}
	}

	// 6. Create the source record in SQLite
	sourceToCreate := &models.Source{
		Name:              name,
		MetaIsAutoCreated: autoCreateTable,
		MetaTSField:       metaTSField,
		MetaSeverityField: metaSeverityField,
		Connection:        conn,
		Description:       description,
		TTLDays:           ttlDays,
		// Schema is not stored in DB, fetched dynamically
		Timestamps: models.Timestamps{
			CreatedAt: time.Now(), // Set by DB ideally, but good practice here too
			UpdatedAt: time.Now(),
		},
	}
	if err := db.CreateSource(ctx, sourceToCreate); err != nil {
		log.Error("failed to create source record in sqlite", "error", err)
		return nil, fmt.Errorf("error saving source configuration: %w", err)
	}

	// 7. Add the newly created source to the ClickHouse connection manager
	if err := chDB.AddSource(sourceToCreate); err != nil {
		log.Error("failed to add source to connection pool after creation, attempting rollback", "error", err, "source_id", sourceToCreate.ID)
		if delErr := db.DeleteSource(ctx, sourceToCreate.ID); delErr != nil {
			log.Error("CRITICAL: failed to delete source from db during rollback", "delete_error", delErr, "source_id", sourceToCreate.ID)
		}
		return nil, fmt.Errorf("failed to establish connection pool for source: %w", err)
	}

	log.Info("source created successfully", "source_id", sourceToCreate.ID, "name", sourceToCreate.Name)
	return sourceToCreate, nil // Return the source with ID populated by CreateSource DB call
}

// UpdateSource updates an existing source's mutable fields (description, ttlDays)
func UpdateSource(ctx context.Context, db *sqlite.DB, log *slog.Logger, id models.SourceID, description string, ttlDays int) (*models.Source, error) {
	// 1. Validate input
	if err := validateSourceUpdate(description, ttlDays); err != nil {
		return nil, err
	}

	// 2. Get existing source
	source, err := db.GetSource(ctx, id)
	if err != nil {
		if sqlite.IsNotFoundError(err) || sqlite.IsSourceNotFoundError(err) {
			return nil, ErrSourceNotFound
		}
		return nil, fmt.Errorf("error getting source: %w", err)
	}

	if source == nil {
		return nil, ErrSourceNotFound
	}

	// 3. Update fields if they have changed
	updated := false
	if description != source.Description {
		source.Description = description
		updated = true
	}
	if ttlDays != source.TTLDays {
		source.TTLDays = ttlDays
		updated = true
	}

	if !updated {
		log.Debug("no update needed for source", "source_id", id)
		return source, nil // Return existing source if no changes
	}

	// source.UpdatedAt = time.Now() // Let DB handle timestamp update

	// 4. Save to database
	if err := db.UpdateSource(ctx, source); err != nil {
		log.Error("failed to update source in sqlite",
			"error", err,
			"source_id", id,
		)
		return nil, fmt.Errorf("error updating source configuration: %w", err)
	}

	// 5. Fetch the updated source again to get potentially updated fields (like updated_at)
	updatedSource, err := db.GetSource(ctx, id)
	if err != nil {
		log.Error("failed to get updated source after successful update", "source_id", id, "error", err)
		// Return the source object we tried to save, but log the fetch error
		return source, nil
	}

	log.Info("source updated successfully", "source_id", updatedSource.ID)
	return updatedSource, nil
}

// DeleteSource deletes a source from SQLite and removes its connection from the manager
func DeleteSource(ctx context.Context, db *sqlite.DB, chDB *clickhouse.Manager, log *slog.Logger, id models.SourceID) error {
	// No input validation needed for ID
	// 1. Validate source exists in SQLite first
	source, err := db.GetSource(ctx, id)
	if err != nil {
		if sqlite.IsNotFoundError(err) || sqlite.IsSourceNotFoundError(err) {
			return ErrSourceNotFound
		}
		return fmt.Errorf("error getting source: %w", err)
	}
	if source == nil { // Should be covered by ErrNotFound check
		return ErrSourceNotFound
	}

	log.Info("deleting source", "source_id", id, "name", source.Name)

	// 2. First remove from ClickHouse manager to prevent any new operations
	if err := chDB.RemoveSource(source.ID); err != nil {
		// Log the error but proceed with deleting from DB
		log.Error("error removing Clickhouse connection, proceeding with DB delete", "source_id", id, "error", err)
		// Optionally return the error if removing the connection is critical:
		// return fmt.Errorf("error removing Clickhouse connection: %w", err)
	}

	// 3. Then remove from database
	if err := db.DeleteSource(ctx, source.ID); err != nil {
		log.Error("failed to remove source from database", "source_id", id, "error", err)
		return fmt.Errorf("error removing from database: %w", err)
	}

	log.Info("source deleted successfully", "source_id", id)
	return nil
}

// CheckSourceConnectionStatus checks the connection status for a given source.
// It returns true if the source is connected and the table is queryable, false otherwise.
func CheckSourceConnectionStatus(ctx context.Context, chDB *clickhouse.Manager, log *slog.Logger, source *models.Source) bool {
	// No input validation needed
	if source == nil {
		return false
	}
	client, err := chDB.GetConnection(source.ID) // Use GetConnection
	if err != nil {
		log.Debug("failed to get client for source status check",
			"source_id", source.ID,
			"error", err)
		return false
	}
	// Use the helper function
	return checkTableExists(ctx, client, log, source.Connection.Database, source.Connection.TableName)
}

// GetSourceHealth retrieves the health status of a source from the ClickHouse manager
func GetSourceHealth(ctx context.Context, db *sqlite.DB, chDB *clickhouse.Manager, id models.SourceID) (models.SourceHealth, error) {
	// No input validation needed for ID
	// 1. Check if source exists in SQLite first to ensure it's a valid source ID
	_, err := db.GetSource(ctx, id)
	if err != nil {
		if sqlite.IsNotFoundError(err) || sqlite.IsSourceNotFoundError(err) {
			return models.SourceHealth{}, ErrSourceNotFound
		}
		return models.SourceHealth{}, fmt.Errorf("error getting source: %w", err)
	}
	// 2. Get health from ClickHouse manager and return it
	health := chDB.GetHealth(id) // Assuming chDB.GetHealth exists and returns models.SourceHealth
	return health, nil
}

// InitializeSource adds a source connection to the ClickHouse manager.
// It assumes the source object contains valid connection details.
func InitializeSource(ctx context.Context, chDB *clickhouse.Manager, source *models.Source) error {
	// No input validation needed
	// The manager's AddSource implicitly handles initialization/connection testing
	return chDB.AddSource(source)
}

// ValidateConnection validates a connection to a ClickHouse database using temporary client
func ValidateConnection(ctx context.Context, chDB *clickhouse.Manager, log *slog.Logger, conn models.ConnectionInfo) (*models.ConnectionValidationResult, error) {
	// 1. Validate connection parameters format
	if err := validateConnection(conn); err != nil {
		return nil, err
	}

	// 2. Create temporary client and attempt connection
	tempSource := &models.Source{Connection: conn}
	client, err := chDB.CreateTemporaryClient(tempSource)
	if err != nil {
		log.Warn("connection validation failed: could not create temporary client", "error", err, "host", conn.Host, "database", conn.Database)
		return nil, &ValidationError{Field: "connection", Message: "Failed to connect to the database", Err: err}
	}
	defer client.Close()

	// 3. If table name provided, check existence
	if conn.TableName != "" {
		if !checkTableExists(ctx, client, log, conn.Database, conn.TableName) {
			return nil, &ValidationError{Field: "tableName", Message: fmt.Sprintf("Connection successful, but table '%s.%s' not found or inaccessible", conn.Database, conn.TableName)}
		}
	}

	log.Debug("connection validation successful", "host", conn.Host, "database", conn.Database, "table", conn.TableName)
	return &models.ConnectionValidationResult{Message: "Connection successful"}, nil
}

// ValidateConnectionWithColumns validates a connection and checks specified column types.
func ValidateConnectionWithColumns(ctx context.Context, chDB *clickhouse.Manager, log *slog.Logger, conn models.ConnectionInfo, tsField, severityField string) (*models.ConnectionValidationResult, error) {
	// 1. Validate connection parameters format
	if err := validateConnection(conn); err != nil {
		return nil, err
	}
	// Table name is required if we need to validate columns
	if conn.TableName == "" {
		return nil, &ValidationError{Field: "tableName", Message: "Table name is required to validate columns"}
	}

	// 2. Create temporary client and attempt connection
	tempSource := &models.Source{Connection: conn}
	client, err := chDB.CreateTemporaryClient(tempSource)
	if err != nil {
		log.Warn("connection validation failed: could not create temporary client", "error", err, "host", conn.Host, "database", conn.Database)
		return nil, &ValidationError{Field: "connection", Message: "Failed to connect to the database", Err: err}
	}
	defer client.Close()

	// 3. Check table existence (implicitly required for column validation)
	if !checkTableExists(ctx, client, log, conn.Database, conn.TableName) {
		return nil, &ValidationError{Field: "tableName", Message: fmt.Sprintf("Connection successful, but table '%s.%s' not found or inaccessible", conn.Database, conn.TableName)}
	}

	// 4. Validate column types
	if err := validateColumnTypes(ctx, client, log, conn.Database, conn.TableName, tsField, severityField); err != nil {
		return nil, err // Return the detailed validation error
	}

	log.Debug("connection and column validation successful", "host", conn.Host, "database", conn.Database, "table", conn.TableName)
	return &models.ConnectionValidationResult{Message: "Connection and column types validated successfully"}, nil
}

// SourceStats represents the combined statistics for a ClickHouse table
// Define locally as it was defined within the source service previously
type SourceStats struct {
	TableStats  *clickhouse.TableStat        `json:"table_stats"`
	ColumnStats []clickhouse.TableColumnStat `json:"column_stats"`
}

// GetSourceStats retrieves statistics for a specific source (ClickHouse table)
func GetSourceStats(ctx context.Context, chDB *clickhouse.Manager, log *slog.Logger, source *models.Source) (*SourceStats, error) {
	if source == nil {
		return nil, ErrSourceNotFound // Or fmt.Errorf("source cannot be nil")
	}

	log.Debug("retrieving source stats",
		"source_id", source.ID,
		"database", source.Connection.Database,
		"table", source.Connection.TableName,
	)

	// Get client for the source
	client, err := chDB.GetConnection(source.ID) // Use GetConnection
	if err != nil {
		log.Error("failed to get client for source stats",
			"error", err,
			"source_id", source.ID,
		)
		return nil, fmt.Errorf("failed to get client for source: %w", err)
	}

	// Create default empty stats in case we can't get real stats
	defaultTableStats := &clickhouse.TableStat{
		Database:     source.Connection.Database,
		Table:        source.Connection.TableName,
		Compressed:   "0B",
		Uncompressed: "0B",
		ComprRate:    0,
		Rows:         0,
		PartCount:    0,
	}

	// Get table stats with fallback to default
	tableStats, err := client.GetTableStats(ctx, source.Connection.Database, source.Connection.TableName)
	if err != nil {
		log.Warn("failed to get table stats, using defaults",
			"error", err,
			"source_id", source.ID,
		)
		// Use default stats instead of returning an error
		tableStats = defaultTableStats
	}

	// Get column stats
	var columnStats []clickhouse.TableColumnStat
	columnStatsResult, err := client.GetTableColumnStats(ctx, source.Connection.Database, source.Connection.TableName)
	if err != nil {
		log.Warn("failed to get column stats, will use schema if available",
			"error", err,
			"source_id", source.ID,
		)
	} else {
		columnStats = columnStatsResult
	}

	// If we have no column stats but we know the columns from the schema,
	// create empty stats for each column for better UX
	if len(columnStats) == 0 && len(source.Columns) > 0 {
		log.Info("no column stats found, creating default empty stats from schema",
			"source_id", source.ID,
			"column_count", len(source.Columns),
		)

		for _, col := range source.Columns {
			columnStats = append(columnStats, clickhouse.TableColumnStat{
				Database: source.Connection.Database,
				Table:    source.Connection.TableName,
				Column:   col.Name,
				// Type: 		  col.Type, // Removed - Field does not exist on clickhouse.TableColumnStat
				Compressed:   "0B",
				Uncompressed: "0B",
				ComprRatio:   0,
				// RowsCount:    0, // These might not be meaningful without stats
				// AvgRowSize:   0,
			})
		}
	}

	stats := &SourceStats{
		TableStats:  tableStats,
		ColumnStats: columnStats,
	}

	log.Debug("successfully retrieved source stats",
		"source_id", source.ID,
		"table_stats_rows", tableStats.Rows,
		"column_stats_count", len(columnStats),
	)

	return stats, nil
}
