package auth

import (
	"context"
	"log/slog"
	"strings"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// matchAllowedDomain reports whether email's domain part exactly matches one
// of allowedDomains (case-insensitive, no subdomain/wildcard matching). It
// returns the lowercased domain and true on a match.
func matchAllowedDomain(email string, allowedDomains []string) (string, bool) {
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		// No "@", or "@" is the last character: no domain part to match.
		return "", false
	}
	domain := strings.ToLower(email[at+1:])

	for _, allowed := range allowedDomains {
		if strings.EqualFold(domain, strings.TrimSpace(allowed)) {
			return domain, true
		}
	}
	return "", false
}

// autoProvisionUser attempts JIT auto-provisioning for a first-time OIDC
// login. It returns (nil, false, nil) when auto-provisioning is disabled or
// the email's domain isn't in the allowed list — callers should treat that
// identically to "user not found". A non-nil error means auto-provisioning
// was attempted (domain matched) but failed; the login must not proceed.
func autoProvisionUser(ctx context.Context, db store.StoreOps, log *slog.Logger, cfg config.AutoProvisionConfig, email, fullName string) (*models.User, bool, error) {
	if !cfg.Enabled {
		return nil, false, nil
	}

	domain, ok := matchAllowedDomain(email, cfg.AllowedDomains)
	if !ok {
		return nil, false, nil
	}

	user, err := core.GetOrCreateAutoProvisionedUser(ctx, db, log, email, fullName)
	if err != nil {
		return nil, true, err
	}

	// Best-effort default team memberships: a missing team (or any membership
	// failure) is logged and skipped, it never fails the login.
	var joinedTeamIDs []int
	for _, id := range cfg.DefaultTeamIDs {
		teamID := models.TeamID(id)
		if _, err := core.GetTeam(ctx, db, teamID); err != nil {
			log.Warn("auto_provision default_team_ids: team not found, skipping", "team_id", id, "email", email, "error", err)
			continue
		}
		if err := core.AddTeamMember(ctx, db, log, teamID, user.ID, models.TeamRoleMember); err != nil {
			log.Warn("auto_provision default_team_ids: failed to add team membership, skipping", "team_id", id, "email", email, "error", err)
			continue
		}
		joinedTeamIDs = append(joinedTeamIDs, id)
	}

	log.Info("user.auto_provisioned",
		"email", email,
		"domain", domain,
		"user_id", user.ID,
		"team_ids", joinedTeamIDs,
	)

	return user, true, nil
}
