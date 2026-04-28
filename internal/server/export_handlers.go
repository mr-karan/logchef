package server

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/template"
	"github.com/mr-karan/logchef/pkg/models"
)

type exportLogsRequest struct {
	RawSQL       string                    `json:"raw_sql"`
	Format       string                    `json:"format"`
	Limit        int                       `json:"limit"`
	QueryTimeout *int                      `json:"query_timeout,omitempty"`
	Variables    []models.TemplateVariable `json:"variables,omitempty"`
}

func (s *Server) handleExportLogs(c *fiber.Ctx) error {
	sourceID, err := core.ParseSourceID(c.Params("sourceID"))
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source ID format", models.ValidationErrorType)
	}
	teamID, err := core.ParseTeamID(c.Params("teamID"))
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid team ID format", models.ValidationErrorType)
	}
	user := c.Locals("user").(*models.User)
	if user == nil {
		return SendErrorWithType(c, fiber.StatusUnauthorized, "User context not found", models.AuthenticationErrorType)
	}

	var req exportLogsRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}
	if strings.TrimSpace(req.RawSQL) == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "raw_sql is required", models.ValidationErrorType)
	}

	formatInput := strings.TrimSpace(req.Format)
	format := formatInput
	if format == "" {
		format = inferExportFormatFromAccept(c.Get("Accept"))
	} else {
		normalized, ok := normalizeExplicitExportFormat(format)
		if !ok {
			return SendErrorWithType(c, fiber.StatusBadRequest, "Unsupported export format. Use csv or ndjson.", models.ValidationErrorType)
		}
		format = normalized
	}
	if format == "" || !isExportFormatAllowed(format, s.config.Export.Formats) {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Unsupported export format. Use csv or ndjson.", models.ValidationErrorType)
	}
	if format == "csv" {
		return SendErrorWithType(c, fiber.StatusBadRequest,
			"Streaming CSV exports are not supported. Create an export job instead.",
			models.ValidationErrorType)
	}

	if req.QueryTimeout == nil {
		defaultTimeout := s.config.Export.DefaultTimeoutSeconds
		req.QueryTimeout = &defaultTimeout
	}
	if err := models.ValidateQueryTimeout(req.QueryTimeout); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	if s.config.Export.MaxTimeoutSeconds > 0 && *req.QueryTimeout > s.config.Export.MaxTimeoutSeconds {
		return SendErrorWithType(c, fiber.StatusBadRequest,
			fmt.Sprintf("Query timeout cannot exceed %d seconds for Download", s.config.Export.MaxTimeoutSeconds),
			models.ValidationErrorType)
	}

	processedSQL := req.RawSQL
	if len(req.Variables) > 0 {
		vars := make([]template.Variable, len(req.Variables))
		for i, v := range req.Variables {
			vars[i] = template.Variable{
				Name:  v.Name,
				Type:  template.VariableType(v.Type),
				Value: v.Value,
			}
		}
		substituted, err := template.SubstituteVariables(req.RawSQL, vars)
		if err != nil {
			return SendErrorWithType(c, fiber.StatusBadRequest,
				fmt.Sprintf("Variable substitution failed: %v", err), models.ValidationErrorType)
		}
		processedSQL = substituted
	}

	source, err := s.sqlite.GetSource(c.Context(), sourceID)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
	}
	client, err := s.clickhouse.GetConnection(sourceID)
	if err != nil {
		s.log.Error("failed to get clickhouse client for export", "source_id", sourceID, "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to get source connection", models.DatabaseErrorType)
	}

	exportLimit := req.Limit
	if exportLimit <= 0 {
		exportLimit = s.config.Export.MaxRows
	}
	if exportLimit > s.config.Export.MaxRows {
		exportLimit = s.config.Export.MaxRows
	}

	qb := clickhouse.NewExtendedQueryBuilder(source.GetFullTableName(), s.config.Export.MaxRows)
	buildResult, err := qb.BuildRawQueryWithLimitPolicy(processedSQL, req.Limit, exportLimit, s.config.Export.MaxRows)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, fmt.Sprintf("Invalid request: %v", err), models.ValidationErrorType)
	}

	queryID := uuid.New().String()
	streamCtx, cancel := context.WithCancel(c.Context())
	if err := queryTracker.StartQueryWithID(
		queryID,
		QueryClassExport,
		user.ID,
		sourceID,
		teamID,
		req.RawSQL,
		cancel,
		s.config.Export.MaxConcurrentPerUser,
		s.config.Export.MaxConcurrentGlobal,
	); err != nil {
		cancel()
		var admissionErr *QueryAdmissionError
		if errors.As(err, &admissionErr) {
			return SendErrorWithType(c, fiber.StatusTooManyRequests, admissionErr.Message, models.ValidationErrorType)
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to track export query", models.GeneralErrorType)
	}

	opts := clickhouse.QueryOptions{
		TimeoutSeconds: req.QueryTimeout,
		Settings: map[string]interface{}{
			"max_execution_time":   *req.QueryTimeout,
			"max_result_rows":      buildResult.AppliedLimit,
			"result_overflow_mode": "break",
		},
		LimitApplied: buildResult.AppliedLimit,
		MaxRows:      buildResult.AppliedLimit,
	}

	contentType, extension := exportContentType(format)
	filename := fmt.Sprintf("logchef-%s.%s", time.Now().UTC().Format("20060102-150405"), extension)
	c.Status(fiber.StatusOK)
	c.Set("Content-Type", contentType)
	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Set("X-LogChef-Query-ID", queryID)
	c.Set("X-LogChef-Limit-Applied", strconv.Itoa(buildResult.AppliedLimit))

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer cancel()
		defer queryTracker.RemoveQuery(queryID)

		writer := newExportRowWriter(format, w, queryID, buildResult.AppliedLimit)
		stats, err := client.QueryStream(streamCtx, buildResult.SQL, opts, writer)
		if err != nil {
			s.log.Error("failed to stream export", "error", err, "source_id", sourceID, "query_id", queryID)
			_ = writer.WriteError(err)
			_ = w.Flush()
			return
		}
		s.log.Info("query.export",
			"user", user.Email,
			"team_id", teamID,
			"source_id", sourceID,
			"query_id", queryID,
			"format", format,
			"rows", stats.RowsReturned,
			"duration_ms", stats.ExecutionTimeMs,
			"limit_applied", stats.LimitApplied,
			"truncated", stats.Truncated,
		)
		_ = w.Flush()
	})

	return nil
}

func normalizeExplicitExportFormat(format string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "csv":
		return "csv", true
	case "ndjson", "jsonl":
		return "ndjson", true
	default:
		return "", false
	}
}

func inferExportFormatFromAccept(accept string) string {
	accept = strings.ToLower(accept)
	switch {
	case strings.Contains(accept, "text/csv"):
		return "csv"
	case strings.Contains(accept, "application/x-ndjson"), strings.Contains(accept, "application/jsonl"):
		return "ndjson"
	default:
		return "ndjson"
	}
}

func isExportFormatAllowed(format string, allowed []string) bool {
	for _, candidate := range allowed {
		if strings.EqualFold(strings.TrimSpace(candidate), format) {
			return true
		}
	}
	return false
}

func exportContentType(format string) (contentType string, extension string) {
	if format == "csv" {
		return "text/csv; charset=utf-8", "csv"
	}
	return "application/x-ndjson; charset=utf-8", "ndjson"
}

type exportRowWriter struct {
	format       string
	out          *bufio.Writer
	csv          *csv.Writer
	columns      []models.ColumnInfo
	queryID      string
	limitApplied int
	rowsWritten  int
}

func newExportRowWriter(format string, out *bufio.Writer, queryID string, limitApplied int) *exportRowWriter {
	w := &exportRowWriter{
		format:       format,
		out:          out,
		queryID:      queryID,
		limitApplied: limitApplied,
	}
	if format == "csv" {
		w.csv = csv.NewWriter(out)
	}
	return w
}

func (w *exportRowWriter) Begin(columns []models.ColumnInfo) error {
	w.columns = append([]models.ColumnInfo(nil), columns...)
	if w.format == "csv" {
		header := make([]string, len(columns))
		for i, col := range columns {
			header[i] = col.Name
		}
		if err := w.csv.Write(header); err != nil {
			return err
		}
		w.csv.Flush()
		return w.csv.Error()
	}
	return w.writeNDJSON(map[string]interface{}{
		"type":          "meta",
		"query_id":      w.queryID,
		"columns":       columns,
		"limit_applied": w.limitApplied,
	})
}

func (w *exportRowWriter) WriteRow(row map[string]interface{}) error {
	w.rowsWritten++
	if w.format == "csv" {
		record := make([]string, len(w.columns))
		for i, col := range w.columns {
			record[i] = csvValue(row[col.Name])
		}
		if err := w.csv.Write(record); err != nil {
			return err
		}
		if w.rowsWritten%100 == 0 {
			w.csv.Flush()
			if err := w.csv.Error(); err != nil {
				return err
			}
			return w.out.Flush()
		}
		return nil
	}
	err := w.writeNDJSON(map[string]interface{}{
		"type": "row",
		"row":  row,
	})
	if err == nil && w.rowsWritten%100 == 0 {
		return w.out.Flush()
	}
	return err
}

func (w *exportRowWriter) Finish(stats models.QueryStats) error {
	if w.format == "csv" {
		w.csv.Flush()
		if err := w.csv.Error(); err != nil {
			return err
		}
		return w.out.Flush()
	}
	if err := w.writeNDJSON(map[string]interface{}{
		"type":  "stats",
		"stats": stats,
	}); err != nil {
		return err
	}
	return w.out.Flush()
}

func (w *exportRowWriter) WriteError(err error) error {
	if w.format == "csv" {
		return fmt.Errorf("streaming csv export failed: %w", err)
	}
	return w.writeNDJSON(map[string]interface{}{
		"type":  "error",
		"error": err.Error(),
	})
}

func (w *exportRowWriter) writeNDJSON(v interface{}) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := w.out.Write(payload); err != nil {
		return err
	}
	return w.out.WriteByte('\n')
}

func csvValue(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case time.Time:
		return v.Format(time.RFC3339Nano)
	case []byte:
		return string(v)
	case fmt.Stringer:
		return v.String()
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case int:
		return strconv.Itoa(v)
	case int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return fmt.Sprint(v)
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(encoded)
	}
}
