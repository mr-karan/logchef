package askchef

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mr-karan/logchef/pkg/models"
)

// QueryMode is the query language of an Explorer view or an evidence record.
type QueryMode string

const (
	ModeLogchefQL QueryMode = "logchefql"
	ModeNative    QueryMode = "native"
)

func parseQueryMode(raw string) (QueryMode, bool) {
	switch QueryMode(raw) {
	case ModeLogchefQL, ModeNative:
		return QueryMode(raw), true
	}
	return "", false
}

// EvidenceID numbers the evidence of one conversation. Its wire form is "E"
// and the number, such as "E3".
type EvidenceID int

func (id EvidenceID) String() string { return "E" + strconv.Itoa(int(id)) }

func (id EvidenceID) MarshalJSON() ([]byte, error) { return json.Marshal(id.String()) }

var errEvidenceID = errors.New(`evidence id must be "E" and a positive number`)

func (id *EvidenceID) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return errEvidenceID
	}
	digits, ok := strings.CutPrefix(raw, "E")
	if !ok || digits == "" || strings.Trim(digits, "0123456789") != "" {
		return errEvidenceID
	}
	n, err := strconv.Atoi(digits)
	if err != nil || n < 1 {
		return errEvidenceID
	}
	*id = EvidenceID(n)
	return nil
}

// Evidence is one tool call that read logs. The answer cites it as [E#], and
// the UI opens its query and window in the Explorer.
type Evidence struct {
	ID       EvidenceID      `json:"id"`
	Tool     string          `json:"tool"`
	TeamID   models.TeamID   `json:"team_id"`
	SourceID models.SourceID `json:"source_id"`
	Mode     QueryMode       `json:"mode"`
	Query    string          `json:"query"`
	// StartMS and EndMS are Unix milliseconds.
	StartMS int64  `json:"start"`
	EndMS   int64  `json:"end"`
	Summary string `json:"summary"`
}

// check reports why e cannot be evidence: a tool that reads no logs, an
// unknown mode, or a field over its limit. The server's own evidence and
// the client's prior evidence meet the same rules.
func (e Evidence) check() error {
	if allowedTools[e.Tool].read == nil {
		return fmt.Errorf("tool %q does not produce evidence", e.Tool)
	}
	if _, ok := parseQueryMode(string(e.Mode)); !ok {
		return errors.New(`mode must be "logchefql" or "native"`)
	}
	if utf8.RuneCountInString(e.Query) > maxEvidenceQueryChars {
		return fmt.Errorf("query is longer than %d characters", maxEvidenceQueryChars)
	}
	if utf8.RuneCountInString(e.Summary) > maxEvidenceSummaryChars {
		return fmt.Errorf("summary is longer than %d characters", maxEvidenceSummaryChars)
	}
	return nil
}
