package provisioning

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// errDryRun is returned from within the reconcile transaction to force a
// rollback on dry_run. It never escapes Reconcile.
var errDryRun = errors.New("provisioning dry-run rollback")

// Reconcile applies the provisioning config to the database.
// It runs in a single write transaction. On dry_run, the transaction is rolled
// back. adminEmails is used to determine global admin role precedence.
func Reconcile(ctx context.Context, cfg *config.ProvisioningConfig, db store.Store, ds *datasource.Service, log *slog.Logger, adminEmails []string) error {
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

	// Sources created/updated in this run, for post-commit datasource setup.
	var sourcesToConnect []models.Source

	// All mutations run in a single transaction. On dry_run we return errDryRun
	// to roll the whole thing back without surfacing an error.
	err := db.WithTx(ctx, func(tx store.StoreOps) error {
		// Phase 1: Sources
		if cfg.ManageSources {
			if err := reconcileSources(ctx, tx, cfg, ds, log, &sourcesToConnect); err != nil {
				return fmt.Errorf("source reconciliation failed: %w", err)
			}
		}

		// Phase 2: Teams (depends on sources being reconciled)
		if cfg.ManageTeams {
			if err := reconcileTeams(ctx, tx, cfg, log, adminSet); err != nil {
				return fmt.Errorf("team reconciliation failed: %w", err)
			}
		}

		if cfg.DryRun {
			return errDryRun
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errDryRun) {
			log.Info("provisioning dry-run complete, rolling back transaction")
			return nil
		}
		return err
	}

	log.Info("provisioning reconciliation committed successfully")

	// Post-commit: establish datasource connections for new/updated sources.
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

func reconcileSources(ctx context.Context, tx store.StoreOps, cfg *config.ProvisioningConfig, ds *datasource.Service, log *slog.Logger, toConnect *[]models.Source) error {
	// Load existing managed sources.
	existingManaged, err := tx.ListManagedSources(ctx)
	if err != nil {
		return fmt.Errorf("failed to list managed sources: %w", err)
	}

	managedByName := make(map[string]*models.Source, len(existingManaged))
	for _, src := range existingManaged {
		managedByName[src.Name] = src
	}

	desiredNames := make(map[string]bool)

	for i := range cfg.Sources {
		cfgSrc := cfg.Sources[i]
		desiredNames[cfgSrc.Name] = true

		existing, isManaged := managedByName[cfgSrc.Name]
		if !isManaged {
			// Check for an unmanaged source with the same name (adopt). Any
			// lookup error is treated as "not found" -> create, matching the
			// prior behavior.
			if unmanaged, err := tx.GetSourceByNameForProvisioning(ctx, cfgSrc.Name); err == nil {
				log.Info("adopting existing source as managed", "name", cfgSrc.Name, "id", unmanaged.ID)
				adopted, err := sourceFromConfig(cfgSrc, unmanaged.ID)
				if err != nil {
					return fmt.Errorf("failed to build provisioned source %q: %w", cfgSrc.Name, err)
				}
				if err := tx.UpdateSource(ctx, adopted); err != nil {
					return fmt.Errorf("failed to adopt source %q: %w", cfgSrc.Name, err)
				}
				if err := tx.SetSourceManaged(ctx, unmanaged.ID, true, cfgSrc.SecretRef); err != nil {
					return fmt.Errorf("failed to set source %q as managed: %w", cfgSrc.Name, err)
				}
				*toConnect = append(*toConnect, *adopted)
				continue
			}

			// Create a new source.
			log.Info("creating managed source", "name", cfgSrc.Name)

			// Validate datasource connectivity before creating (best-effort).
			if err := validateSourceConnection(ctx, ds, cfgSrc, log); err != nil {
				log.Warn("datasource validation failed for provisioned source, creating anyway",
					"name", cfgSrc.Name, "error", err)
			}

			src, err := sourceFromConfig(cfgSrc, 0)
			if err != nil {
				return fmt.Errorf("failed to build provisioned source %q: %w", cfgSrc.Name, err)
			}
			if err := tx.CreateSource(ctx, src); err != nil {
				return fmt.Errorf("failed to create source %q: %w", cfgSrc.Name, err)
			}
			if err := tx.SetSourceManaged(ctx, src.ID, true, cfgSrc.SecretRef); err != nil {
				return fmt.Errorf("failed to set source %q as managed: %w", cfgSrc.Name, err)
			}

			*toConnect = append(*toConnect, *src)
		} else {
			// Update existing managed source if fields changed.
			needsUpdate, err := sourceNeedsUpdate(existing, cfgSrc)
			if err != nil {
				return fmt.Errorf("failed to compare managed source %q: %w", cfgSrc.Name, err)
			}
			if needsUpdate {
				log.Info("updating managed source", "name", cfgSrc.Name)
				updated, err := sourceFromConfig(cfgSrc, existing.ID)
				if err != nil {
					return fmt.Errorf("failed to build provisioned source %q: %w", cfgSrc.Name, err)
				}
				if err := tx.UpdateSource(ctx, updated); err != nil {
					return fmt.Errorf("failed to update source %q: %w", cfgSrc.Name, err)
				}
				*toConnect = append(*toConnect, *updated)
			}
		}
	}

	// Prune: delete managed sources not in config.
	for name, src := range managedByName {
		if desiredNames[name] {
			continue
		}
		if cfg.Prune {
			log.Warn("pruning managed source not in config", "name", name, "id", src.ID)
			if err := tx.DeleteSource(ctx, src.ID); err != nil {
				return fmt.Errorf("failed to prune source %q: %w", name, err)
			}
		} else {
			log.Warn("managed source not in config (prune=false, keeping)", "name", name)
		}
	}

	return nil
}

func reconcileTeams(ctx context.Context, tx store.StoreOps, cfg *config.ProvisioningConfig, log *slog.Logger, adminSet map[string]bool) error {
	// Load existing managed teams.
	existingManaged, err := tx.ListManagedTeams(ctx)
	if err != nil {
		return fmt.Errorf("failed to list managed teams: %w", err)
	}

	managedByName := make(map[string]*models.Team, len(existingManaged))
	for _, team := range existingManaged {
		managedByName[team.Name] = team
	}

	desiredNames := make(map[string]bool)

	for _, cfgTeam := range cfg.Teams {
		desiredNames[cfgTeam.Name] = true

		var teamID models.TeamID

		existing, isManaged := managedByName[cfgTeam.Name]
		if !isManaged {
			// Adopt an existing unmanaged team if present; any lookup error is
			// treated as "not found" -> create, matching the prior behavior.
			if unmanaged, err := tx.GetTeamByName(ctx, cfgTeam.Name); err == nil {
				log.Info("adopting existing team as managed", "name", cfgTeam.Name, "id", unmanaged.ID)
				teamID = unmanaged.ID
				if err := tx.SetTeamManaged(ctx, teamID, true); err != nil {
					return fmt.Errorf("failed to set team %q as managed: %w", cfgTeam.Name, err)
				}
				if unmanaged.Description != cfgTeam.Description {
					if err := updateTeamDescription(ctx, tx, teamID, cfgTeam); err != nil {
						return fmt.Errorf("failed to update team %q: %w", cfgTeam.Name, err)
					}
				}
			} else {
				log.Info("creating managed team", "name", cfgTeam.Name)
				team := &models.Team{Name: cfgTeam.Name, Description: cfgTeam.Description}
				if err := tx.CreateTeam(ctx, team); err != nil {
					return fmt.Errorf("failed to create team %q: %w", cfgTeam.Name, err)
				}
				teamID = team.ID
				if err := tx.SetTeamManaged(ctx, teamID, true); err != nil {
					return fmt.Errorf("failed to set team %q as managed: %w", cfgTeam.Name, err)
				}
			}
		} else {
			teamID = existing.ID
			if existing.Description != cfgTeam.Description {
				if err := updateTeamDescription(ctx, tx, teamID, cfgTeam); err != nil {
					return fmt.Errorf("failed to update team %q: %w", cfgTeam.Name, err)
				}
			}
		}

		if err := reconcileTeamMembers(ctx, tx, teamID, cfgTeam, log, adminSet, cfg.Prune); err != nil {
			return fmt.Errorf("failed to reconcile members for team %q: %w", cfgTeam.Name, err)
		}

		if err := reconcileTeamSources(ctx, tx, teamID, cfgTeam, log, cfg.Prune); err != nil {
			return fmt.Errorf("failed to reconcile sources for team %q: %w", cfgTeam.Name, err)
		}
	}

	// Prune teams not in config.
	for name, team := range managedByName {
		if desiredNames[name] {
			continue
		}
		if cfg.Prune {
			log.Warn("pruning managed team not in config (cascades to saved queries/alerts)", "name", name, "id", team.ID)
			if err := tx.DeleteTeam(ctx, team.ID); err != nil {
				return fmt.Errorf("failed to prune team %q: %w", name, err)
			}
		} else {
			log.Warn("managed team not in config (prune=false, keeping)", "name", name)
		}
	}

	return nil
}

func reconcileTeamMembers(ctx context.Context, tx store.StoreOps, teamID models.TeamID, cfgTeam config.ProvisionTeam, log *slog.Logger, adminSet map[string]bool, prune bool) error {
	// Load current members, keyed by user email.
	currentMembers, err := tx.ListTeamMembers(ctx, teamID)
	if err != nil {
		return fmt.Errorf("failed to list team members: %w", err)
	}

	currentByEmail := make(map[string]*models.TeamMember, len(currentMembers))
	for _, m := range currentMembers {
		user, err := tx.GetUser(ctx, m.UserID)
		if err != nil {
			continue
		}
		currentByEmail[user.Email] = m
	}

	desiredEmails := make(map[string]bool)

	for _, member := range cfgTeam.Members {
		email := strings.ToLower(member.Email)
		desiredEmails[email] = true
		role := models.TeamRole(strings.ToLower(member.Role))

		user, err := tx.GetUserByEmail(ctx, email)
		if err != nil {
			// User doesn't exist (or lookup failed): create a managed stub.
			log.Info("creating managed user for team membership", "email", email, "team", cfgTeam.Name)

			globalRole := models.UserRoleMember
			if adminSet[email] {
				globalRole = models.UserRoleAdmin
			}

			newUser := &models.User{
				Email:       email,
				FullName:    email, // Placeholder, updated on first OIDC login.
				Role:        globalRole,
				Status:      models.UserStatusActive,
				AccountType: models.UserAccountTypeHuman,
			}
			if err := tx.CreateUser(ctx, newUser); err != nil {
				return fmt.Errorf("failed to create user %q: %w", email, err)
			}
			if err := tx.SetUserManaged(ctx, newUser.ID, true); err != nil {
				return fmt.Errorf("failed to set user %q as managed: %w", email, err)
			}
			if err := tx.AddTeamMember(ctx, teamID, newUser.ID, role); err != nil {
				return fmt.Errorf("failed to add member %q to team: %w", email, err)
			}
			continue
		}

		// User exists: mark managed if config references it.
		if !user.Managed {
			if err := tx.SetUserManaged(ctx, user.ID, true); err != nil {
				return fmt.Errorf("failed to set user %q as managed: %w", email, err)
			}
		}

		// Ensure membership with the correct role.
		existingMember, hasMembership := currentByEmail[user.Email]
		switch {
		case !hasMembership:
			log.Info("adding member to managed team", "email", email, "team", cfgTeam.Name, "role", member.Role)
			if err := tx.AddTeamMember(ctx, teamID, user.ID, role); err != nil {
				return fmt.Errorf("failed to add member %q: %w", email, err)
			}
		case existingMember.Role != role:
			log.Info("updating member role in managed team", "email", email, "team", cfgTeam.Name,
				"old_role", existingMember.Role, "new_role", member.Role)
			if err := tx.UpdateTeamMemberRole(ctx, teamID, existingMember.UserID, role); err != nil {
				return fmt.Errorf("failed to update member role for %q: %w", email, err)
			}
		}
	}

	// Prune members not in config.
	if prune {
		for email, member := range currentByEmail {
			if desiredEmails[strings.ToLower(email)] {
				continue
			}
			log.Warn("removing member from managed team (not in config)", "email", email, "team", cfgTeam.Name)
			if err := tx.RemoveTeamMember(ctx, teamID, member.UserID); err != nil {
				return fmt.Errorf("failed to remove member %q: %w", email, err)
			}
		}
	}

	return nil
}

func reconcileTeamSources(ctx context.Context, tx store.StoreOps, teamID models.TeamID, cfgTeam config.ProvisionTeam, log *slog.Logger, prune bool) error {
	// Load current source links.
	currentLinks, err := tx.ListTeamSources(ctx, teamID)
	if err != nil {
		return fmt.Errorf("failed to list team sources: %w", err)
	}

	currentSourceIDs := make(map[models.SourceID]bool, len(currentLinks))
	for _, link := range currentLinks {
		currentSourceIDs[link.ID] = true
	}

	desiredSourceIDs := make(map[models.SourceID]bool)

	for _, srcName := range cfgTeam.Sources {
		src, err := tx.GetSourceByNameForProvisioning(ctx, srcName)
		if err != nil {
			return fmt.Errorf("source %q referenced by team %q not found", srcName, cfgTeam.Name)
		}

		desiredSourceIDs[src.ID] = true

		if !currentSourceIDs[src.ID] {
			log.Info("linking source to managed team", "source", srcName, "team", cfgTeam.Name)
			if err := tx.AddTeamSource(ctx, teamID, src.ID); err != nil {
				// Ignore duplicate link errors.
				if !strings.Contains(err.Error(), "UNIQUE") {
					return fmt.Errorf("failed to link source %q to team %q: %w", srcName, cfgTeam.Name, err)
				}
			}
		}
	}

	// Prune source links not in config.
	if prune {
		for _, link := range currentLinks {
			if desiredSourceIDs[link.ID] {
				continue
			}
			log.Warn("unlinking source from managed team (not in config)", "source_id", link.ID, "team", cfgTeam.Name)
			if err := tx.RemoveTeamSource(ctx, teamID, link.ID); err != nil {
				return fmt.Errorf("failed to unlink source %d from team %q: %w", link.ID, cfgTeam.Name, err)
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
		Connection:     payload,
		TimestampField: src.MetaTSField,
		SeverityField:  src.MetaSeverityField,
	})
	if err != nil {
		log.Debug("provisioning datasource validation failed", "source", src.Name, "error", err)
		return err
	}
	return nil
}

// sourceFromConfig builds a Source model from provisioning config. id is 0 for
// a create (CreateSource assigns the real id) or the existing id for an update.
func sourceFromConfig(src config.ProvisionSource, id models.SourceID) (*models.Source, error) {
	source, err := buildProvisionedSourceModel(src)
	if err != nil {
		return nil, err
	}
	source.ID = id
	return &source, nil
}

// updateTeamDescription updates a team's name/description, stamping updated_at.
func updateTeamDescription(ctx context.Context, tx store.StoreOps, teamID models.TeamID, cfgTeam config.ProvisionTeam) error {
	return tx.UpdateTeam(ctx, &models.Team{
		ID:          teamID,
		Name:        cfgTeam.Name,
		Description: cfgTeam.Description,
		Timestamps:  models.Timestamps{UpdatedAt: time.Now()},
	})
}

func sourceNeedsUpdate(existing *models.Source, desired config.ProvisionSource) (bool, error) {
	source, err := buildProvisionedSourceModel(desired)
	if err != nil {
		return false, err
	}

	return existing.SourceType != source.SourceType ||
		string(existing.ConnectionConfig) != string(source.ConnectionConfig) ||
		existing.IdentityKey != source.IdentityKey ||
		existing.Description != desired.Description ||
		existing.TTLDays != desired.TTLDays ||
		existing.MetaTSField != desired.MetaTSField ||
		existing.MetaSeverityField != desired.MetaSeverityField ||
		existing.SecretRef != desired.SecretRef, nil
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
