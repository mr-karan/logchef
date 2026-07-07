package postgres

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// pgtype <-> Go conversion helpers shared by the domain wrappers. The generated
// sqlc models use pgtype.* for nullable columns; these fold them to/from the
// plain Go types that pkg/models uses.

// tsPtr converts a nullable timestamptz to *time.Time (nil when NULL).
func tsPtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	tt := t.Time
	return &tt
}

// ts builds a non-null timestamptz.
func ts(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// tsFromPtr builds a nullable timestamptz from *time.Time (NULL when nil).
func tsFromPtr(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

// textStr returns the string value of a nullable text ("" when NULL).
func textStr(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return t.String
}

// text builds a text value, treating empty string as NULL — matching the
// SQLite wrappers' nullString convention.
func text(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

// int8Val builds a non-null bigint.
func int8Val(v int64) pgtype.Int8 {
	return pgtype.Int8{Int64: v, Valid: true}
}

// int8FromPtr builds a nullable bigint from *int64 (NULL when nil).
func int8FromPtr(v *int64) pgtype.Int8 {
	if v == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *v, Valid: true}
}
