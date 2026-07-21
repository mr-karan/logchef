package server

// Natural-language-to-SQL generation handler and its helpers.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/gofiber/fiber/v2"

	"github.com/mr-karan/logchef/internal/ai"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// handleGenerateAISQL handles the generation of SQL from natural language queries
func (s *Server) handleGenerateAISQL(c *fiber.Ctx) error {
	if err := s.validateAIConfig(); err != nil {
		return err(c)
	}

	sourceID, teamID, err := s.parseSourceTeamIDs(c)
	if err != nil {
		return err
	}

	user := c.Locals("user").(*models.User)
	if user == nil {
		return SendErrorWithType(c, http.StatusUnauthorized, "Unauthorized", models.AuthenticationErrorType)
	}

	hasAccess, accessErr := core.UserHasAccessToTeamSource(c.Context(), s.sqlite, s.log, user.ID, teamID, sourceID)
	if accessErr != nil {
		return SendErrorWithType(c, http.StatusInternalServerError, "Failed to verify source access", models.GeneralErrorType)
	}
	if !hasAccess {
		return SendErrorWithType(c, http.StatusForbidden, "You don't have access to this source", models.AuthorizationErrorType)
	}

	var req models.GenerateSQLRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, http.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}
	if req.NaturalLanguageQuery == "" {
		return SendErrorWithType(c, http.StatusBadRequest, "Natural language query is required", models.ValidationErrorType)
	}

	source, schemaJSON, tableName, err := s.getSourceSchemaForAI(c, sourceID)
	if err != nil {
		return err
	}

	// The target language is chosen deterministically from the source backend
	// and the editor mode. The model never chooses the backend.
	target := deriveAITarget(source.SourceType, req.Mode)

	generatedQuery, err := s.callAIToGenerateSQL(c.Context(), req, target, schemaJSON, tableName)
	if err != nil {
		return err
	}

	return SendSuccess(c, http.StatusOK, models.GenerateSQLResponse{
		SQLQuery: generatedQuery,
		Language: string(target),
	})
}

// deriveAITarget maps the source backend and editor mode to the concrete AI
// target language, deterministically and server-side. An empty/absent or
// unrecognized mode is treated as "native" for backward compatibility, so an
// existing ClickHouse caller still gets ClickHouse SQL.
//
//	(ClickHouse,   native) -> ClickHouse SQL
//	(ClickHouse,   logchefql) -> LogchefQL
//	(VictoriaLogs, native) -> LogsQL
//	(VictoriaLogs, logchefql) -> LogchefQL
func deriveAITarget(sourceType models.SourceType, mode string) ai.TargetLanguage {
	if mode == string(models.QueryLanguageLogchefQL) {
		return ai.TargetLogchefQL
	}
	if sourceType == models.SourceTypeVictoriaLogs {
		return ai.TargetLogsQL
	}
	return ai.TargetClickHouseSQL
}

func (s *Server) validateAIConfig() func(*fiber.Ctx) error {
	if !s.config.AI.Enabled {
		return func(c *fiber.Ctx) error {
			return SendErrorWithType(c, http.StatusServiceUnavailable, "AI SQL generation is not enabled", models.GeneralErrorType)
		}
	}
	// The openai provider (default) requires an API key; the bedrock provider
	// authenticates via the AWS credential chain instead.
	if (s.config.AI.Provider == "" || s.config.AI.Provider == ai.ProviderOpenAI) && s.config.AI.APIKey == "" {
		return func(c *fiber.Ctx) error {
			return SendErrorWithType(c, http.StatusServiceUnavailable, "AI SQL generation is not configured (missing API key)", models.GeneralErrorType)
		}
	}
	return nil
}

func (s *Server) parseSourceTeamIDs(c *fiber.Ctx) (models.SourceID, models.TeamID, error) {
	sourceID, err := core.ParseSourceID(c.Params("sourceID"))
	if err != nil {
		return 0, 0, SendErrorWithType(c, http.StatusBadRequest, "Invalid source ID", models.ValidationErrorType)
	}
	teamID, err := core.ParseTeamID(c.Params("teamID"))
	if err != nil {
		return 0, 0, SendErrorWithType(c, http.StatusBadRequest, "Invalid team ID", models.ValidationErrorType)
	}
	return sourceID, teamID, nil
}

func (s *Server) getSourceSchemaForAI(c *fiber.Ctx, sourceID models.SourceID) (source *models.Source, schemaJSON, tableName string, err error) {
	source, err = core.GetSource(c.Context(), s.datasources, sourceID)
	if err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return nil, "", "", SendErrorWithType(c, http.StatusNotFound, "Source not found", models.NotFoundErrorType)
		}
		return nil, "", "", SendErrorWithType(c, http.StatusInternalServerError, "Failed to get source", models.DatabaseErrorType)
	}
	if source == nil {
		return nil, "", "", SendErrorWithType(c, http.StatusNotFound, "Source not found", models.NotFoundErrorType)
	}
	if !source.HasCapability(string(datasource.CapabilityAISQLGeneration)) {
		return nil, "", "", SendErrorWithType(c, http.StatusBadRequest, "AI query generation is not supported for this source", models.ValidationErrorType)
	}

	if !source.IsConnected {
		return nil, "", "", SendErrorWithType(c, http.StatusServiceUnavailable, "Source is not currently connected", models.ExternalServiceErrorType)
	}
	// ClickHouse SQL generation needs a concrete schema. VictoriaLogs discovers
	// fields dynamically and may legitimately have a sparse/empty column set, so
	// we proceed for VL and let the prompt work from whatever schema is available.
	if source.SourceType != models.SourceTypeVictoriaLogs && len(source.Columns) == 0 {
		return nil, "", "", SendErrorWithType(c, http.StatusInternalServerError, "Failed to get source schema", models.ExternalServiceErrorType)
	}

	schemaJSON = formatSchemaForAI(source)
	tableName = source.GetFullTableName()
	return source, schemaJSON, tableName, nil
}

func formatSchemaForAI(source *models.Source) string {
	columns := make([]map[string]interface{}, 0, len(source.Columns))
	for _, col := range source.Columns {
		columns = append(columns, map[string]interface{}{"name": col.Name, "type": col.Type})
	}
	if len(source.SortKeys) > 0 {
		columns = append(columns, map[string]interface{}{
			"name": "_sort_keys", "keys": source.SortKeys,
			"note": "The columns above are sort keys. Queries filtered by these columns will be faster.",
		})
	}
	schemaJSON, _ := json.MarshalIndent(columns, "", "  ")
	return string(schemaJSON)
}

func (s *Server) callAIToGenerateSQL(ctx context.Context, req models.GenerateSQLRequest, target ai.TargetLanguage, schemaJSON, tableName string) (string, error) {
	aiCtx, cancel := context.WithTimeout(ctx, AIRequestTimeout)
	defer cancel()

	provider, err := ai.NewProvider(aiCtx, ai.ProviderConfig{
		Provider: s.config.AI.Provider,
		APIKey:   s.config.AI.APIKey,
		BaseURL:  s.config.AI.BaseURL,
		Region:   s.config.AI.Region,
	}, s.log)
	if err != nil {
		return "", fmt.Errorf("failed to initialize AI provider: %w", err)
	}

	gen := ai.NewGenerator(provider, ai.GeneratorConfig{
		Model:       s.config.AI.Model,
		MaxTokens:   s.config.AI.MaxTokens,
		Temperature: s.config.AI.Temperature,
		Timeout:     AIRequestTimeout,
	}, s.log)

	generatedQuery, err := gen.GenerateQuery(aiCtx, ai.GenerateQueryInput{
		Target:               target,
		NaturalLanguageQuery: req.NaturalLanguageQuery,
		Schema:               schemaJSON,
		TableName:            tableName,
		CurrentQuery:         req.CurrentQuery,
	})
	if err != nil {
		if errors.Is(err, ai.ErrInvalidSQLGeneratedByAI) {
			return "", fmt.Errorf("AI could not generate a valid query: %w", err)
		}
		return "", fmt.Errorf("failed to generate query: %w", err)
	}
	return generatedQuery, nil
}
