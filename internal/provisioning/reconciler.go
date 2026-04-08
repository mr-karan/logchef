package provisioning

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/internal/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// Reconcile applies the provisioning config to the database.
// It runs in a single SQLite write transaction. On dry_run, the transaction is rolled back.
// adminEmails is used to determine global admin role precedence.
func Reconcile(ctx context.Context, cfg *config.ProvisioningConfig, db *sqlite.DB, ds *datasource.Service, log *slog.Logger, adminEmails []string) error {
	if !cfg.Enabled() {
		return nil
	}

	// Validate config
	if err := ValidateConfig(cfg); err != nil {
		return err
	}

	// Resolve secrets from environment
	ResolveSecrets(cfg)

	// Build admin email set for role precedence
	adminSet := make(map[string]bool)
	for _, email := range adminEmails {
		adminSet[strings.ToLower(email)] = true
	}

	// Begin transaction on write connection
	tx, err := db.BeginWriteTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := db.WriteQueriesWithTx(tx)

	// Track sources created/updated for post-commit datasource connection setup.
	var sourcesToConnect []models.Source

	// Phase 1: Sources
	if cfg.ManageSources {
		sources, err := reconcileSources(ctx, qtx, cfg, ds, log, &sourcesToConnect)
		if err != nil {
			return fmt.Errorf("source reconciliation failed: %w", err)
		}
		_ = sources
	}

	// Phase 2: Teams (depends on sources being reconciled)
	if cfg.ManageTeams {
		if err := reconcileTeams(ctx, qtx, cfg, log, adminSet); err != nil {
			return fmt.Errorf("team reconciliation failed: %w", err)
		}
	}

	// Commit or rollback
	if cfg.DryRun {
		log.Info("provisioning dry-run complete, rolling back transaction")
		return tx.Rollback()
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Info("provisioning reconciliation committed successfully")

	// Post-commit: establish datasource connections for new/updated sources
	for i := range sourcesToConnect {
		if err := ds.RemoveSource(&sourcesToConnect[i]); err != nil {
			log.Debug("failed to clear existing datasource connection during provisioning",
				"source_id", sourcesToConnect[i].ID, "name", sourcesToConnect[i].Name, "error", err)
		}
		if err := ds.InitializeSource(ctx, &sourcesToConnect[i]); err != nil {
			log.Warn("failed to establish datasource connection for provisioned source",
				"source_id", sourcesToConnect[i].ID, "name", sourcesToConnect[i].Name, "error", err)
		}
	}

	return nil
}

func reconcileSources(ctx context.Context, qtx *sqlc.Queries, cfg *config.ProvisioningConfig, ds *datasource.Service, log *slog.Logger, toConnect *[]models.Source) (map[string]int64, error) {
	// Load existing managed sources
	existingManaged, err := qtx.ListManagedSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list managed sources: %w", err)
	}

	managedByName := make(map[string]sqlc.Source)
	for _, src := range existingManaged {
		managedByName[src.Name] = src
	}

	// Track desired source names for pruning
	desiredNames := make(map[string]bool)
	// Map source name → DB ID for team source linking
	sourceIDs := make(map[string]int64)

	for _, cfgSrc := range cfg.Sources {
		desiredNames[cfgSrc.Name] = true

		existing, isManaged := managedByName[cfgSrc.Name]
		if !isManaged {
			// Check for unmanaged source with same name (adopt)
			unmanaged, err := qtx.GetSourceByNameForProvisioning(ctx, cfgSrc.Name)
			if err == nil {
				// Adopt: update fields and mark managed
				log.Info("adopting existing source as managed", "name", cfgSrc.Name, "id", unmanaged.ID)
				if err := updateSourceFromConfig(ctx, qtx, unmanaged.ID, cfgSrc); err != nil {
					return nil, fmt.Errorf("failed to adopt source %q: %w", cfgSrc.Name, err)
				}
				if err := qtx.SetSourceManaged(ctx, sqlc.SetSourceManagedParams{
					Managed:   1,
					SecretRef: sql.NullString{String: cfgSrc.SecretRef, Valid: cfgSrc.SecretRef != ""},
					ID:        unmanaged.ID,
				}); err != nil {
					return nil, fmt.Errorf("failed to set source %q as managed: %w", cfgSrc.Name, err)
				}
				sourceIDs[cfgSrc.Name] = unmanaged.ID
				sourceModel, err := buildProvisionedSourceModelWithID(unmanaged.ID, cfgSrc)
				if err != nil {
					return nil, fmt.Errorf("failed to build provisioned source %q: %w", cfgSrc.Name, err)
				}
				*toConnect = append(*toConnect, sourceModel)
				continue
			}

			// Create new source
			log.Info("creating managed source", "name", cfgSrc.Name)

			// Validate datasource connectivity before creating.
			if err := validateSourceConnection(ctx, ds, cfgSrc, log); err != nil {
				log.Warn("datasource validation failed for provisioned source, creating anyway",
					"name", cfgSrc.Name, "error", err)
			}

			createParams, err := buildCreateSourceParams(cfgSrc)
			if err != nil {
				return nil, fmt.Errorf("failed to build source params for %q: %w", cfgSrc.Name, err)
			}
			id, err := qtx.CreateSource(ctx, createParams)
			if err != nil {
				return nil, fmt.Errorf("failed to create source %q: %w", cfgSrc.Name, err)
			}
			if err := qtx.SetSourceManaged(ctx, sqlc.SetSourceManagedParams{
				Managed:   1,
				SecretRef: sql.NullString{String: cfgSrc.SecretRef, Valid: cfgSrc.SecretRef != ""},
				ID:        id,
			}); err != nil {
				return nil, fmt.Errorf("failed to set source %q as managed: %w", cfgSrc.Name, err)
			}

			sourceIDs[cfgSrc.Name] = id
			sourceModel, err := buildProvisionedSourceModelWithID(id, cfgSrc)
			if err != nil {
				return nil, fmt.Errorf("failed to build provisioned source %q: %w", cfgSrc.Name, err)
			}
			*toConnect = append(*toConnect, sourceModel)
		} else {
			// Update existing managed source if fields changed
			needsUpdate, err := sourceNeedsUpdate(existing, cfgSrc)
			if err != nil {
				return nil, fmt.Errorf("failed to compare managed source %q: %w", cfgSrc.Name, err)
			}
			if needsUpdate {
				log.Info("updating managed source", "name", cfgSrc.Name)
				if err := updateSourceFromConfig(ctx, qtx, existing.ID, cfgSrc); err != nil {
					return nil, fmt.Errorf("failed to update source %q: %w", cfgSrc.Name, err)
				}
				sourceModel, err := buildProvisionedSourceModelWithID(existing.ID, cfgSrc)
				if err != nil {
					return nil, fmt.Errorf("failed to build provisioned source %q: %w", cfgSrc.Name, err)
				}
				*toConnect = append(*toConnect, sourceModel)
			}
			sourceIDs[cfgSrc.Name] = existing.ID
		}
	}

	// Prune: delete managed sources not in config
	if cfg.Prune {
		for name, src := range managedByName {
			if !desiredNames[name] {
				log.Warn("pruning managed source not in config", "name", name, "id", src.ID)
				if err := qtx.DeleteSource(ctx, src.ID); err != nil {
					return nil, fmt.Errorf("failed to prune source %q: %w", name, err)
				}
			}
		}
	} else {
		// Log orphaned managed sources as warnings
		for name := range managedByName {
			if !desiredNames[name] {
				log.Warn("managed source not in config (prune=false, keeping)", "name", name)
			}
		}
	}

	return sourceIDs, nil
}

func reconcileTeams(ctx context.Context, qtx *sqlc.Queries, cfg *config.ProvisioningConfig, log *slog.Logger, adminSet map[string]bool) error {
	// Load existing managed teams
	existingManaged, err := qtx.ListManagedTeams(ctx)
	if err != nil {
		return fmt.Errorf("failed to list managed teams: %w", err)
	}

	managedByName := make(map[string]sqlc.Team)
	for _, team := range existingManaged {
		managedByName[team.Name] = team
	}

	desiredNames := make(map[string]bool)

	for _, cfgTeam := range cfg.Teams {
		desiredNames[cfgTeam.Name] = true

		var teamID int64

		existing, isManaged := managedByName[cfgTeam.Name]
		if !isManaged {
			// Check for unmanaged team with same name (adopt)
			unmanaged, err := qtx.GetTeamByName(ctx, cfgTeam.Name)
			if err == nil {
				log.Info("adopting existing team as managed", "name", cfgTeam.Name, "id", unmanaged.ID)
				teamID = unmanaged.ID
				if err := qtx.SetTeamManaged(ctx, sqlc.SetTeamManagedParams{Managed: 1, ID: teamID}); err != nil {
					return fmt.Errorf("failed to set team %q as managed: %w", cfgTeam.Name, err)
				}
				// Update description if different
				if unmanaged.Description.String != cfgTeam.Description {
					if err := qtx.UpdateTeam(ctx, sqlc.UpdateTeamParams{
						Name:        cfgTeam.Name,
						Description: sql.NullString{String: cfgTeam.Description, Valid: cfgTeam.Description != ""},
						ID:          teamID,
					}); err != nil {
						return fmt.Errorf("failed to update team %q: %w", cfgTeam.Name, err)
					}
				}
			} else {
				// Create new team
				log.Info("creating managed team", "name", cfgTeam.Name)
				id, err := qtx.CreateTeam(ctx, sqlc.CreateTeamParams{
					Name:        cfgTeam.Name,
					Description: sql.NullString{String: cfgTeam.Description, Valid: cfgTeam.Description != ""},
				})
				if err != nil {
					return fmt.Errorf("failed to create team %q: %w", cfgTeam.Name, err)
				}
				teamID = id
				if err := qtx.SetTeamManaged(ctx, sqlc.SetTeamManagedParams{Managed: 1, ID: teamID}); err != nil {
					return fmt.Errorf("failed to set team %q as managed: %w", cfgTeam.Name, err)
				}
			}
		} else {
			teamID = existing.ID
			// Update description if changed
			if existing.Description.String != cfgTeam.Description {
				if err := qtx.UpdateTeam(ctx, sqlc.UpdateTeamParams{
					Name:        cfgTeam.Name,
					Description: sql.NullString{String: cfgTeam.Description, Valid: cfgTeam.Description != ""},
					ID:          teamID,
				}); err != nil {
					return fmt.Errorf("failed to update team %q: %w", cfgTeam.Name, err)
				}
			}
		}

		// Reconcile members
		if err := reconcileTeamMembers(ctx, qtx, teamID, cfgTeam, log, adminSet, cfg.Prune); err != nil {
			return fmt.Errorf("failed to reconcile members for team %q: %w", cfgTeam.Name, err)
		}

		// Reconcile source links
		if err := reconcileTeamSources(ctx, qtx, teamID, cfgTeam, log, cfg.Prune); err != nil {
			return fmt.Errorf("failed to reconcile sources for team %q: %w", cfgTeam.Name, err)
		}
	}

	// Prune teams
	if cfg.Prune {
		for name, team := range managedByName {
			if !desiredNames[name] {
				log.Warn("pruning managed team not in config (cascades to saved queries/alerts)", "name", name, "id", team.ID)
				if err := qtx.DeleteTeam(ctx, team.ID); err != nil {
					return fmt.Errorf("failed to prune team %q: %w", name, err)
				}
			}
		}
	} else {
		for name := range managedByName {
			if !desiredNames[name] {
				log.Warn("managed team not in config (prune=false, keeping)", "name", name)
			}
		}
	}

	return nil
}

func reconcileTeamMembers(ctx context.Context, qtx *sqlc.Queries, teamID int64, cfgTeam config.ProvisionTeam, log *slog.Logger, adminSet map[string]bool, prune bool) error {
	// Load current members
	currentMembers, err := qtx.ListTeamMembers(ctx, teamID)
	if err != nil {
		return fmt.Errorf("failed to list team members: %w", err)
	}

	currentByEmail := make(map[string]sqlc.TeamMember)
	for _, m := range currentMembers {
		// Get user email
		user, err := qtx.GetUser(ctx, m.UserID)
		if err != nil {
			continue
		}
		currentByEmail[user.Email] = m
	}

	desiredEmails := make(map[string]bool)

	for _, member := range cfgTeam.Members {
		email := strings.ToLower(member.Email)
		desiredEmails[email] = true

		// Ensure user exists
		user, err := qtx.GetUserByEmail(ctx, email)
		if err != nil {
			// Create user stub
			log.Info("creating managed user for team membership", "email", email, "team", cfgTeam.Name)

			// Global role: admin if in admin_emails, otherwise member
			globalRole := "member"
			if adminSet[email] {
				globalRole = "admin"
			}

			userID, err := qtx.CreateUser(ctx, sqlc.CreateUserParams{
				Email:    email,
				FullName: email, // Placeholder — updated on first OIDC login
				Role:     globalRole,
				Status:   "active",
			})
			if err != nil {
				return fmt.Errorf("failed to create user %q: %w", email, err)
			}
			if err := qtx.SetUserManaged(ctx, sqlc.SetUserManagedParams{Managed: 1, ID: userID}); err != nil {
				return fmt.Errorf("failed to set user %q as managed: %w", email, err)
			}

			// Add team membership
			if err := qtx.AddTeamMember(ctx, sqlc.AddTeamMemberParams{
				TeamID: teamID,
				UserID: userID,
				Role:   strings.ToLower(member.Role),
			}); err != nil {
				return fmt.Errorf("failed to add member %q to team: %w", email, err)
			}
		} else {
			// User exists — mark as managed if referenced by config
			if user.Managed == 0 {
				if err := qtx.SetUserManaged(ctx, sqlc.SetUserManagedParams{Managed: 1, ID: user.ID}); err != nil {
					return fmt.Errorf("failed to set user %q as managed: %w", email, err)
				}
			}

			// Ensure team membership with correct role
			existingMember, hasMembership := currentByEmail[user.Email]
			if !hasMembership {
				// Add membership
				log.Info("adding member to managed team", "email", email, "team", cfgTeam.Name, "role", member.Role)
				if err := qtx.AddTeamMember(ctx, sqlc.AddTeamMemberParams{
					TeamID: teamID,
					UserID: user.ID,
					Role:   strings.ToLower(member.Role),
				}); err != nil {
					return fmt.Errorf("failed to add member %q: %w", email, err)
				}
			} else if existingMember.Role != strings.ToLower(member.Role) {
				// Update role
				log.Info("updating member role in managed team", "email", email, "team", cfgTeam.Name,
					"old_role", existingMember.Role, "new_role", member.Role)
				if err := qtx.UpdateTeamMemberRole(ctx, sqlc.UpdateTeamMemberRoleParams{
					Role:   strings.ToLower(member.Role),
					TeamID: teamID,
					UserID: existingMember.UserID,
				}); err != nil {
					return fmt.Errorf("failed to update member role for %q: %w", email, err)
				}
			}
		}
	}

	// Prune members not in config
	if prune {
		for email, member := range currentByEmail {
			if !desiredEmails[strings.ToLower(email)] {
				log.Warn("removing member from managed team (not in config)", "email", email, "team", cfgTeam.Name)
				if err := qtx.RemoveTeamMember(ctx, sqlc.RemoveTeamMemberParams{
					TeamID: teamID,
					UserID: member.UserID,
				}); err != nil {
					return fmt.Errorf("failed to remove member %q: %w", email, err)
				}
			}
		}
	}

	return nil
}

func reconcileTeamSources(ctx context.Context, qtx *sqlc.Queries, teamID int64, cfgTeam config.ProvisionTeam, log *slog.Logger, prune bool) error {
	// Load current source links
	currentLinks, err := qtx.ListTeamSources(ctx, teamID)
	if err != nil {
		return fmt.Errorf("failed to list team sources: %w", err)
	}

	currentSourceIDs := make(map[int64]bool)
	for _, link := range currentLinks {
		currentSourceIDs[link.ID] = true
	}

	desiredSourceIDs := make(map[int64]bool)

	for _, srcName := range cfgTeam.Sources {
		src, err := qtx.GetSourceByNameForProvisioning(ctx, srcName)
		if err != nil {
			return fmt.Errorf("source %q referenced by team %q not found", srcName, cfgTeam.Name)
		}

		desiredSourceIDs[src.ID] = true

		if !currentSourceIDs[src.ID] {
			log.Info("linking source to managed team", "source", srcName, "team", cfgTeam.Name)
			if err := qtx.AddTeamSource(ctx, sqlc.AddTeamSourceParams{
				TeamID:   teamID,
				SourceID: src.ID,
			}); err != nil {
				// Ignore duplicate link errors
				if !strings.Contains(err.Error(), "UNIQUE") {
					return fmt.Errorf("failed to link source %q to team %q: %w", srcName, cfgTeam.Name, err)
				}
			}
		}
	}

	// Prune source links not in config
	if prune {
		for _, link := range currentLinks {
			if !desiredSourceIDs[link.ID] {
				log.Warn("unlinking source from managed team (not in config)", "source_id", link.ID, "team", cfgTeam.Name)
				if err := qtx.RemoveTeamSource(ctx, sqlc.RemoveTeamSourceParams{
					TeamID:   teamID,
					SourceID: link.ID,
				}); err != nil {
					return fmt.Errorf("failed to unlink source %d from team %q: %w", link.ID, cfgTeam.Name, err)
				}
			}
		}
	}

	return nil
}

// Helper functions

func validateSourceConnection(ctx context.Context, ds *datasource.Service, src config.ProvisionSource, log *slog.Logger) error {
	payload, err := src.ConnectionPayload()
	if err != nil {
		return fmt.Errorf("marshal connection config: %w", err)
	}

	_, err = ds.ValidateConnection(ctx, &models.ValidateConnectionRequest{
		SourceType:     src.NormalizedSourceType(),
		Connection: payload,
		TimestampField: src.MetaTSField,
		SeverityField:  src.MetaSeverityField,
	})
	if err != nil {
		log.Debug("provisioning datasource validation failed", "source", src.Name, "error", err)
		return err
	}
	return nil
}

func buildCreateSourceParams(src config.ProvisionSource) (sqlc.CreateSourceParams, error) {
	source, err := buildProvisionedSourceModel(src)
	if err != nil {
		return sqlc.CreateSourceParams{}, err
	}

	return sqlc.CreateSourceParams{
		Name:              src.Name,
		MetaIsAutoCreated: 0,
		SourceType:        source.SourceType.String(),
		MetaTsField:       source.MetaTSField,
		MetaSeverityField: sql.NullString{String: source.MetaSeverityField, Valid: source.MetaSeverityField != ""},
		ConnectionConfig:  string(source.ConnectionConfig),
		IdentityKey:       source.IdentityKey,
		Description:       sql.NullString{String: source.Description, Valid: source.Description != ""},
		TtlDays:           int64(source.TTLDays),
		Managed:           0,
		SecretRef:         sql.NullString{},
	}, nil
}

func updateSourceFromConfig(ctx context.Context, qtx *sqlc.Queries, sourceID int64, src config.ProvisionSource) error {
	source, err := buildProvisionedSourceModel(src)
	if err != nil {
		return err
	}

	return qtx.UpdateSource(ctx, sqlc.UpdateSourceParams{
		Name:              source.Name,
		MetaIsAutoCreated: 0,
		SourceType:        source.SourceType.String(),
		MetaTsField:       source.MetaTSField,
		MetaSeverityField: sql.NullString{String: source.MetaSeverityField, Valid: source.MetaSeverityField != ""},
		ConnectionConfig:  string(source.ConnectionConfig),
		IdentityKey:       source.IdentityKey,
		Description:       sql.NullString{String: source.Description, Valid: source.Description != ""},
		TtlDays:           int64(source.TTLDays),
		Managed:           1,
		SecretRef:         sql.NullString{String: src.SecretRef, Valid: src.SecretRef != ""},
		ID:                sourceID,
	})
}

func sourceNeedsUpdate(existing sqlc.Source, desired config.ProvisionSource) (bool, error) {
	source, err := buildProvisionedSourceModel(desired)
	if err != nil {
		return false, err
	}

	return existing.SourceType != source.SourceType.String() ||
		existing.ConnectionConfig != string(source.ConnectionConfig) ||
		existing.IdentityKey != source.IdentityKey ||
		existing.Description.String != desired.Description ||
		int(existing.TtlDays) != desired.TTLDays ||
		existing.MetaTsField != desired.MetaTSField ||
		existing.MetaSeverityField.String != desired.MetaSeverityField ||
		existing.SecretRef.String != desired.SecretRef, nil
}

func buildProvisionedSourceModel(src config.ProvisionSource) (models.Source, error) {
	connectionPayload, err := src.ConnectionPayload()
	if err != nil {
		return models.Source{}, fmt.Errorf("build provisioned source %q: %w", src.Name, err)
	}

	source := models.Source{
		Name:              src.Name,
		SourceType:        src.NormalizedSourceType(),
		MetaIsAutoCreated: false,
		MetaTSField:       src.MetaTSField,
		MetaSeverityField: src.MetaSeverityField,
		ConnectionConfig:  connectionPayload,
		Description:       src.Description,
		TTLDays:           src.TTLDays,
		Managed:           true,
		SecretRef:         src.SecretRef,
	}
	if source.IsClickHouse() {
		conn, err := src.ClickHouseConnection()
		if err != nil {
			return models.Source{}, err
		}
		source.Connection = conn
	}
	if err := source.SyncConnectionConfig(); err != nil {
		return models.Source{}, err
	}

	return source, nil
}

func buildProvisionedSourceModelWithID(id int64, src config.ProvisionSource) (models.Source, error) {
	source, err := buildProvisionedSourceModel(src)
	if err != nil {
		return models.Source{}, err
	}
	source.ID = models.SourceID(id)
	return source, nil
}
