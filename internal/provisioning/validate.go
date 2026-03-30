package provisioning

import (
	"fmt"
	"os"
	"strings"

	"github.com/mr-karan/logchef/internal/config"
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

		if src.Host == "" {
			errs = append(errs, fmt.Sprintf("%s: host is required", prefix))
		}
		if src.Database == "" {
			errs = append(errs, fmt.Sprintf("%s: database is required", prefix))
		}
		if src.TableName == "" {
			errs = append(errs, fmt.Sprintf("%s: table_name is required", prefix))
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
