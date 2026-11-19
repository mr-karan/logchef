package models

import (
	"net/url"
	"time"
)

// DestinationID identifies a notification destination.
type DestinationID int64

// DestinationType says how a destination delivers a notification.
type DestinationType string

const (
	DestinationTypeEmail     DestinationType = "email"
	DestinationTypeWebhook   DestinationType = "webhook"
	DestinationTypeSlack     DestinationType = "slack"
	DestinationTypePagerDuty DestinationType = "pagerduty"
)

// DestinationScope says who may use a destination in a rule.
type DestinationScope string

const (
	// DestinationScopePrivate is usable by its creator.
	DestinationScopePrivate DestinationScope = "private"
	// DestinationScopeTeam is usable by the members of its team.
	DestinationScopeTeam DestinationScope = "team"
	// DestinationScopeGlobal is usable by everyone.
	DestinationScopeGlobal DestinationScope = "global"
)

// DestinationConfig holds the settings of a destination that are safe to
// show. Each type uses its own fields; the others stay empty.
type DestinationConfig struct {
	// UserIDs and Addresses are the recipients of an email destination.
	UserIDs   []UserID `json:"user_ids,omitempty"`
	Addresses []string `json:"addresses,omitempty"`
	// PayloadVersion is the body format of a webhook destination.
	PayloadVersion int `json:"payload_version,omitempty"`
}

// NotificationDestination is a reusable target for notifications. Secret is
// never serialized: the API returns HasSecret and SecretHint instead.
type NotificationDestination struct {
	ID         DestinationID     `json:"id"`
	Name       string            `json:"name"`
	Type       DestinationType   `json:"type"`
	Scope      DestinationScope  `json:"scope"`
	TeamID     *TeamID           `json:"team_id,omitempty"`
	Config     DestinationConfig `json:"config"`
	Secret     string            `json:"-"`
	HasSecret  bool              `json:"has_secret"`
	SecretHint string            `json:"secret_hint,omitempty"`
	IsActive   bool              `json:"is_active"`
	CreatedBy  *UserID           `json:"created_by,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

// DestinationRequest creates or updates a destination. On update a blank
// Secret keeps the stored one.
type DestinationRequest struct {
	Name     string            `json:"name"`
	Type     DestinationType   `json:"type"`
	Scope    DestinationScope  `json:"scope"`
	TeamID   *TeamID           `json:"team_id,omitempty"`
	Config   DestinationConfig `json:"config"`
	Secret   string            `json:"secret"`
	IsActive *bool             `json:"is_active,omitempty"`
}

// TestDestinationRequest names a saved destination by ID, or carries an
// unsaved definition.
type TestDestinationRequest struct {
	ID *DestinationID `json:"id,omitempty"`
	DestinationRequest
}

// DestinationTestResult is the outcome of a test send to one target of a
// destination: the destination itself, or one email recipient.
type DestinationTestResult struct {
	Target string `json:"target"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
}

// DestinationSecretHint is what the API shows of a secret: the host of a URL,
// or the last four characters of a routing key.
func DestinationSecretHint(t DestinationType, secret string) string {
	if secret == "" {
		return ""
	}
	if t == DestinationTypePagerDuty {
		if len(secret) <= 4 {
			return "…"
		}
		return "…" + secret[len(secret)-4:]
	}
	parsed, err := url.Parse(secret)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	return parsed.Hostname()
}

// DeliveryStatus is the state of one queued notification.
type DeliveryStatus string

const (
	// DeliveryPending waits for its next attempt.
	DeliveryPending DeliveryStatus = "pending"
	// DeliveryAttempting is an email row inside its SMTP transaction.
	DeliveryAttempting DeliveryStatus = "attempting"
	DeliverySent       DeliveryStatus = "sent"
	DeliveryFailed     DeliveryStatus = "failed"
	// DeliveryUnknown may have reached the receiver; it is never retried.
	DeliveryUnknown DeliveryStatus = "unknown"
)

// NotificationDelivery is one row of the delivery queue: one message for one
// destination, or for one recipient of an email destination.
type NotificationDelivery struct {
	ID            int64
	DestinationID DestinationID
	Producer      string
	ProducerRef   string
	// Recipient is "user:<id>" or an address for an email destination, and
	// empty for other types.
	Recipient string
	// Rows with the same destination, recipient and sequence key go out in
	// ID order.
	SequenceKey   string
	Event         string
	DedupKey      string
	MessageJSON   string
	Status        DeliveryStatus
	Attempts      int
	NextAttemptAt time.Time
	ReservedToken int64
	ReservedUntil *time.Time
	LastError     string
	CreatedAt     time.Time
	SentAt        *time.Time
}

// DeliveryOutcome is what one send attempt records. A pending outcome is
// tried again after RetryAfter.
type DeliveryOutcome struct {
	Status     DeliveryStatus
	RetryAfter time.Duration
	Error      string
}

// DeliverySummary is one delivery of an alert history entry, as the API
// shows it.
type DeliverySummary struct {
	DestinationID   DestinationID   `json:"destination_id"`
	DestinationName string          `json:"destination_name"`
	DestinationType DestinationType `json:"destination_type"`
	Recipient       string          `json:"recipient,omitempty"`
	Event           string          `json:"event"`
	Status          DeliveryStatus  `json:"status"`
	Attempts        int             `json:"attempts"`
	LastError       string          `json:"last_error,omitempty"`
	SentAt          *time.Time      `json:"sent_at,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}
