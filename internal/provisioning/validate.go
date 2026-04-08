package provisioning

import (
	"fmt"
	"os"
	"strings"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/pkg/models"
)

// ValidateConfig checks the provisioning config for internal consistency.
func ValidateConfig(cfg *config.ProvisioningConfig) error {
	var errs []string

	if cfg.ManageSources {
		errs = append(errs, validateSources(cfg)...)
	}
	if cfg.ManageTeams {
		errs = append(errs, validateTeams(cfg)...)
	}

	if len(errs) > 0 {
		return fmt.Errorf("provisioning config validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

func validateSources(cfg *config.ProvisioningConfig) []string {
	var errs []string
	seen := make(map[string]bool)

	for i, src := range cfg.Sources {
		prefix := fmt.Sprintf("sources[%d] (%q)", i, src.Name)

		if src.Name == "" {
			errs = append(errs, fmt.Sprintf("sources[%d]: name is required", i))
			continue
		}
		if seen[src.Name] {
			errs = append(errs, fmt.Sprintf("%s: duplicate source name", prefix))
		}
		seen[src.Name] = true

		sourceType := src.NormalizedSourceType()
		if !sourceType.Valid() {
			errs = append(errs, fmt.Sprintf("%s: unsupported source_type %q", prefix, src.SourceType))
			continue
		}

		switch sourceType {
		case models.SourceTypeClickHouse:
			conn, err := src.ClickHouseConnection()
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", prefix, err))
				continue
			}
			if strings.TrimSpace(conn.Host) == "" {
				errs = append(errs, fmt.Sprintf("%s: clickhouse host is required", prefix))
			}
			if strings.TrimSpace(conn.Database) == "" {
				errs = append(errs, fmt.Sprintf("%s: clickhouse database is required", prefix))
			}
			if strings.TrimSpace(conn.TableName) == "" {
				errs = append(errs, fmt.Sprintf("%s: clickhouse table_name is required", prefix))
			}
		case models.SourceTypeVictoriaLogs:
			conn, err := src.VictoriaLogsConnection()
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", prefix, err))
				continue
			}
			if strings.TrimSpace(conn.BaseURL) == "" {
				errs = append(errs, fmt.Sprintf("%s: victorialogs connection.base_url is required", prefix))
			}

			switch normalizedAuthMode(conn.Auth.Mode) {
			case "", "none":
			case "basic":
				if strings.TrimSpace(conn.Auth.Username) == "" {
					errs = append(errs, fmt.Sprintf("%s: victorialogs basic auth requires connection.auth.username", prefix))
				}
				if strings.TrimSpace(conn.Auth.Password) == "" && src.SecretRef == "" {
					errs = append(errs, fmt.Sprintf("%s: victorialogs basic auth requires connection.auth.password or secret_ref", prefix))
				}
			case "bearer":
				if strings.TrimSpace(conn.Auth.Token) == "" && src.SecretRef == "" {
					errs = append(errs, fmt.Sprintf("%s: victorialogs bearer auth requires connection.auth.token or secret_ref", prefix))
				}
			default:
				errs = append(errs, fmt.Sprintf("%s: unsupported victorialogs auth mode %q", prefix, conn.Auth.Mode))
			}
		}

		if src.SecretRef != "" && sourceSecretValueMissing(src, sourceType) {
			val := os.Getenv(src.SecretRef)
			if val == "" {
				errs = append(errs, fmt.Sprintf("%s: secret_ref %q env var is empty or not set", prefix, src.SecretRef))
			}
		}
	}
	return errs
}

func validateTeams(cfg *config.ProvisioningConfig) []string {
	var errs []string
	seen := make(map[string]bool)

	// Build source name set for reference validation
	sourceNames := make(map[string]bool)
	for _, src := range cfg.Sources {
		sourceNames[src.Name] = true
	}

	for i, team := range cfg.Teams {
		prefix := fmt.Sprintf("teams[%d] (%q)", i, team.Name)

		if team.Name == "" {
			errs = append(errs, fmt.Sprintf("teams[%d]: name is required", i))
			continue
		}
		if seen[team.Name] {
			errs = append(errs, fmt.Sprintf("%s: duplicate team name", prefix))
		}
		seen[team.Name] = true

		// Validate source references
		if cfg.ManageSources {
			for _, srcName := range team.Sources {
				if !sourceNames[srcName] {
					errs = append(errs, fmt.Sprintf("%s: references unknown source %q", prefix, srcName))
				}
			}
		}

		// Validate members
		memberSeen := make(map[string]bool)
		for j, member := range team.Members {
			memberPrefix := fmt.Sprintf("%s.members[%d]", prefix, j)

			if member.Email == "" {
				errs = append(errs, fmt.Sprintf("%s: email is required", memberPrefix))
				continue
			}
			if memberSeen[member.Email] {
				errs = append(errs, fmt.Sprintf("%s: duplicate email %q", memberPrefix, member.Email))
			}
			memberSeen[member.Email] = true

			role := strings.ToLower(member.Role)
			if role == "" {
				role = "member"
			}
			if role != "admin" && role != "editor" && role != "member" {
				errs = append(errs, fmt.Sprintf("%s: invalid role %q (must be admin, editor, or member)", memberPrefix, member.Role))
			}
		}
	}
	return errs
}

// ResolveSecrets resolves password values from environment variables.
// Must be called after ValidateConfig.
func ResolveSecrets(cfg *config.ProvisioningConfig) {
	for i := range cfg.Sources {
		source := &cfg.Sources[i]
		source.SourceType = source.NormalizedSourceType()

		switch source.SourceType {
		case models.SourceTypeClickHouse:
			conn, err := source.ClickHouseConnection()
			if err == nil {
				if conn.Password == "" && source.SecretRef != "" {
					conn.Password = os.Getenv(source.SecretRef)
					source.Password = conn.Password
				}
				_ = source.SetConnectionConfig(conn)
			}
			if source.MetaTSField == "" {
				source.MetaTSField = "timestamp"
			}
		case models.SourceTypeVictoriaLogs:
			conn, err := source.VictoriaLogsConnection()
			if err == nil {
				switch normalizedAuthMode(conn.Auth.Mode) {
				case "bearer":
					if conn.Auth.Token == "" && source.SecretRef != "" {
						conn.Auth.Token = os.Getenv(source.SecretRef)
					}
				case "basic":
					if conn.Auth.Password == "" && source.SecretRef != "" {
						conn.Auth.Password = os.Getenv(source.SecretRef)
					}
				}
				_ = source.SetConnectionConfig(conn)
			}
			if source.MetaTSField == "" {
				source.MetaTSField = "_time"
			}
		}
	}

	// Default member roles
	for i := range cfg.Teams {
		for j := range cfg.Teams[i].Members {
			if cfg.Teams[i].Members[j].Role == "" {
				cfg.Teams[i].Members[j].Role = "member"
			}
		}
	}
}

func normalizedAuthMode(mode string) string {
	return strings.ToLower(strings.TrimSpace(mode))
}

func sourceSecretValueMissing(src config.ProvisionSource, sourceType models.SourceType) bool {
	switch sourceType {
	case models.SourceTypeClickHouse:
		conn, err := src.ClickHouseConnection()
		if err != nil {
			return false
		}
		return strings.TrimSpace(conn.Password) == ""
	case models.SourceTypeVictoriaLogs:
		conn, err := src.VictoriaLogsConnection()
		if err != nil {
			return false
		}
		switch normalizedAuthMode(conn.Auth.Mode) {
		case "bearer":
			return strings.TrimSpace(conn.Auth.Token) == ""
		case "basic":
			return strings.TrimSpace(conn.Auth.Password) == ""
		default:
			return false
		}
	default:
		return false
	}
}
