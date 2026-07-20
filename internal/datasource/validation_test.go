package datasource

import "testing"

func TestValidateClickHouseConnection_AllowsEmptyPassword(t *testing.T) {
	// validateClickHouseConnection does not take a password argument at all
	// (password is never validated for format/presence), so this exercises
	// that a valid host/database/table combination passes regardless.
	err := validateClickHouseConnection(
		"connection.",
		true,
		"127.0.0.1:9000",
		"default",
		"http",
	)
	if err != nil {
		t.Fatalf("expected empty password to be allowed for ClickHouse, got %v", err)
	}
}

func TestValidateClickHouseConnection_RequiresDatabase(t *testing.T) {
	err := validateClickHouseConnection(
		"connection.",
		true,
		"127.0.0.1:9000",
		"",
		"http",
	)
	if err == nil {
		t.Fatal("expected validation error for missing database")
	}
}
