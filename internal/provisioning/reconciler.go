package provisioning

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"reflect"
	"time"

	"github.com/google/uuid"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// errDryRun is returned from within the reconcile transaction to force a
// rollback on dry_run. It never escapes Reconcile.
var errDryRun = errors.New("provisioning dry-run rollback")

// auditLog collects the events of one reconcile run. Reconcile records them
// only after the transaction commits, so a rollback or a dry run leaves no
// row and no log line. Recording inside the transaction would wait on SQLite
// for the write connection that the transaction holds.
type auditLog struct {
	actor  audit.Actor
	events []audit.Event
}

func (l *auditLog) add(action audit.Action, target audit.Target, before, after *audit.Fields) {
	l.addWithDetail(action, target, before, after, nil)
}

func (l *auditLog) addWithDetail(action audit.Action, target audit.Target, before, after, detail *audit.Fields) {
	l.events = append(l.events, audit.Event{
		Action:  action,
		Outcome: models.AuditOutcomeSuccess,
		Actor:   l.actor,
		Target:  target,
		Before:  before,
		After:   after,
		Detail:  detail,
	})
}

// Reconcile applies the provisioning config to the database.
// It runs in a single write transaction. On dry_run, the transaction is rolled
// back. adminEmails is used to determine global admin role precedence. Each
// committed change is recorded as an audit event by the system actor, with
// one run ID per call; a failed audit insert does not undo the reconcile.
func Reconcile(ctx context.Context, cfg *config.ProvisioningConfig, db store.Store, ds *datasource.Service, rec *audit.Recorder, log *slog.Logger, adminEmails []string) error {
	if !cfg.Enabled() {
		return nil
	}

	ResolveConfig(cfg)
	if err := ValidateConfig(cfg, ds); err != nil {
		return err
	}

	// Build admin email set for role precedence
	adminSet := make(map[string]bool)
	for _, email := range adminEmails {
		adminSet[models.NormalizeEmail(email)] = true
	}

	if err := validateNewSources(ctx, cfg, db, ds, log); err != nil {
		return err
	}

	runID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate provisioning run id: %w", err)
	}
	events := &auditLog{actor: audit.Actor{
		Type:         models.AuditActorSystem,
		Label:        "provisioning",
		AuthMethod:   models.AuditAuthProvisioning,
		CredentialID: runID.String(),
	}}

	// Sources created/updated in this run, for post-commit datasource setup.
	var sourcesToConnect []models.Source

	// All mutations run in a single transaction. On dry_run we return errDryRun
	// to roll the whole thing back without surfacing an error.
	err = db.WithTx(ctx, func(tx store.StoreOps) error {
		// Share the deletion lock because pruning cascades beyond configured users.
		if err := tx.LockMembershipWrites(ctx); err != nil {
			return err
		}
		usersByEmail, err := loadProvisioningUsers(ctx, tx, cfg)
		if err != nil {
			return err
		}

		// Phase 1: Sources
		if cfg.ManageSources {
			if err := reconcileSources(ctx, tx, cfg, log, events, &sourcesToConnect); err != nil {
				return fmt.Errorf("source reconciliation failed: %w", err)
			}
			if err := reconcileSourceViews(ctx, tx, cfg, log, events); err != nil {
				return fmt.Errorf("source view reconciliation failed: %w", err)
			}
		}

		// Phase 2: Teams (depends on sources being reconciled)
		if cfg.ManageTeams {
			if err := reconcileTeams(ctx, tx, cfg, log, events, adminSet, usersByEmail); err != nil {
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

	log.Info("provisioning reconciliation committed successfully", "run_id", runID.String(), "changes", len(events.events))
	for i := range events.events {
		rec.Record(ctx, events.events[i])
	}

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

func reconcileSources(ctx context.Context, tx store.StoreOps, cfg *config.ProvisioningConfig, log *slog.Logger, events *auditLog, toConnect *[]models.Source) error {
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

		if existing, isManaged := managedByName[cfgSrc.Name]; isManaged {
			if err := updateManagedSourceIfNeeded(ctx, tx, log, events, cfgSrc, existing, toConnect); err != nil {
				return err
			}
		} else {
			if err := createOrAdoptSource(ctx, tx, log, events, cfgSrc, toConnect); err != nil {
				return err
			}
		}
	}

	return pruneManagedSources(ctx, tx, cfg, log, events, managedByName, desiredNames)
}

// createOrAdoptSource handles a config source with no managed counterpart:
// it adopts a same-named unmanaged source if one exists, otherwise creates a
// new managed source.
func createOrAdoptSource(ctx context.Context, tx store.StoreOps, log *slog.Logger, events *auditLog, cfgSrc config.ProvisionSource, toConnect *[]models.Source) error {
	if found, err := tx.GetSourceByNameForProvisioning(ctx, cfgSrc.Name); err == nil {
		// Read the row again under its lock, so the audit diff is the row
		// that this update replaces and not one an admin changed since.
		unmanaged, err := tx.GetSourceForUpdate(ctx, found.ID)
		if err != nil {
			return fmt.Errorf("failed to lock source %q: %w", cfgSrc.Name, err)
		}
		log.Info("adopting existing source as managed", "name", cfgSrc.Name, "id", unmanaged.ID)
		adopted, err := sourceFromConfig(cfgSrc, unmanaged.ID)
		if err != nil {
			return fmt.Errorf("failed to build provisioned source %q: %w", cfgSrc.Name, err)
		}
		if err := rejectLokiIdentityChange(unmanaged, adopted); err != nil {
			return err
		}
		if err := tx.UpdateSource(ctx, adopted); err != nil {
			return fmt.Errorf("failed to adopt source %q: %w", cfgSrc.Name, err)
		}
		if err := tx.SetSourceManaged(ctx, unmanaged.ID, true, cfgSrc.SecretRef); err != nil {
			return fmt.Errorf("failed to set source %q as managed: %w", cfgSrc.Name, err)
		}
		events.add(audit.SourceUpdate, audit.SourceTarget(adopted), audit.SourceFields(unmanaged), audit.SourceFields(adopted))
		*toConnect = append(*toConnect, *adopted)
		return nil
	} else if !models.IsNotFound(err) {
		return fmt.Errorf("failed to look up source %q: %w", cfgSrc.Name, err)
	}

	// Create a new source.
	log.Info("creating managed source", "name", cfgSrc.Name)

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
	events.add(audit.SourceCreate, audit.SourceTarget(src), nil, audit.SourceFields(src))

	*toConnect = append(*toConnect, *src)
	return nil
}

// updateManagedSourceIfNeeded updates an already-managed source in place when
// its desired config diverges from the persisted row.
func updateManagedSourceIfNeeded(ctx context.Context, tx store.StoreOps, log *slog.Logger, events *auditLog, cfgSrc config.ProvisionSource, existing *models.Source, toConnect *[]models.Source) error {
	needsUpdate, err := sourceNeedsUpdate(existing, cfgSrc)
	if err != nil {
		return fmt.Errorf("failed to compare managed source %q: %w", cfgSrc.Name, err)
	}
	if !needsUpdate {
		return nil
	}

	log.Info("updating managed source", "name", cfgSrc.Name)
	updated, err := sourceFromConfig(cfgSrc, existing.ID)
	if err != nil {
		return fmt.Errorf("failed to build provisioned source %q: %w", cfgSrc.Name, err)
	}
	if err := rejectLokiIdentityChange(existing, updated); err != nil {
		return err
	}
	if err := tx.UpdateSource(ctx, updated); err != nil {
		return fmt.Errorf("failed to update source %q: %w", cfgSrc.Name, err)
	}
	events.add(audit.SourceUpdate, audit.SourceTarget(updated), audit.SourceFields(existing), audit.SourceFields(updated))
	*toConnect = append(*toConnect, *updated)
	return nil
}

// pruneManagedSources deletes (or, with prune=false, just logs) managed
// sources that are no longer declared in config.
func pruneManagedSources(ctx context.Context, tx store.StoreOps, cfg *config.ProvisioningConfig, log *slog.Logger, events *auditLog, managedByName map[string]*models.Source, desiredNames map[string]bool) error {
	for name, src := range managedByName {
		if desiredNames[name] {
			continue
		}
		if cfg.Prune {
			log.Warn("pruning managed source not in config", "name", name, "id", src.ID)
			if err := tx.DeleteSource(ctx, src.ID); err != nil {
				return fmt.Errorf("failed to prune source %q: %w", name, err)
			}
			events.add(audit.SourceDelete, audit.SourceTarget(src), audit.SourceFields(src), nil)
		} else {
			log.Warn("managed source not in config (prune=false, keeping)", "name", name)
		}
	}
	return nil
}

func reconcileTeams(ctx context.Context, tx store.StoreOps, cfg *config.ProvisioningConfig, log *slog.Logger, events *auditLog, adminSet map[string]bool, usersByEmail map[string]*models.User) error {
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

		teamID, err := ensureManagedTeam(ctx, tx, log, events, cfgTeam, managedByName[cfgTeam.Name])
		if err != nil {
			return err
		}

		if err := reconcileTeamMembers(ctx, tx, teamID, cfgTeam, log, events, adminSet, usersByEmail, cfg.Prune); err != nil {
			return fmt.Errorf("failed to reconcile members for team %q: %w", cfgTeam.Name, err)
		}

		if err := reconcileTeamSources(ctx, tx, teamID, cfgTeam, log, events, cfg.Prune); err != nil {
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
			events.add(audit.TeamDelete, audit.TeamTarget(team), audit.TeamFields(team), nil)
		} else {
			log.Warn("managed team not in config (prune=false, keeping)", "name", name)
		}
	}

	return nil
}

// ensureManagedTeam returns the ID of the managed team that cfgTeam declares.
// It updates the description of existing, the managed team of that name if
// there is one, and otherwise adopts an unmanaged team of that name or
// creates the team.
func ensureManagedTeam(ctx context.Context, tx store.StoreOps, log *slog.Logger, events *auditLog, cfgTeam config.ProvisionTeam, existing *models.Team) (models.TeamID, error) {
	if existing != nil {
		if existing.Description != cfgTeam.Description {
			if err := updateTeamDescription(ctx, tx, existing.ID, cfgTeam); err != nil {
				return 0, fmt.Errorf("failed to update team %q: %w", cfgTeam.Name, err)
			}
			updated := *existing
			updated.Description = cfgTeam.Description
			events.add(audit.TeamUpdate, audit.TeamTarget(&updated), audit.TeamFields(existing), audit.TeamFields(&updated))
		}
		return existing.ID, nil
	}

	found, err := tx.GetTeamByName(ctx, cfgTeam.Name)
	if models.IsNotFound(err) {
		log.Info("creating managed team", "name", cfgTeam.Name)
		team := &models.Team{Name: cfgTeam.Name, Description: cfgTeam.Description}
		if err := tx.CreateTeam(ctx, team); err != nil {
			return 0, fmt.Errorf("failed to create team %q: %w", cfgTeam.Name, err)
		}
		if err := tx.SetTeamManaged(ctx, team.ID, true); err != nil {
			return 0, fmt.Errorf("failed to set team %q as managed: %w", cfgTeam.Name, err)
		}
		team.Managed = true
		events.add(audit.TeamCreate, audit.TeamTarget(team), nil, audit.TeamFields(team))
		return team.ID, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to look up team %q: %w", cfgTeam.Name, err)
	}

	// Decide and snapshot from the locked row, so a concurrent admin edit is
	// neither lost nor attributed to this run.
	unmanaged, err := tx.GetTeamForUpdate(ctx, found.ID)
	if err != nil {
		return 0, fmt.Errorf("failed to lock team %q: %w", cfgTeam.Name, err)
	}
	log.Info("adopting existing team as managed", "name", cfgTeam.Name, "id", unmanaged.ID)
	if err := tx.SetTeamManaged(ctx, unmanaged.ID, true); err != nil {
		return 0, fmt.Errorf("failed to set team %q as managed: %w", cfgTeam.Name, err)
	}
	if unmanaged.Description != cfgTeam.Description {
		if err := updateTeamDescription(ctx, tx, unmanaged.ID, cfgTeam); err != nil {
			return 0, fmt.Errorf("failed to update team %q: %w", cfgTeam.Name, err)
		}
	}
	adopted := *unmanaged
	adopted.Description, adopted.Managed = cfgTeam.Description, true
	events.add(audit.TeamUpdate, audit.TeamTarget(&adopted), audit.TeamFields(unmanaged), audit.TeamFields(&adopted))
	return unmanaged.ID, nil
}

func reconcileTeamMembers(ctx context.Context, tx store.StoreOps, teamID models.TeamID, cfgTeam config.ProvisionTeam, log *slog.Logger, events *auditLog, adminSet map[string]bool, usersByEmail map[string]*models.User, prune bool) error {
	// Load current members, keyed by user email.
	currentMembers, err := tx.ListTeamMembersWithDetails(ctx, teamID)
	if err != nil {
		return fmt.Errorf("failed to list team members: %w", err)
	}

	currentByEmail := make(map[string]*models.TeamMember, len(currentMembers))
	for _, m := range currentMembers {
		currentByEmail[m.Email] = m
	}

	desiredEmails := make(map[string]bool)

	for _, member := range cfgTeam.Members {
		email := member.Email
		desiredEmails[email] = true
		role := models.TeamRole(member.Role)

		user, exists := usersByEmail[email]
		if !exists {
			log.Info("creating managed user for team membership", "email", email, "team", cfgTeam.Name)

			globalRole := models.UserRoleMember
			if adminSet[email] {
				globalRole = models.UserRoleAdmin
			}

			newUser, err := createManagedUser(ctx, tx, email, globalRole)
			if err != nil {
				return err
			}
			newUser.Managed = true
			usersByEmail[email] = newUser
			events.add(audit.UserCreate, audit.UserTarget(newUser), nil, audit.UserFields(newUser))
			if err := tx.AddTeamMember(ctx, teamID, newUser.ID, role); err != nil {
				return fmt.Errorf("failed to add member %q to team: %w", email, err)
			}
			events.add(audit.TeamMemberAdd, audit.TeamMemberTarget(teamID, newUser.ID), nil,
				audit.TeamMemberFields(&models.TeamMember{TeamID: teamID, UserID: newUser.ID, Role: role}))
			continue
		}

		// User exists: mark managed if config references it.
		if !user.Managed {
			if err := tx.SetUserManaged(ctx, user.ID, true); err != nil {
				return fmt.Errorf("failed to set user %q as managed: %w", email, err)
			}
			before := audit.UserFields(user)
			user.Managed = true
			events.add(audit.UserUpdate, audit.UserTarget(user), before, audit.UserFields(user))
		}

		// Ensure membership with the correct role.
		existingMember, hasMembership := currentByEmail[user.Email]
		switch {
		case !hasMembership:
			log.Info("adding member to managed team", "email", email, "team", cfgTeam.Name, "role", member.Role)
			if err := tx.AddTeamMember(ctx, teamID, user.ID, role); err != nil {
				return fmt.Errorf("failed to add member %q: %w", email, err)
			}
			events.add(audit.TeamMemberAdd, audit.TeamMemberTarget(teamID, user.ID), nil,
				audit.TeamMemberFields(&models.TeamMember{TeamID: teamID, UserID: user.ID, Role: role}))
		case existingMember.Role != role:
			log.Info("updating member role in managed team", "email", email, "team", cfgTeam.Name,
				"old_role", existingMember.Role, "new_role", member.Role)
			if err := tx.UpdateTeamMemberRole(ctx, teamID, existingMember.UserID, role); err != nil {
				return fmt.Errorf("failed to update member role for %q: %w", email, err)
			}
			updated := *existingMember
			updated.Role = role
			events.add(audit.TeamMemberAdd, audit.TeamMemberTarget(teamID, existingMember.UserID),
				audit.TeamMemberFields(existingMember), audit.TeamMemberFields(&updated))
		}
	}

	// Prune members not in config.
	if prune {
		for email, member := range currentByEmail {
			if desiredEmails[email] {
				continue
			}
			log.Warn("removing member from managed team (not in config)", "email", email, "team", cfgTeam.Name)
			if err := tx.RemoveTeamMember(ctx, teamID, member.UserID); err != nil {
				return fmt.Errorf("failed to remove member %q: %w", email, err)
			}
			events.add(audit.TeamMemberRemove, audit.TeamMemberTarget(teamID, member.UserID), audit.TeamMemberFields(member), nil)
		}
	}

	return nil
}

func reconcileTeamSources(ctx context.Context, tx store.StoreOps, teamID models.TeamID, cfgTeam config.ProvisionTeam, log *slog.Logger, events *auditLog, prune bool) error {
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
			return fmt.Errorf("failed to look up source %q referenced by team %q: %w", srcName, cfgTeam.Name, err)
		}

		desiredSourceIDs[src.ID] = true

		if !currentSourceIDs[src.ID] {
			log.Info("linking source to managed team", "source", srcName, "team", cfgTeam.Name)
			if err := tx.AddTeamSource(ctx, teamID, src.ID); err != nil {
				return fmt.Errorf("failed to link source %q to team %q: %w", srcName, cfgTeam.Name, err)
			}
			events.add(audit.TeamSourceLink, audit.TeamSourceTarget(teamID, src.ID), nil, audit.TeamSourceFields(teamID, src.ID))
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
			events.add(audit.TeamSourceUnlink, audit.TeamSourceTarget(teamID, link.ID), audit.TeamSourceFields(teamID, link.ID), nil)
		}
	}

	return nil
}

// Helper functions

func validateNewSources(ctx context.Context, cfg *config.ProvisioningConfig, db store.StoreOps, ds *datasource.Service, log *slog.Logger) error {
	if !cfg.ManageSources {
		return nil
	}
	for i := range cfg.Sources {
		src := &cfg.Sources[i]
		if _, err := db.GetSourceByNameForProvisioning(ctx, src.Name); err != nil {
			if !models.IsNotFound(err) {
				return fmt.Errorf("failed to look up source %q: %w", src.Name, err)
			}
			if err := validateSourceConnection(ctx, ds, *src, log); err != nil {
				log.Warn("datasource validation failed for provisioned source, creating anyway", "name", src.Name, "error", err)
			}
		}
	}
	return nil
}

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

// rejectLokiIdentityChange keeps the identity of a Loki source fixed, as the
// API does: its team links and saved queries were granted for one URL, tenant,
// selector and principal, and must not follow a changed one.
func rejectLokiIdentityChange(existing, desired *models.Source) error {
	if !existing.IsLoki() || existing.IdentityKey == desired.IdentityKey {
		return nil
	}
	return fmt.Errorf("loki source %q: base_url, tenant_id, selector and principal cannot change after creation; "+
		"declare a source with a new name for the new connection", existing.Name)
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
		!sameJSON(existing.ConnectionConfig, source.ConnectionConfig) ||
		existing.IdentityKey != source.IdentityKey ||
		existing.Description != desired.Description ||
		existing.TTLDays != desired.TTLDays ||
		existing.MetaTSField != desired.MetaTSField ||
		existing.MetaSeverityField != desired.MetaSeverityField ||
		existing.SecretRef != desired.SecretRef ||
		!maps.Equal(existing.HeaderSecretRefs, source.HeaderSecretRefs), nil
}

// sameJSON compares two JSON documents by value. Postgres stores
// connection_config as JSONB, which reorders keys and drops spacing, so the
// stored text never matches the marshaled config byte for byte. A document
// that does not parse is different, so the update rewrites it.
func sameJSON(a, b []byte) bool {
	if bytes.Equal(a, b) {
		return true
	}
	var av, bv any
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
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
		HeaderSecretRefs:  customHeaderRefs(src.Name, src.HeaderSecretRefs),
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

func createManagedUser(ctx context.Context, tx store.StoreOps, email string, role models.UserRole) (*models.User, error) {
	user := &models.User{
		Email:       email,
		FullName:    email,
		Role:        role,
		Status:      models.UserStatusActive,
		AccountType: models.UserAccountTypeHuman,
	}
	if err := tx.CreateUser(ctx, user); err != nil {
		return nil, fmt.Errorf("failed to create user %q: %w", email, err)
	}
	if err := tx.SetUserManaged(ctx, user.ID, true); err != nil {
		return nil, fmt.Errorf("failed to set user %q as managed: %w", email, err)
	}
	return user, nil
}
