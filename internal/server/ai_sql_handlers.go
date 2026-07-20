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
	_ = source

	generatedSQL, err := s.callAIToGenerateSQL(c.Context(), req, schemaJSON, tableName)
	if err != nil {
		return err
	}

	return SendSuccess(c, http.StatusOK, models.GenerateSQLResponse{SQLQuery: generatedSQL})
}

func (s *Server) validateAIConfig() func(*fiber.Ctx) error {
	if !s.config.AI.Enabled {
		return func(c *fiber.Ctx) error {
			return SendErrorWithType(c, http.StatusServiceUnavailable, "AI SQL generation is not enabled", models.GeneralErrorType)
		}
	}
	if s.config.AI.APIKey == "" {
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
		return nil, "", "", SendErrorWithType(c, http.StatusBadRequest, "AI SQL generation is only supported for ClickHouse sources", models.ValidationErrorType)
	}

	if !source.IsConnected {
		return nil, "", "", SendErrorWithType(c, http.StatusServiceUnavailable, "Source is not currently connected", models.ExternalServiceErrorType)
	}
	if len(source.Columns) == 0 {
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

func (s *Server) callAIToGenerateSQL(ctx context.Context, req models.GenerateSQLRequest, schemaJSON, tableName string) (string, error) {
	aiCtx, cancel := context.WithTimeout(ctx, OpenAIRequestTimeout)
	defer cancel()

	aiClient, err := ai.NewClient(ai.ClientOptions{
		APIKey:      s.config.AI.APIKey,
		Model:       s.config.AI.Model,
		MaxTokens:   s.config.AI.MaxTokens,
		Temperature: s.config.AI.Temperature,
		Timeout:     OpenAIRequestTimeout,
		BaseURL:     s.config.AI.BaseURL,
	}, s.log)
	if err != nil {
		return "", fmt.Errorf("failed to initialize AI client: %w", err)
	}

	generatedSQL, err := aiClient.GenerateSQL(aiCtx, req.NaturalLanguageQuery, schemaJSON, tableName, req.CurrentQuery)
	if err != nil {
		if errors.Is(err, ai.ErrInvalidSQLGeneratedByAI) {
			return "", fmt.Errorf("AI could not generate valid SQL: %w", err)
		}
		return "", fmt.Errorf("failed to generate SQL: %w", err)
	}
	return generatedSQL, nil
}
