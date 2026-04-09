package logchefql

import "testing"

func TestTranslateToLogsQL(t *testing.T) {
	t.Run("empty query becomes star filter", func(t *testing.T) {
		result := TranslateToLogsQL("", nil)
		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}
		if result.Query != "*" {
			t.Fatalf("expected '*' query, got %q", result.Query)
		}
	})

	t.Run("translates exact equality", func(t *testing.T) {
		result := TranslateToLogsQL(`level = "error"`, nil)
		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}
		if result.Query != `level:="error"` {
			t.Fatalf("unexpected LogsQL query: %q", result.Query)
		}
	})

	t.Run("translates substring operator to case insensitive regex", func(t *testing.T) {
		result := TranslateToLogsQL(`path ~ "/api/v1/users.123"`, nil)
		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}
		expected := `path:~"(?i)/api/v1/users\\.123"`
		if result.Query != expected {
			t.Fatalf("expected %q, got %q", expected, result.Query)
		}
	})

	t.Run("translates negation", func(t *testing.T) {
		result := TranslateToLogsQL(`level != "debug"`, nil)
		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}
		if result.Query != `NOT level:="debug"` {
			t.Fatalf("unexpected LogsQL query: %q", result.Query)
		}
	})

	t.Run("translates numeric comparisons", func(t *testing.T) {
		result := TranslateToLogsQL(`status >= 500 and status < 600`, nil)
		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}
		expected := `(status:>=500) AND (status:<600)`
		if result.Query != expected {
			t.Fatalf("expected %q, got %q", expected, result.Query)
		}
	})

	t.Run("preserves logical precedence", func(t *testing.T) {
		result := TranslateToLogsQL(`a = "1" or b = "2" and c = "3"`, nil)
		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}
		expected := `(a:="1") OR ((b:="2") AND (c:="3"))`
		if result.Query != expected {
			t.Fatalf("expected %q, got %q", expected, result.Query)
		}
	})

	t.Run("quotes special field names", func(t *testing.T) {
		result := TranslateToLogsQL(`log_attributes."foo bar" = "value"`, nil)
		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}
		expected := `"log_attributes.foo bar":="value"`
		if result.Query != expected {
			t.Fatalf("expected %q, got %q", expected, result.Query)
		}
	})

	t.Run("translates select pipe with timestamp field", func(t *testing.T) {
		result := TranslateToLogsQL(`level = "error" | _msg service`, &LogsQLTranslateOptions{
			DefaultTimestampField: "_time",
		})
		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}
		expected := `level:="error" | fields _time, _msg, service`
		if result.Query != expected {
			t.Fatalf("expected %q, got %q", expected, result.Query)
		}
	})

	t.Run("translates pipe only query", func(t *testing.T) {
		result := TranslateToLogsQL(`| service _msg`, &LogsQLTranslateOptions{
			DefaultTimestampField: "_time",
		})
		if !result.Valid {
			t.Fatalf("expected valid result, got error: %v", result.Error)
		}
		expected := `* | fields _time, service, _msg`
		if result.Query != expected {
			t.Fatalf("expected %q, got %q", expected, result.Query)
		}
	})

	t.Run("rejects null equality", func(t *testing.T) {
		result := TranslateToLogsQL(`field = null`, nil)
		if result.Valid {
			t.Fatalf("expected invalid result")
		}
		if result.Error == nil || result.Error.Code != ErrUnsupportedFeature {
			t.Fatalf("expected unsupported feature error, got %#v", result.Error)
		}
	})
}
