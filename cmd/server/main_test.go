package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveConfigPath(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(present, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.toml")

	tests := []struct {
		name     string
		path     string
		explicit bool
		want     string
	}{
		{"default file present", present, false, present},
		{"default file missing means environment only", missing, false, ""},
		{"explicit file present", present, true, present},
		{"explicit file missing is kept so loading fails", missing, true, missing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveConfigPath(tt.path, tt.explicit); got != tt.want {
				t.Errorf("resolveConfigPath(%q, %v) = %q, want %q", tt.path, tt.explicit, got, tt.want)
			}
		})
	}
}
