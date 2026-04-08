package provisioning

import (
	"context"
	"fmt"
	"strings"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

// ExportConfig reads the current database state and produces a ProvisioningConfig.
// Passwords are replaced with secret_ref placeholders (never exported).
func ExportConfig(ctx context.Context, db *sqlite.DB) (*config.ProvisioningConfig, error) {
	cfg := &config.ProvisioningConfig{
		ManageSources: true,
		ManageTeams:   true,
		Prune:         false,
		DryRun:        false,
	}

	// Export sources
	sources, err := db.ListSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list sources: %w", err)
	}

	for _, src := range sources {
		provisioned := config.ProvisionSource{
			Name:              src.Name,
			SourceType:        models.NormalizeSourceType(src.SourceType),
			Description:       src.Description,
			TTLDays:           src.TTLDays,
			MetaTSField:       src.MetaTSField,
			MetaSeverityField: src.MetaSeverityField,
		}

		switch models.NormalizeSourceType(src.SourceType) {
		case models.SourceTypeVictoriaLogs:
			conn, err := src.VictoriaLogsConnection()
			if err != nil {
				return nil, fmt.Errorf("failed to decode victorialogs source %q: %w", src.Name, err)
			}

			if src.SecretRef != "" {
				provisioned.SecretRef = src.SecretRef
			} else {
				switch strings.ToLower(strings.TrimSpace(conn.Auth.Mode)) {
				case "bearer":
					if conn.Auth.Token != "" {
						provisioned.SecretRef = fmt.Sprintf("LOGCHEF_SOURCE_%s_TOKEN", sanitizeEnvName(src.Name))
					}
				case "basic":
					if conn.Auth.Password != "" {
						provisioned.SecretRef = fmt.Sprintf("LOGCHEF_SOURCE_%s_PASSWORD", sanitizeEnvName(src.Name))
					}
				}
			}

			conn.Auth.Password = ""
			conn.Auth.Token = ""
			if err := provisioned.SetConnectionConfig(conn); err != nil {
				return nil, fmt.Errorf("failed to encode victorialogs source %q: %w", src.Name, err)
			}
		case models.SourceTypeClickHouse:
			fallthrough
		default:
			conn := src.Connection
			if src.SecretRef != "" {
				provisioned.SecretRef = src.SecretRef
			} else if conn.Password != "" {
				provisioned.SecretRef = fmt.Sprintf("LOGCHEF_SOURCE_%s_PASSWORD", sanitizeEnvName(src.Name))
			}

			conn.Password = ""
			if err := provisioned.SetConnectionConfig(conn); err != nil {
				return nil, fmt.Errorf("failed to encode clickhouse source %q: %w", src.Name, err)
			}
		}

		cfg.Sources = append(cfg.Sources, provisioned)
	}

	// Export teams
	teams, err := db.ListTeams(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list teams: %w", err)
	}

	for _, team := range teams {
		pt := config.ProvisionTeam{
			Name:        team.Name,
			Description: team.Description,
		}

		// Get members
		members, err := db.ListTeamMembersWithDetails(ctx, team.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to list members for team %q: %w", team.Name, err)
		}
		for _, m := range members {
			pt.Members = append(pt.Members, config.ProvisionMember{
				Email: m.Email,
				Role:  string(m.Role),
			})
		}

		// Get source links
		teamSources, err := db.ListTeamSources(ctx, team.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to list sources for team %q: %w", team.Name, err)
		}
		for _, ts := range teamSources {
			pt.Sources = append(pt.Sources, ts.Name)
		}

		cfg.Teams = append(cfg.Teams, pt)
	}

	return cfg, nil
}

func sanitizeEnvName(name string) string {
	var result []byte
	for _, c := range []byte(strings.ToUpper(name)) {
		if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			result = append(result, c)
		} else {
			result = append(result, '_')
		}
	}
	return string(result)
}
