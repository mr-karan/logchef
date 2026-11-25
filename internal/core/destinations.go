package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/notify"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

var (
	// ErrDestinationNotFound is returned for a missing destination.
	ErrDestinationNotFound = errors.New("notification destination not found")
	// ErrInvalidDestination wraps a validation failure; the message names the field.
	ErrInvalidDestination = errors.New("invalid notification destination")
	// ErrDestinationForbidden is returned when the caller may not create,
	// change or use the destination.
	ErrDestinationForbidden = errors.New("not allowed for this notification destination")
	// ErrDestinationInUse is returned on delete while rules use the destination.
	ErrDestinationInUse = errors.New("notification destination is in use")
)

const (
	// MaxDestinationsPerRule bounds the destinations of one alert rule.
	MaxDestinationsPerRule = 10
	// MaxTeamDestinations bounds the destinations of one team.
	MaxTeamDestinations      = 100
	maxDestinationNameLength = 200
)

// teamRole returns the user's role in the team, or "" for a non-member.
func teamRole(ctx context.Context, db store.StoreOps, teamID models.TeamID, userID models.UserID) (models.TeamRole, error) {
	member, err := db.GetTeamMember(ctx, teamID, userID)
	if err != nil {
		return "", fmt.Errorf("failed to check team membership: %w", err)
	}
	if member == nil {
		return "", nil
	}
	return member.Role, nil
}

// CanUseDestination reports whether the user may put the destination on a
// rule: the creator of a private one, the members of a team one, everyone
// for a global one.
func CanUseDestination(ctx context.Context, db store.StoreOps, user *models.User, d *models.NotificationDestination) (bool, error) {
	switch d.Scope {
	case models.DestinationScopeGlobal:
		return true, nil
	case models.DestinationScopePrivate:
		return d.CreatedBy != nil && *d.CreatedBy == user.ID, nil
	case models.DestinationScopeTeam:
		role, err := teamRole(ctx, db, *d.TeamID, user.ID)
		return role != "", err
	}
	return false, nil
}

// canEditDestination: global admins edit everything; the creator edits a
// private or team destination; team owners and admins edit a team one.
func canEditDestination(ctx context.Context, db store.StoreOps, user *models.User, d *models.NotificationDestination) (bool, error) {
	if user.Role == models.UserRoleAdmin {
		return true, nil
	}
	if d.Scope == models.DestinationScopeGlobal {
		return false, nil
	}
	if d.CreatedBy != nil && *d.CreatedBy == user.ID {
		return true, nil
	}
	if d.Scope == models.DestinationScopeTeam {
		role, err := teamRole(ctx, db, *d.TeamID, user.ID)
		return role == models.TeamRoleOwner || role == models.TeamRoleAdmin, err
	}
	return false, nil
}

// canCreateInScope: anyone creates a private destination, a team owner, admin
// or editor a team one, a global admin a global one.
func canCreateInScope(ctx context.Context, db store.StoreOps, user *models.User, scope models.DestinationScope, teamID *models.TeamID) (bool, error) {
	if user.Role == models.UserRoleAdmin {
		return true, nil
	}
	switch scope {
	case models.DestinationScopePrivate:
		return true, nil
	case models.DestinationScopeTeam:
		role, err := teamRole(ctx, db, *teamID, user.ID)
		return role == models.TeamRoleOwner || role == models.TeamRoleAdmin || role == models.TeamRoleEditor, err
	case models.DestinationScopeGlobal:
		return false, nil
	}
	return false, nil
}

func invalidDestination(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidDestination, fmt.Sprintf(format, args...))
}

// buildDestination validates req into d. existing is nil on create. A blank
// secret on update keeps the stored one.
func buildDestination(ctx context.Context, db store.StoreOps, user *models.User, req *models.DestinationRequest, existing *models.NotificationDestination) (*models.NotificationDestination, error) {
	d := &models.NotificationDestination{IsActive: true}
	if existing != nil {
		*d = *existing
		if req.Type != "" && req.Type != existing.Type {
			return nil, invalidDestination("type cannot change; create a new destination")
		}
	} else {
		d.Type = req.Type
		d.CreatedBy = &user.ID
	}
	if req.IsActive != nil {
		d.IsActive = *req.IsActive
	}
	d.Name = strings.TrimSpace(req.Name)
	if d.Name == "" || len(d.Name) > maxDestinationNameLength {
		return nil, invalidDestination("name is required and has at most %d characters", maxDestinationNameLength)
	}

	if err := applyDestinationScope(ctx, db, d, req); err != nil {
		return nil, err
	}
	if err := applyDestinationConfig(ctx, db, user, d, req, existing); err != nil {
		return nil, err
	}
	return d, nil
}

// applyDestinationScope sets and checks the scope and team of d.
func applyDestinationScope(ctx context.Context, db store.StoreOps, d *models.NotificationDestination, req *models.DestinationRequest) error {
	d.Scope, d.TeamID = req.Scope, req.TeamID
	switch d.Scope {
	case models.DestinationScopePrivate, models.DestinationScopeGlobal:
		if d.TeamID != nil {
			return invalidDestination("team_id is set only for scope team")
		}
	case models.DestinationScopeTeam:
		if d.TeamID == nil {
			return invalidDestination("team_id is required for scope team")
		}
		if _, err := db.GetTeam(ctx, *d.TeamID); err != nil {
			if models.IsNotFound(err) {
				return invalidDestination("team_id: team %d not found", *d.TeamID)
			}
			return fmt.Errorf("failed to load team: %w", err)
		}
	default:
		return invalidDestination("scope must be private, team or global")
	}

	return nil
}

// applyDestinationConfig sets and checks the config and secret of d for its
// type.
func applyDestinationConfig(ctx context.Context, db store.StoreOps, user *models.User, d *models.NotificationDestination, req *models.DestinationRequest, existing *models.NotificationDestination) error {
	switch d.Type {
	case models.DestinationTypeEmail:
		return applyEmailConfig(ctx, db, user, d, req, existing)
	case models.DestinationTypeWebhook:
		if len(req.Config.UserIDs) > 0 || len(req.Config.Addresses) > 0 {
			return invalidDestination("config: a webhook destination has no recipients")
		}
		// An omitted version keeps the stored one; a new destination gets
		// version 2.
		version := req.Config.PayloadVersion
		switch {
		case version == 0 && existing != nil:
			version = existing.Config.PayloadVersion
		case version == 0:
			version = notify.WebhookPayloadV2
		case version != notify.WebhookPayloadV1 && version != notify.WebhookPayloadV2:
			return invalidDestination("config.payload_version must be 1 or 2")
		}
		d.Config = models.DestinationConfig{PayloadVersion: version}
		return applyDestinationSecret(d, req, "webhook URL", notify.ValidateWebhookURL)
	case models.DestinationTypeSlack:
		if len(req.Config.UserIDs) > 0 || len(req.Config.Addresses) > 0 || req.Config.PayloadVersion != 0 {
			return invalidDestination("config: a Slack destination has no settings besides its URL")
		}
		d.Config = models.DestinationConfig{}
		return applyDestinationSecret(d, req, "Slack webhook URL", notify.ValidateSlackWebhookURL)
	case models.DestinationTypePagerDuty:
		if len(req.Config.UserIDs) > 0 || len(req.Config.Addresses) > 0 || req.Config.PayloadVersion != 0 {
			return invalidDestination("config: a PagerDuty destination has no settings besides its routing key")
		}
		d.Config = models.DestinationConfig{}
		return applyDestinationSecret(d, req, "PagerDuty routing key", notify.ValidatePagerDutyRoutingKey)
	default:
		return invalidDestination("type must be email, webhook, slack or pagerduty")
	}
}

func applyEmailConfig(ctx context.Context, db store.StoreOps, user *models.User, d *models.NotificationDestination, req *models.DestinationRequest, existing *models.NotificationDestination) error {
	config, err := notify.NormalizeEmailConfig(req.Config)
	if err != nil {
		return invalidDestination("config: %v", err)
	}
	for _, id := range config.UserIDs {
		if _, err := db.GetUser(ctx, id); err != nil {
			if models.IsNotFound(err) {
				return invalidDestination("config.user_ids: user %d not found", id)
			}
			return fmt.Errorf("failed to load user: %w", err)
		}
	}
	addressesChanged := existing == nil || !slices.Equal(existing.Config.Addresses, config.Addresses)
	if len(config.Addresses) > 0 && addressesChanged && user.Role != models.UserRoleAdmin {
		return fmt.Errorf("%w: only a global admin can add an email address that is not a Logchef user", ErrDestinationForbidden)
	}
	if req.Secret != "" {
		return invalidDestination("secret is not used by an email destination")
	}
	d.Config, d.Secret = config, ""
	return nil
}

// applyDestinationSecret checks and sets a new secret; a blank one keeps the
// stored secret, and a destination needs one.
func applyDestinationSecret(d *models.NotificationDestination, req *models.DestinationRequest, what string, validate func(string) error) error {
	if secret := strings.TrimSpace(req.Secret); secret != "" {
		if err := validate(secret); err != nil {
			return invalidDestination("secret: %v", err)
		}
		d.Secret = secret
	}
	if d.Secret == "" {
		return invalidDestination("secret: the %s is required", what)
	}
	return nil
}

// ListDestinationsForUser lists the destinations the user can use. A global
// admin sees every destination, so it can manage them.
func ListDestinationsForUser(ctx context.Context, db store.StoreOps, user *models.User) ([]*models.NotificationDestination, error) {
	if user.Role == models.UserRoleAdmin {
		return db.ListNotificationDestinations(ctx)
	}
	return db.ListUsableNotificationDestinations(ctx, user.ID)
}

// ListDestinationSummariesForPrincipal lists the destinations the principal
// may use in a rule, without config or secret hints. Only a global admin's
// session or token lists every destination; an OAuth principal never does.
func ListDestinationSummariesForPrincipal(ctx context.Context, db store.StoreOps, p access.Principal) ([]models.DestinationSummary, error) {
	if err := p.Require(models.TokenScopeDestinationsRead); err != nil {
		return nil, err
	}
	var (
		list []*models.NotificationDestination
		err  error
	)
	if p.RequireGlobalAdmin() == nil {
		list, err = db.ListNotificationDestinations(ctx)
	} else {
		list, err = db.ListUsableNotificationDestinations(ctx, p.User.ID)
	}
	if err != nil {
		return nil, err
	}
	summaries := make([]models.DestinationSummary, len(list))
	for i, d := range list {
		summaries[i] = d.Summary()
	}
	return summaries, nil
}

// CreateDestination creates a destination owned by the user.
func CreateDestination(ctx context.Context, db store.Store, user *models.User, req *models.DestinationRequest) (*models.NotificationDestination, error) {
	var created *models.NotificationDestination
	err := db.WithTx(ctx, func(tx store.StoreOps) error {
		d, err := buildDestination(ctx, tx, user, req, nil)
		if err != nil {
			return err
		}
		if err := checkScopeChange(ctx, tx, user, d, nil); err != nil {
			return err
		}
		if err := tx.CreateNotificationDestination(ctx, d); err != nil {
			return err
		}
		created = d
		return nil
	})
	return created, err
}

// UpdateDestination changes a destination the user may edit.
func UpdateDestination(ctx context.Context, db store.Store, user *models.User, id models.DestinationID, req *models.DestinationRequest) (before, after *models.NotificationDestination, err error) {
	err = db.WithTx(ctx, func(tx store.StoreOps) error {
		existing, err := loadEditableDestination(ctx, tx, user, id)
		if err != nil {
			return err
		}
		d, err := buildDestination(ctx, tx, user, req, existing)
		if err != nil {
			return err
		}
		if err := checkScopeChange(ctx, tx, user, d, existing); err != nil {
			return err
		}
		if err := tx.UpdateNotificationDestination(ctx, d); err != nil {
			return err
		}
		before, after = existing, d
		return nil
	})
	return before, after, err
}

// checkScopeChange applies the create rule to a new destination and to an
// update that moves it to another scope or team, and the per-team limit.
func checkScopeChange(ctx context.Context, db store.StoreOps, user *models.User, d, existing *models.NotificationDestination) error {
	moved := existing == nil || existing.Scope != d.Scope || !sameTeam(existing.TeamID, d.TeamID)
	if !moved {
		return nil
	}
	ok, err := canCreateInScope(ctx, db, user, d.Scope, d.TeamID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: you cannot create a %s destination here", ErrDestinationForbidden, d.Scope)
	}
	if d.TeamID != nil {
		n, err := db.CountTeamNotificationDestinations(ctx, *d.TeamID)
		if err != nil {
			return err
		}
		if n >= MaxTeamDestinations {
			return invalidDestination("a team has at most %d destinations", MaxTeamDestinations)
		}
	}
	return nil
}

func sameTeam(a, b *models.TeamID) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}

// DeleteDestination deletes a destination the user may edit and no rule uses.
func DeleteDestination(ctx context.Context, db store.Store, user *models.User, id models.DestinationID) (*models.NotificationDestination, error) {
	var deleted *models.NotificationDestination
	err := db.WithTx(ctx, func(tx store.StoreOps) error {
		d, err := loadEditableDestination(ctx, tx, user, id)
		if err != nil {
			return err
		}
		n, err := tx.CountAlertsUsingDestination(ctx, id)
		if err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("%w: %d alert rules use it", ErrDestinationInUse, n)
		}
		if err := tx.DeleteNotificationDestination(ctx, id); err != nil {
			if models.IsConflict(err) {
				return fmt.Errorf("%w: an alert rule uses it", ErrDestinationInUse)
			}
			return err
		}
		deleted = d
		return nil
	})
	return deleted, err
}

func loadEditableDestination(ctx context.Context, db store.StoreOps, user *models.User, id models.DestinationID) (*models.NotificationDestination, error) {
	d, err := db.GetNotificationDestination(ctx, id)
	if models.IsNotFound(err) {
		return nil, ErrDestinationNotFound
	}
	if err != nil {
		return nil, err
	}
	ok, err := canEditDestination(ctx, db, user, d)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: only its creator, a manager of its team or a global admin can change it", ErrDestinationForbidden)
	}
	return d, nil
}

// DestinationForTest returns the destination a test send goes to: a saved one
// the user can use, or an unsaved definition the user could create.
func DestinationForTest(ctx context.Context, db store.StoreOps, user *models.User, req *models.TestDestinationRequest) (*models.NotificationDestination, error) {
	if req.ID != nil {
		d, err := db.GetNotificationDestination(ctx, *req.ID)
		if models.IsNotFound(err) {
			return nil, ErrDestinationNotFound
		}
		if err != nil {
			return nil, err
		}
		ok, err := CanUseDestination(ctx, db, user, d)
		if err != nil {
			return nil, err
		}
		if !ok && user.Role != models.UserRoleAdmin {
			return nil, fmt.Errorf("%w: you cannot use this destination", ErrDestinationForbidden)
		}
		// The form tests a version change before it is saved.
		if version := req.Config.PayloadVersion; version != 0 {
			if d.Type != models.DestinationTypeWebhook || (version != notify.WebhookPayloadV1 && version != notify.WebhookPayloadV2) {
				return nil, invalidDestination("config.payload_version must be 1 or 2, for a webhook destination")
			}
			d.Config.PayloadVersion = version
		}
		return d, nil
	}
	d, err := buildDestination(ctx, db, user, &req.DestinationRequest, nil)
	if err != nil {
		return nil, err
	}
	ok, err := canCreateInScope(ctx, db, user, d.Scope, d.TeamID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: you cannot create a %s destination here", ErrDestinationForbidden, d.Scope)
	}
	return d, nil
}

// resolveAlertDestinations checks the destinations requested for a rule.
// Each must exist; a destination the rule does not have yet must be usable
// by the editor; one it already has stays even when the editor cannot use it.
func resolveAlertDestinations(ctx context.Context, db store.StoreOps, user *models.User, requested, current []models.DestinationID) ([]models.DestinationID, error) {
	ids := make([]models.DestinationID, 0, len(requested))
	for _, id := range requested {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) > MaxDestinationsPerRule {
		return nil, fmt.Errorf("%w: destination_ids: a rule has at most %d destinations", ErrInvalidAlertConfiguration, MaxDestinationsPerRule)
	}
	for _, id := range ids {
		d, err := db.GetNotificationDestination(ctx, id)
		if models.IsNotFound(err) {
			return nil, fmt.Errorf("%w: destination_ids: destination %d not found", ErrInvalidAlertConfiguration, id)
		}
		if err != nil {
			return nil, err
		}
		if slices.Contains(current, id) {
			continue
		}
		ok, err := CanUseDestination(ctx, db, user, d)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%w: you cannot use destination %d", ErrDestinationForbidden, id)
		}
	}
	slices.Sort(ids)
	return ids, nil
}
