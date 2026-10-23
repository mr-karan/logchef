package models

import "time"

// SystemSetting is a single key/value system configuration entry. It is the
// backend-neutral representation of a row in system_settings: nullable columns
// and dialect-specific flags (e.g. SQLite's 0/1 booleans) are normalized here
// so callers never see driver types.
type SystemSetting struct {
	Key         string    `json:"key" db:"key"`
	Value       string    `json:"value" db:"value"`
	ValueType   string    `json:"value_type" db:"value_type"`
	Category    string    `json:"category" db:"category"`
	Description string    `json:"description,omitempty" db:"description"`
	IsSensitive bool      `json:"is_sensitive" db:"is_sensitive"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// IsSensitiveSetting reports whether a key always contains a secret.
func IsSensitiveSetting(key string) bool {
	switch key {
	case "ai.api_key", "alerts.smtp_password":
		return true
	default:
		return false
	}
}

// IsSessionOnlySetting reports whether a key contains a secret or selects its destination.
func IsSessionOnlySetting(key string) bool {
	if IsSensitiveSetting(key) {
		return true
	}
	switch key {
	case "alerts.smtp_host", "alerts.smtp_port", "alerts.smtp_security", "alerts.tls_insecure_skip_verify", "ai.base_url", "server.frontend_url":
		return true
	default:
		return false
	}
}
