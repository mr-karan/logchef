package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gofiber/websocket/v2"
	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"
)

// handleLiveQueryRequest handles a WebSocket message containing a log query request,
// executes the query, and sends the result back to the client.
func (s *Server) handleLiveQueryRequest(conn *websocket.Conn, sourceID models.SourceID, msg []byte) {
	var req models.APIQueryRequest
	if err := json.Unmarshal(msg, &req); err != nil {
		s.log.Warn("Invalid WebSocket query request format", "error", err)
		conn.WriteMessage(websocket.TextMessage, []byte("Invalid request format"))
		return
	}

	// Set default values if not provided
	if req.Limit <= 0 {
		req.Limit = 100
	}
	if req.QueryTimeout == nil {
		defaultTimeout := models.DefaultQueryTimeoutSeconds
		req.QueryTimeout = &defaultTimeout
	}

	if err := models.ValidateQueryTimeout(req.QueryTimeout); err != nil {
		s.log.Warn("Invalid query timeout", "error", err)
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Invalid query timeout: %v", err)))
		return
	}

	result, err := core.QueryLogs(context.Background(), s.sqlite, s.clickhouse, s.log, sourceID, clickhouse.LogQueryParams{
		RawSQL:       req.RawSQL,
		Limit:        req.Limit,
		QueryTimeout: req.QueryTimeout,
	})
	if err != nil {
		s.log.Error("WebSocket query failed", "error", err)
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Query error: %v", err)))
		return
	}

	respBytes, err := json.Marshal(result)
	if err != nil {
		s.log.Error("Failed to marshal query result", "error", err)
		conn.WriteMessage(websocket.TextMessage, []byte("Internal error while preparing result"))
		return
	}

	if err := conn.WriteMessage(websocket.TextMessage, respBytes); err != nil {
		s.log.Warn("WebSocket write error", "error", err)
	}
}
