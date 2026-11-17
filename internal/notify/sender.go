package notify

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mr-karan/logchef/pkg/models"
)

// Event is what a message announces.
type Event string

const (
	EventFiring   Event = "firing"
	EventResolved Event = "resolved"
	EventNoData   Event = "no_data"
	EventTest     Event = "test"
)

// Message is one rendered notification. It is stored with each delivery row,
// and each sender uses the parts that its wire format needs.
type Message struct {
	Event Event `json:"event"`
	// Title and Text are the subject and plain-text body of an email.
	Title string            `json:"title"`
	Text  string            `json:"text"`
	Group map[string]string `json:"group,omitempty"`
	// Alert is the body of a version 1 webhook.
	Alert *AlertPayload `json:"alert,omitempty"`
}

// Sender delivers one message to one destination, or to one recipient of an
// email destination. It returns an error from Permanent for a failure that a
// retry cannot fix, one that wraps ErrOutcomeUnknown when the receiver may
// have accepted the message, and any other error for a transient failure.
type Sender interface {
	Send(ctx context.Context, dest *models.NotificationDestination, recipient string, deliveryID int64, msg Message) error
}

// ErrOutcomeUnknown means the receiver may have accepted the message, so it
// must not be sent again.
var ErrOutcomeUnknown = errors.New("the receiver may have accepted the message")

type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

// Permanent marks err as a failure that a retry cannot fix.
func Permanent(err error) error { return &permanentError{err: err} }

// IsPermanent reports whether err was marked by Permanent.
func IsPermanent(err error) bool {
	_, ok := errors.AsType[*permanentError](err)
	return ok
}

type retryAfterError struct {
	err   error
	after time.Duration
}

func (e *retryAfterError) Error() string { return e.err.Error() }
func (e *retryAfterError) Unwrap() error { return e.err }

// RetryAfter marks a transient err whose receiver asked for a delay.
func RetryAfter(err error, after time.Duration) error {
	return &retryAfterError{err: err, after: after}
}

// wrapClassified prefixes err and keeps its class.
func wrapClassified(prefix string, err error) error {
	wrapped := fmt.Errorf("%s: %w", prefix, err)
	switch {
	case IsPermanent(err):
		return Permanent(wrapped)
	default:
		if r, ok := errors.AsType[*retryAfterError](err); ok {
			return RetryAfter(wrapped, r.after)
		}
		return wrapped
	}
}

const maxDeliveryErrorBytes = 512

// BoundedError cuts an error's text to 512 bytes of valid UTF-8 and marks the
// cut, so a stored error stays small.
func BoundedError(err error) string {
	message := strings.ToValidUTF8(err.Error(), "?")
	if len(message) <= maxDeliveryErrorBytes {
		return message
	}
	const marker = " [truncated]"
	limit := maxDeliveryErrorBytes - len(marker)
	for !utf8.ValidString(message[:limit]) {
		limit--
	}
	return message[:limit] + marker
}

// formatGroupValues joins a group's values as key=value, sorted by key.
func formatGroupValues(group map[string]string) string {
	keys := slices.Sorted(maps.Keys(group))
	parts := make([]string, len(keys))
	for i, key := range keys {
		parts[i] = key + "=" + group[key]
	}
	return strings.Join(parts, ", ")
}

// formatNumber shows at most four decimals, without trailing zeros.
func formatNumber(v float64) string {
	s := strconv.FormatFloat(v, 'f', 4, 64)
	return strings.TrimRight(strings.TrimRight(s, "0"), ".")
}

var thresholdSymbols = map[string]string{"gt": ">", "gte": ">=", "lt": "<", "lte": "<=", "eq": "=", "neq": "!="}

// thresholdText is a rule's threshold as an operator symbol and a number.
func thresholdText(op string, value float64) string {
	symbol := thresholdSymbols[op]
	if symbol == "" {
		symbol = op
	}
	return symbol + " " + formatNumber(value)
}

// cut keeps s within limit characters, ending a cut with an ellipsis.
func cut(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	return string(runes[:limit-1]) + "…"
}
