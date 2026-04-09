package datasource

import "testing"

func TestValidateClickHouseConnection_AllowsEmptyPassword(t *testing.T) {
	err := validateClickHouseConnection(
		"connection.",
		true,
		"127.0.0.1:9000",
		"default",
		"",
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
		"default",
		"",
		"",
		"http",
	)
	if err == nil {
		t.Fatal("expected validation error for missing database")
	}
}
