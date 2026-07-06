package provisioning

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/mr-karan/logchef/internal/config"
)

// sqlIdentifierRe matches a ClickHouse identifier (database, table, column):
// starts with a letter or underscore, then letters/digits/underscores. Provisioned
// sources are admin config-as-code, but these values are interpolated into raw
// ClickHouse SQL, so validate them here too (defense-in-depth, matching the API).
var sqlIdentifierRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

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

	for i := range cfg.Sources {
		src := cfg.Sources[i]
		prefix := fmt.Sprintf("sources[%d] (%q)", i, src.Name)

		if src.Name == "" {
			errs = append(errs, fmt.Sprintf("sources[%d]: name is required", i))
			continue
		}
		if seen[src.Name] {
			errs = append(errs, fmt.Sprintf("%s: duplicate source name", prefix))
		}
		seen[src.Name] = true

		if src.Host == "" {
			errs = append(errs, fmt.Sprintf("%s: host is required", prefix))
		}
		if src.Database == "" {
			errs = append(errs, fmt.Sprintf("%s: database is required", prefix))
		} else if !sqlIdentifierRe.MatchString(src.Database) {
			errs = append(errs, fmt.Sprintf("%s: database %q is not a valid identifier", prefix, src.Database))
		}
		if src.TableName == "" {
			errs = append(errs, fmt.Sprintf("%s: table_name is required", prefix))
		} else if !sqlIdentifierRe.MatchString(src.TableName) {
			errs = append(errs, fmt.Sprintf("%s: table_name %q is not a valid identifier", prefix, src.TableName))
		}
		if src.MetaTSField != "" && !sqlIdentifierRe.MatchString(src.MetaTSField) {
			errs = append(errs, fmt.Sprintf("%s: meta_ts_field %q is not a valid identifier", prefix, src.MetaTSField))
		}
		if src.MetaSeverityField != "" && !sqlIdentifierRe.MatchString(src.MetaSeverityField) {
			errs = append(errs, fmt.Sprintf("%s: meta_severity_field %q is not a valid identifier", prefix, src.MetaSeverityField))
		}

		// Resolve password from secret_ref if needed
		if src.Password == "" && src.SecretRef != "" {
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
	for i := range cfg.Sources {
		src := cfg.Sources[i]
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
		if cfg.Sources[i].Password == "" && cfg.Sources[i].SecretRef != "" {
			cfg.Sources[i].Password = os.Getenv(cfg.Sources[i].SecretRef)
		}
		// Apply defaults
		if cfg.Sources[i].MetaTSField == "" {
			cfg.Sources[i].MetaTSField = "timestamp"
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
