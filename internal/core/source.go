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
)

// ErrSourceNotFound is returned when a source is not found
var ErrSourceNotFound = fmt.Errorf("source not found")
var ErrSourceAlreadyExists = fmt.Errorf("source already exists")

// --- Source Validation Functions ---

// validateSourceCreation validates source creation parameters.
func validateSourceCreation(name string, conn models.ConnectionInfo, description string, ttlDays int, metaTSField, metaSeverityField string) error {
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
			source.IsConnected = false
		} else {
			// Use integrated Ping method to check both connection and table existence
			source.IsConnected = client.Ping(ctx, source.Connection.Database, source.Connection.TableName) == nil

			// Only fetch schema details if source is connected
			if source.IsConnected {
				// Get the comprehensive table schema information
				tableInfo, err := client.GetTableInfo(ctx, source.Connection.Database, source.Connection.TableName)
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
				}
			}
		}

		sources = append(sources, source)
	}

	return sources, nil
}

// ListSources returns all sources with basic connection status but without schema details.
// This is optimized for performance in list views where the schema isn't needed.
func ListSources(ctx context.Context, db *sqlite.DB, chDB *clickhouse.Manager, log *slog.Logger) ([]*models.Source, error) {
	// Get the basic source records from the database
	sources, err := db.ListSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("error listing sources: %w", err)
	}

	// Check connection status for each source
	for i := range sources {
		source := sources[i]
		if source == nil {
			continue
		}

		// Default to not connected
		source.IsConnected = false

		// Attempt to get client and perform a health check that includes table verification
		client, err := chDB.GetConnection(source.ID)
		if err == nil {
			// Use integrated Ping method that checks both connection and table existence
			source.IsConnected = client.Ping(ctx, source.Connection.Database, source.Connection.TableName) == nil
		}

		// Clear schema-related fields to avoid sending unnecessary data
		source.Columns = nil
		source.Schema = ""
		source.Engine = ""
		source.EngineParams = nil
		source.SortKeys = nil
	}

	return sources, nil
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
		source.IsConnected = false
	} else {
		// Use integrated Ping method to check both connection and table existence
		source.IsConnected = client.Ping(ctx, source.Connection.Database, source.Connection.TableName) == nil

		// Fetch the table schema and CREATE statement only if the source is connected
		if source.IsConnected {
			// Get the table schema (including all metadata)
			tableInfo, err := client.GetTableInfo(ctx, source.Connection.Database, source.Connection.TableName)
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
			}
		}
	}

	return source, nil
}

// CreateSource creates a new source, validates connection, and optionally creates the table.
func CreateSource(ctx context.Context, db *sqlite.DB, chDB *clickhouse.Manager, log *slog.Logger, name string, autoCreateTable bool, conn models.ConnectionInfo, description string, ttlDays int, metaTSField, metaSeverityField, customSchema string) (*models.Source, error) {
	// 1. Validate input parameters
	if err := validateSourceCreation(name, conn, description, ttlDays, metaTSField, metaSeverityField); err != nil {
		return nil, err
	}

	// 2. Check if source already exists in SQLite (using validateSourceConfig)
	if err := validateSourceConfig(ctx, db, log, conn.Database, conn.TableName); err != nil {
		// This returns ErrSourceAlreadyExists if it exists
		return nil, err
	}

	tempSourceForValidation := &models.Source{Connection: conn}
	tempClient, err := chDB.CreateTemporaryClient(ctx, tempSourceForValidation)
	if err != nil {
		log.Error("failed to initialize temporary connection during source creation", "error", err, "host", conn.Host, "database", conn.Database)
		return nil, &ValidationError{Field: "connection", Message: "Failed to connect to the database", Err: err}
	}
	defer tempClient.Close()

	// 4. If not auto-creating table, verify it exists and column types are compatible
	if !autoCreateTable {
		// Check table existence using client.Ping directly
		if tempClient.Ping(ctx, conn.Database, conn.TableName) != nil {
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
	if err := chDB.AddSource(ctx, sourceToCreate); err != nil {
		log.Error("failed to add source to connection pool after creation, attempting rollback", "error", err, "source_id", sourceToCreate.ID)
		if delErr := db.DeleteSource(ctx, sourceToCreate.ID); delErr != nil {
			log.Error("CRITICAL: failed to delete source from db during rollback", "delete_error", delErr, "source_id", sourceToCreate.ID)
		}
		return nil, fmt.Errorf("failed to establish connection pool for source: %w", err)
	}

	log.Info("source created successfully", "source_id", sourceToCreate.ID, "name", sourceToCreate.Name)
	return sourceToCreate, nil // Return the source with ID populated by CreateSource DB call
}

// applySourceMetadataUpdates applies name, description, and TTL updates to a source.
// Returns true if any field was updated.
func applySourceMetadataUpdates(source *models.Source, req *models.UpdateSourceRequest) (bool, error) {
	updated := false

	if req.Name != nil && *req.Name != source.Name {
		if *req.Name == "" {
			return false, &ValidationError{Field: "name", Message: "name cannot be empty"}
		}
		source.Name = *req.Name
		updated = true
	}

	if req.Description != nil && *req.Description != source.Description {
		if len(*req.Description) > 500 {
			return false, &ValidationError{Field: "description", Message: "description must not exceed 500 characters"}
		}
		source.Description = *req.Description
		updated = true
	}

	if req.TTLDays != nil && *req.TTLDays != source.TTLDays {
		if *req.TTLDays < -1 {
			return false, &ValidationError{Field: "ttl_days", Message: "TTL days must be -1 (no TTL) or a non-negative number"}
		}
		source.TTLDays = *req.TTLDays
		updated = true
	}

	return updated, nil
}

// applySourceConnectionUpdates applies connection field updates to a source.
// Returns the new connection info and whether any connection field changed.
func applySourceConnectionUpdates(source *models.Source, req *models.UpdateSourceRequest) (models.ConnectionInfo, bool) {
	newConn := source.Connection
	changed := false

	if req.Host != nil && *req.Host != source.Connection.Host {
		newConn.Host = *req.Host
		changed = true
	}
	if req.Username != nil && *req.Username != source.Connection.Username {
		newConn.Username = *req.Username
		changed = true
	}
	if req.Password != nil {
		newConn.Password = *req.Password
		changed = true
	}
	if req.Database != nil && *req.Database != source.Connection.Database {
		newConn.Database = *req.Database
		changed = true
	}
	if req.TableName != nil && *req.TableName != source.Connection.TableName {
		newConn.TableName = *req.TableName
		changed = true
	}

	return newConn, changed
}

// validateAndTestConnection validates connection parameters and tests connectivity.
func validateAndTestConnection(ctx context.Context, chDB *clickhouse.Manager, log *slog.Logger, conn models.ConnectionInfo) error {
	if err := validateConnection(conn); err != nil {
		return err
	}

	tempSource := &models.Source{Connection: conn}
	tempClient, err := chDB.CreateTemporaryClient(ctx, tempSource)
	if err != nil {
		log.Error("failed to validate new connection", "error", err, "host", conn.Host)
		return &ValidationError{Field: "connection", Message: "Failed to connect with new credentials", Err: err}
	}
	defer tempClient.Close()

	if err := tempClient.Ping(ctx, conn.Database, conn.TableName); err != nil {
		return &ValidationError{Field: "connection", Message: fmt.Sprintf("Table '%s.%s' not accessible with new credentials", conn.Database, conn.TableName), Err: err}
	}

	return nil
}

// refreshSourceConnectionPool removes old connection and adds new one to the pool.
func refreshSourceConnectionPool(ctx context.Context, chDB *clickhouse.Manager, log *slog.Logger, id models.SourceID, source *models.Source) error {
	if err := chDB.RemoveSource(id); err != nil {
		log.Warn("failed to remove old connection from pool", "error", err, "source_id", id)
	}
	if err := chDB.AddSource(ctx, source); err != nil {
		log.Error("failed to add updated source to connection pool", "error", err, "source_id", id)
		return fmt.Errorf("failed to refresh connection pool: %w", err)
	}
	log.Info("connection pool refreshed for source", "source_id", id)
	return nil
}

// UpdateSource updates an existing source's mutable fields using partial update semantics.
// Connection changes trigger re-validation and pool refresh.
func UpdateSource(ctx context.Context, db *sqlite.DB, chDB *clickhouse.Manager, log *slog.Logger, id models.SourceID, req *models.UpdateSourceRequest) (*models.Source, error) {
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

	// Apply metadata updates
	metadataUpdated, err := applySourceMetadataUpdates(source, req)
	if err != nil {
		return nil, err
	}

	// Apply connection updates
	newConn, connectionChanged := applySourceConnectionUpdates(source, req)

	// Validate and test new connection if changed
	if connectionChanged {
		if err := validateAndTestConnection(ctx, chDB, log, newConn); err != nil {
			return nil, err
		}
		source.Connection = newConn
	}

	if !metadataUpdated && !connectionChanged {
		return source, nil
	}

	// Save to database
	if err := db.UpdateSource(ctx, source); err != nil {
		log.Error("failed to update source in sqlite", "error", err, "source_id", id)
		return nil, fmt.Errorf("error updating source configuration: %w", err)
	}

	// Refresh connection pool if connection changed
	if connectionChanged {
		if err := refreshSourceConnectionPool(ctx, chDB, log, id, source); err != nil {
			return nil, err
		}
	}

	// Fetch updated source
	updatedSource, err := db.GetSource(ctx, id)
	if err != nil {
		log.Error("failed to get updated source after successful update", "source_id", id, "error", err)
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
		return false
	}
	// Use the integrated Ping method
	return client.Ping(ctx, source.Connection.Database, source.Connection.TableName) == nil
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
	health := chDB.GetHealth(ctx, id)
	return health, nil
}

// InitializeSource adds a source connection to the ClickHouse manager.
// It assumes the source object contains valid connection details.
func InitializeSource(ctx context.Context, chDB *clickhouse.Manager, source *models.Source) error {
	return chDB.AddSource(ctx, source)
}

// ValidateConnection validates a connection to a ClickHouse database using temporary client
func ValidateConnection(ctx context.Context, chDB *clickhouse.Manager, log *slog.Logger, conn models.ConnectionInfo) (*models.ConnectionValidationResult, error) {
	// 1. Validate connection parameters format
	if err := validateConnection(conn); err != nil {
		return nil, err
	}

	tempSource := &models.Source{Connection: conn}
	client, err := chDB.CreateTemporaryClient(ctx, tempSource)
	if err != nil {
		log.Warn("connection validation failed: could not create temporary client", "error", err, "host", conn.Host, "database", conn.Database)
		return nil, &ValidationError{Field: "connection", Message: "Failed to connect to the database", Err: err}
	}
	defer client.Close()
	if conn.TableName != "" {
		// Check table existence using client.Ping directly
		if client.Ping(ctx, conn.Database, conn.TableName) != nil {
			return nil, &ValidationError{Field: "tableName", Message: fmt.Sprintf("Connection successful, but table '%s.%s' not found or inaccessible", conn.Database, conn.TableName)}
		}
	}

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

	tempSource := &models.Source{Connection: conn}
	client, err := chDB.CreateTemporaryClient(ctx, tempSource)
	if err != nil {
		log.Warn("connection validation failed: could not create temporary client", "error", err, "host", conn.Host, "database", conn.Database)
		return nil, &ValidationError{Field: "connection", Message: "Failed to connect to the database", Err: err}
	}
	defer client.Close()
	// Check table existence using client.Ping directly
	if client.Ping(ctx, conn.Database, conn.TableName) != nil {
		return nil, &ValidationError{Field: "tableName", Message: fmt.Sprintf("Connection successful, but table '%s.%s' not found or inaccessible", conn.Database, conn.TableName)}
	}

	// 4. Validate column types
	if err := validateColumnTypes(ctx, client, log, conn.Database, conn.TableName, tsField, severityField); err != nil {
		return nil, err // Return the detailed validation error
	}

	return &models.ConnectionValidationResult{Message: "Connection and column types validated successfully"}, nil
}

// extractTTLFromCreateQuery extracts TTL information from a CREATE TABLE statement
func extractTTLFromCreateQuery(createQuery string) string {
	if createQuery == "" {
		return ""
	}

	// Look for TTL keyword (case insensitive)
	ttlIndex := strings.Index(strings.ToUpper(createQuery), " TTL ")
	if ttlIndex == -1 {
		return ""
	}

	// Extract everything after "TTL "
	ttlPart := createQuery[ttlIndex+5:] // +5 to skip " TTL "
	return parseTTLExpression(ttlPart)
}

// parseTTLExpression parses the TTL expression from a TTL clause string
// Handles ClickHouse TTL syntax: expression [DELETE|TO DISK|TO VOLUME] [SETTINGS]
func parseTTLExpression(ttlPart string) string {
	if ttlPart == "" {
		return ""
	}

	// Find the end of the TTL expression by looking for common terminators
	// while properly handling nested parentheses
	parenCount := 0
	endIndex := len(ttlPart)

	for i, char := range ttlPart {
		switch char {
		case '(':
			parenCount++
		case ')':
			parenCount--
			// If we're back to 0 parentheses, check if this might be the end
			if parenCount == 0 {
				remaining := strings.TrimSpace(ttlPart[i+1:])
				upperRemaining := strings.ToUpper(remaining)
				if strings.HasPrefix(upperRemaining, "SETTINGS") ||
					strings.HasPrefix(upperRemaining, "DELETE") ||
					strings.HasPrefix(upperRemaining, "TO DISK") ||
					strings.HasPrefix(upperRemaining, "TO VOLUME") ||
					remaining == "" {
					endIndex = i
					break
				}
			}
		case ' ':
			if parenCount == 0 {
				// Look for keywords that would end the TTL expression
				remaining := strings.TrimSpace(ttlPart[i:])
				upperRemaining := strings.ToUpper(remaining)
				if strings.HasPrefix(upperRemaining, "SETTINGS") ||
					strings.HasPrefix(upperRemaining, "DELETE") ||
					strings.HasPrefix(upperRemaining, "TO DISK") ||
					strings.HasPrefix(upperRemaining, "TO VOLUME") {
					endIndex = i
					break
				}
			}
		}
	}

	// Extract and clean the TTL expression
	ttlExpr := strings.TrimSpace(ttlPart[:endIndex])
	ttlExpr = strings.TrimRight(ttlExpr, ",")

	return ttlExpr
}

// SourceStats represents the combined statistics for a ClickHouse table
// Use types directly from the clickhouse package
type SourceStats struct {
	TableStats  *clickhouse.TableStat        `json:"table_stats"`   // Use pointer to allow nil if stats fail completely
	ColumnStats []clickhouse.TableColumnStat `json:"column_stats"`  // Slice is sufficient, empty if stats fail
	TableInfo   *clickhouse.TableInfo        `json:"table_info"`    // Schema, engine, and metadata information
	TTL         string                       `json:"ttl,omitempty"` // TTL information extracted from CREATE TABLE
}

// GetSourceStats retrieves statistics for a specific source (ClickHouse table)
func GetSourceStats(ctx context.Context, chDB *clickhouse.Manager, log *slog.Logger, source *models.Source) (*SourceStats, error) {
	if source == nil {
		return nil, ErrSourceNotFound
	}

	client, err := chDB.GetConnection(source.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get client for source %d: %w", source.ID, err)
	}

	tableInfo, _ := client.GetTableInfo(ctx, source.Connection.Database, source.Connection.TableName)

	ttlExpr := extractTTLFromTableInfo(ctx, client, tableInfo)
	statsDB, statsTable := getStatsTableLocation(source, tableInfo)

	tableStats, _ := client.TableStats(ctx, statsDB, statsTable)
	columnStats, _ := client.ColumnStats(ctx, statsDB, statsTable)
	columnStats = ensureColumnStats(columnStats, source)

	return &SourceStats{
		TableStats:  tableStats,
		ColumnStats: columnStats,
		TableInfo:   tableInfo,
		TTL:         ttlExpr,
	}, nil
}

func extractTTLFromTableInfo(ctx context.Context, client *clickhouse.Client, tableInfo *clickhouse.TableInfo) string {
	if tableInfo == nil || tableInfo.CreateQuery == "" {
		return ""
	}

	if tableInfo.Engine == "Distributed" && len(tableInfo.EngineParams) >= 3 {
		localDB, localTable := tableInfo.EngineParams[1], tableInfo.EngineParams[2]
		localTableInfo, err := client.GetTableInfo(ctx, localDB, localTable)
		if err == nil && localTableInfo != nil {
			return extractTTLFromCreateQuery(localTableInfo.CreateQuery)
		}
		return ""
	}
	return extractTTLFromCreateQuery(tableInfo.CreateQuery)
}

func getStatsTableLocation(source *models.Source, tableInfo *clickhouse.TableInfo) (database, table string) {
	if tableInfo != nil && tableInfo.Engine == "Distributed" && len(tableInfo.EngineParams) >= 3 {
		return tableInfo.EngineParams[1], tableInfo.EngineParams[2]
	}
	return source.Connection.Database, source.Connection.TableName
}

func ensureColumnStats(columnStats []clickhouse.TableColumnStat, source *models.Source) []clickhouse.TableColumnStat {
	if len(columnStats) > 0 {
		return columnStats
	}
	if len(source.Columns) == 0 {
		return []clickhouse.TableColumnStat{}
	}

	stats := make([]clickhouse.TableColumnStat, 0, len(source.Columns))
	for _, col := range source.Columns {
		stats = append(stats, clickhouse.TableColumnStat{
			Database:     source.Connection.Database,
			Table:        source.Connection.TableName,
			Column:       col.Name,
			Compressed:   "N/A",
			Uncompressed: "N/A",
		})
	}
	return stats
}
