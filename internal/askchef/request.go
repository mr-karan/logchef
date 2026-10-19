package askchef

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mr-karan/logchef/internal/ai"
	"github.com/mr-karan/logchef/pkg/models"
)

// Request limits from the API contract.
const (
	// MaxRequestBytes bounds a turn request body. It is above any request
	// that keeps the field limits below with realistic text.
	MaxRequestBytes  = 1 << 20
	maxHistoryItems  = 20
	maxHistoryChars  = 8000
	maxEvidenceItems = 50
	maxMessageChars  = 4000
	// Evidence fields come back from the client, and the system prompt
	// repeats them, so each is bounded.
	maxEvidenceQueryChars   = 4000
	maxEvidenceSummaryChars = 300
)

// Request is a validated turn request.
type Request struct {
	TeamID   models.TeamID
	SourceID models.SourceID
	Location *time.Location
	View     View
	History  []HistoryMessage
	// Evidence is what earlier turns produced. The answer may cite it again.
	Evidence []Evidence
	Message  string
}

// View is what the user is looking at when they ask.
type View struct {
	// Mode and Query are empty when the request gave none.
	Mode  QueryMode
	Query string
	Start time.Time
	End   time.Time
	// Alert is set only when the user asks from an alert.
	Alert *AlertRef
}

// AlertRef names the alert, and optionally the history entry, the user is
// looking at.
type AlertRef struct {
	AlertID   models.AlertID
	HistoryID int64
}

// HistoryMessage is one earlier message of the conversation.
type HistoryMessage struct {
	Role ai.ChatRole
	Text string
}

// RequestError is a request that does not meet the contract. Its message is
// shown to the caller.
type RequestError struct{ Message string }

func (e *RequestError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &RequestError{Message: fmt.Sprintf(format, args...)}
}

type wireRequest struct {
	TeamID   int    `json:"team_id"`
	SourceID int    `json:"source_id"`
	Timezone string `json:"timezone"`
	Context  struct {
		Kind           string `json:"kind"`
		Mode           string `json:"mode"`
		Query          string `json:"query"`
		Start          int64  `json:"start"`
		End            int64  `json:"end"`
		AlertID        *int64 `json:"alert_id"`
		AlertHistoryID *int64 `json:"alert_history_id"`
	} `json:"context"`
	History []struct {
		Role string `json:"role"`
		Text string `json:"text"`
	} `json:"history"`
	Evidence []Evidence `json:"evidence"`
	Message  string     `json:"message"`
}

// ParseRequest decodes and validates a turn request body. Any error is a
// *RequestError.
func ParseRequest(body []byte) (Request, error) {
	var w wireRequest
	if err := json.Unmarshal(body, &w); err != nil {
		return Request{}, invalid("invalid request body: %v", err)
	}
	if w.TeamID < 1 || w.SourceID < 1 {
		return Request{}, invalid("team_id and source_id are required")
	}
	loc := time.UTC
	if w.Timezone != "" {
		var err error
		if loc, err = time.LoadLocation(w.Timezone); err != nil {
			return Request{}, invalid("unknown timezone %q", w.Timezone)
		}
	}
	view, err := parseView(w)
	if err != nil {
		return Request{}, err
	}
	history, err := parseHistory(w)
	if err != nil {
		return Request{}, err
	}
	if err := checkEvidence(w.Evidence); err != nil {
		return Request{}, err
	}
	if strings.TrimSpace(w.Message) == "" || utf8.RuneCountInString(w.Message) > maxMessageChars {
		return Request{}, invalid("message must be 1 to %d characters", maxMessageChars)
	}
	return Request{
		TeamID:   models.TeamID(w.TeamID),
		SourceID: models.SourceID(w.SourceID),
		Location: loc,
		View:     view,
		History:  history,
		Evidence: w.Evidence,
		Message:  w.Message,
	}, nil
}

func parseView(w wireRequest) (View, error) {
	c := w.Context
	if c.Start <= 0 || c.End <= 0 || c.Start >= c.End {
		return View{}, invalid("context.start and context.end must be Unix milliseconds with start before end")
	}
	view := View{Query: c.Query, Start: time.UnixMilli(c.Start), End: time.UnixMilli(c.End)}
	if c.Mode != "" {
		mode, ok := parseQueryMode(c.Mode)
		if !ok {
			return View{}, invalid(`context.mode must be "logchefql" or "native"`)
		}
		view.Mode = mode
	}
	switch c.Kind {
	case "explorer":
		if c.AlertID != nil || c.AlertHistoryID != nil {
			return View{}, invalid(`context.alert_id and context.alert_history_id are allowed only when context.kind is "alert"`)
		}
	case "alert":
		if c.AlertID == nil || *c.AlertID < 1 {
			return View{}, invalid(`context.alert_id is required when context.kind is "alert"`)
		}
		view.Alert = &AlertRef{AlertID: models.AlertID(*c.AlertID)}
		if c.AlertHistoryID != nil {
			if *c.AlertHistoryID < 1 {
				return View{}, invalid("context.alert_history_id must be positive")
			}
			view.Alert.HistoryID = *c.AlertHistoryID
		}
	default:
		return View{}, invalid(`context.kind must be "explorer" or "alert"`)
	}
	return view, nil
}

func parseHistory(w wireRequest) ([]HistoryMessage, error) {
	if len(w.History) > maxHistoryItems {
		return nil, invalid("history has at most %d items", maxHistoryItems)
	}
	history := make([]HistoryMessage, len(w.History))
	for i, h := range w.History {
		switch ai.ChatRole(h.Role) {
		case ai.ChatRoleUser, ai.ChatRoleAssistant:
		default:
			return nil, invalid(`history[%d].role must be "user" or "assistant"`, i)
		}
		if utf8.RuneCountInString(h.Text) > maxHistoryChars {
			return nil, invalid("history[%d].text is longer than %d characters", i, maxHistoryChars)
		}
		history[i] = HistoryMessage{Role: ai.ChatRole(h.Role), Text: h.Text}
	}
	return history, nil
}

func checkEvidence(evidence []Evidence) error {
	if len(evidence) > maxEvidenceItems {
		return invalid("evidence has at most %d items", maxEvidenceItems)
	}
	for i, e := range evidence {
		if e.ID < 1 {
			return invalid("evidence[%d].id is required", i)
		}
		if err := e.check(); err != nil {
			return invalid("evidence[%d]: %v", i, err)
		}
	}
	return nil
}
