package provisioning

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// SecretRefConflictError reports two secrets that would export as one
// environment variable, which cannot hold both.
type SecretRefConflictError struct {
	EnvVar, First, Second string
}

func (e *SecretRefConflictError) Error() string {
	return fmt.Sprintf("%s and %s both export as %s; rename a source, or declare a variable of its own in secret_ref or header_secret_refs",
		e.First, e.Second, e.EnvVar)
}

// ExportConfig reads the current database state and produces a ProvisioningConfig.
// Passwords and custom header values are replaced with secret_ref and
// header_secret_refs placeholders (never exported).
func ExportConfig(ctx context.Context, db store.StoreOps) (*config.ProvisioningConfig, error) {
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

	// Generated variable names come from sanitized names, so two sources or
	// headers can map to one variable that cannot hold both secrets. A name
	// the config declared may repeat: one variable can feed several sources.
	type claim struct {
		owner     string
		generated bool
	}
	claimed := make(map[string]claim)
	claimVar := func(envVar, owner string, generated bool) error {
		if other, taken := claimed[envVar]; taken && (generated || other.generated) {
			return &SecretRefConflictError{EnvVar: envVar, First: other.owner, Second: owner}
		}
		claimed[envVar] = claim{owner: owner, generated: generated}
		return nil
	}
	for _, src := range sources {
		provisioned, err := exportSource(src)
		if err != nil {
			return nil, err
		}
		if provisioned.SecretRef != "" {
			if err := claimVar(provisioned.SecretRef, fmt.Sprintf("the secret of source %q", src.Name), src.SecretRef == ""); err != nil {
				return nil, err
			}
		}
		for _, header := range slices.Sorted(maps.Keys(provisioned.HeaderSecretRefs)) {
			generated := declaredHeaderRef(src.HeaderSecretRefs, header) == ""
			if err := claimVar(provisioned.HeaderSecretRefs[header], fmt.Sprintf("header %q of source %q", header, src.Name), generated); err != nil {
				return nil, err
			}
		}
		if provisioned.Views, err = exportSourceViews(ctx, db, src); err != nil {
			return nil, err
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

// exportSource converts a stored source into its provisioning entry. A secret
// is never exported: its place is taken by a secret_ref, and each custom
// header value by a header_secret_refs entry.
func exportSource(src *models.Source) (config.ProvisionSource, error) {
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
		if err := exportVictoriaLogsSource(src, &provisioned); err != nil {
			return config.ProvisionSource{}, err
		}
	case models.SourceTypeLoki:
		if err := exportLokiSource(src, &provisioned); err != nil {
			return config.ProvisionSource{}, err
		}
	default:
		conn := src.Connection
		if src.SecretRef != "" {
			provisioned.SecretRef = src.SecretRef
		} else if conn.Password != "" {
			provisioned.SecretRef = fmt.Sprintf("LOGCHEF_SOURCE_%s_PASSWORD", sanitizeEnvName(src.Name))
		}

		conn.Password = ""
		if err := provisioned.SetConnectionConfig(conn); err != nil {
			return config.ProvisionSource{}, fmt.Errorf("failed to encode clickhouse source %q: %w", src.Name, err)
		}
	}

	return provisioned, nil
}

// exportVictoriaLogsSource fills p with the connection of a VictoriaLogs
// source, its secret and header values replaced by references.
func exportVictoriaLogsSource(src *models.Source, p *config.ProvisionSource) error {
	conn, err := src.VictoriaLogsConnection()
	if err != nil {
		return fmt.Errorf("failed to decode victorialogs source %q: %w", src.Name, err)
	}

	if src.SecretRef != "" {
		p.SecretRef = src.SecretRef
	} else {
		switch strings.ToLower(strings.TrimSpace(conn.Auth.Mode)) {
		case "bearer":
			if conn.Auth.Token != "" {
				p.SecretRef = fmt.Sprintf("LOGCHEF_SOURCE_%s_TOKEN", sanitizeEnvName(src.Name))
			}
		case "basic":
			if conn.Auth.Password != "" {
				p.SecretRef = fmt.Sprintf("LOGCHEF_SOURCE_%s_PASSWORD", sanitizeEnvName(src.Name))
			}
		}
	}

	conn.Auth.Password = ""
	conn.Auth.Token = ""
	p.HeaderSecretRefs, conn.Headers = exportHeaders(src, conn.Headers)
	if err := p.SetConnectionConfig(conn); err != nil {
		return fmt.Errorf("failed to encode victorialogs source %q: %w", src.Name, err)
	}
	return nil
}

// exportLokiSource fills p with the connection of a Loki source, its secret
// and header values replaced by references.
func exportLokiSource(src *models.Source, p *config.ProvisionSource) error {
	conn, err := src.LokiConnection()
	if err != nil {
		return fmt.Errorf("failed to decode loki source %q: %w", src.Name, err)
	}

	if secret := lokiSecretField(&conn); src.SecretRef != "" {
		p.SecretRef = src.SecretRef
	} else if secret != nil && *secret != "" {
		suffix := "PASSWORD"
		if normalizedAuthMode(conn.Auth.Mode) == "bearer" {
			suffix = "TOKEN"
		}
		p.SecretRef = fmt.Sprintf("LOGCHEF_SOURCE_%s_%s", sanitizeEnvName(src.Name), suffix)
	}

	conn.Auth.Password = ""
	conn.Auth.Token = ""
	p.HeaderSecretRefs, conn.Headers = exportHeaders(src, conn.Headers)
	if err := p.SetConnectionConfig(conn); err != nil {
		return fmt.Errorf("failed to encode loki source %q: %w", src.Name, err)
	}
	return nil
}

// exportHeaders replaces each custom header value with a reference to an
// environment variable: the one provisioning declared for the header, or else
// the generated LOGCHEF_SOURCE_<SOURCE>_HEADER_<HEADER>. A header with an
// empty value holds no secret and stays in the connection, so importing the
// export does not demand a variable for it.
func exportHeaders(src *models.Source, headers map[string]string) (refs, kept map[string]string) {
	for _, name := range slices.Sorted(maps.Keys(headers)) {
		if headers[name] == "" {
			if kept == nil {
				kept = make(map[string]string)
			}
			kept[name] = ""
			continue
		}
		if refs == nil {
			refs = make(map[string]string)
		}
		refs[name] = declaredHeaderRef(src.HeaderSecretRefs, name)
		if refs[name] == "" {
			refs[name] = generatedHeaderRef(src.Name, name)
		}
	}
	return refs, kept
}

func generatedHeaderRef(sourceName, header string) string {
	return fmt.Sprintf("LOGCHEF_SOURCE_%s_HEADER_%s", sanitizeEnvName(sourceName), sanitizeEnvName(header))
}

// customHeaderRefs returns the header references a source keeps for export,
// keyed by headerRefKey: those that name a variable other than the one export
// generates, so importing an export leaves the source unchanged.
func customHeaderRefs(sourceName string, refs map[string]string) map[string]string {
	var custom map[string]string
	for header, envVar := range refs {
		if envVar == generatedHeaderRef(sourceName, header) {
			continue
		}
		if custom == nil {
			custom = make(map[string]string)
		}
		custom[headerRefKey(header)] = envVar
	}
	return custom
}

// declaredHeaderRef returns the variable that provisioning declared for the
// header, or "" if none.
func declaredHeaderRef(declared map[string]string, header string) string {
	return declared[headerRefKey(header)]
}

// headerRefKey is the key of a header in models.Source.HeaderSecretRefs.
// Header names match without regard to case, so the key is lower case.
func headerRefKey(header string) string {
	return strings.ToLower(strings.TrimSpace(header))
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
