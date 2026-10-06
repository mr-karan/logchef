// Package access is the authorization boundary for team, source and log
// operations. Every entrypoint (HTTP today, MCP later) builds a Principal for
// the caller and authorizes through this package, so the entrypoints cannot
// apply different rules.
package access

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// AuthMethod is how the caller authenticated. The zero value is not a valid
// method, so a Principal that nobody built on purpose is always denied.
type AuthMethod int

const (
	AuthSession AuthMethod = iota + 1
	AuthAPIToken
	AuthOAuth
)

var (
	// ErrInsufficientScope means the credential does not grant the scope, or
	// the authentication method is unknown.
	ErrInsufficientScope = errors.New("credential does not grant the required scope")
	// ErrNotTeamMember means the user is not a member of the team and is not
	// a global admin.
	ErrNotTeamMember = errors.New("team membership required")
	// ErrSourceNotInTeam means the team is not linked to the source.
	ErrSourceNotInTeam = errors.New("team does not have access to this source")
	// ErrGlobalAdminRequired means the user is not a global admin.
	ErrGlobalAdminRequired = errors.New("global admin required")
)

// Principal is an authenticated caller. User is the active, human-or-service
// account loaded on this request.
type Principal struct {
	User   *models.User
	Method AuthMethod
	// scopes is nil for sessions, which carry the user's full rights.
	scopes []models.TokenScope
}

// SessionPrincipal builds the principal for a browser session.
func SessionPrincipal(user *models.User) Principal {
	return Principal{User: user, Method: AuthSession}
}

// APITokenPrincipal builds the principal for a personal or service API token.
func APITokenPrincipal(user *models.User, token *models.APIToken) Principal {
	p := Principal{User: user, Method: AuthAPIToken}
	if token != nil {
		p.scopes = append([]models.TokenScope(nil), token.Scopes...)
	}
	return p
}

// Require returns ErrInsufficientScope unless the principal holds scope.
// Sessions hold every scope. API tokens also honor the "*" scope. OAuth grants
// never contain "*", so only an exact match counts. Any other method is denied.
func (p Principal) Require(scope models.TokenScope) error {
	switch p.Method {
	case AuthSession:
		return nil
	case AuthAPIToken:
		if slices.Contains(p.scopes, scope) || slices.Contains(p.scopes, models.TokenScopeAll) {
			return nil
		}
	case AuthOAuth:
		if slices.Contains(p.scopes, scope) {
			return nil
		}
	}
	return ErrInsufficientScope
}

// RequireGlobalAdmin returns ErrGlobalAdminRequired unless the user is a
// global admin. It does not check scope.
func (p Principal) RequireGlobalAdmin() error {
	if p.User == nil || p.User.Role != models.UserRoleAdmin {
		return ErrGlobalAdminRequired
	}
	return nil
}

// AuthorizedSource proves that a user passed AuthorizeTeamSource for one team
// and source. Only this package can build one, so a function that takes it
// cannot run for a caller that skipped authorization.
type AuthorizedSource struct {
	teamID   models.TeamID
	sourceID models.SourceID
	userID   models.UserID
}

func (a AuthorizedSource) TeamID() models.TeamID     { return a.teamID }
func (a AuthorizedSource) SourceID() models.SourceID { return a.sourceID }
func (a AuthorizedSource) UserID() models.UserID     { return a.userID }

// AuthorizeTeam checks team membership, then scope. Global admins pass the
// membership check. Every call reads the database; nothing is cached.
func AuthorizeTeam(ctx context.Context, db store.StoreOps, p Principal, teamID models.TeamID, scope models.TokenScope) error {
	if err := requireTeamMember(ctx, db, p, teamID); err != nil {
		return err
	}
	return p.Require(scope)
}

// AuthorizeTeamSource checks team membership, then the team-source link, then
// scope. This is the order the HTTP middleware has always applied, so the
// error a caller sees for a request with several faults does not change.
// Global admins pass the membership check but still need the link.
func AuthorizeTeamSource(ctx context.Context, db store.StoreOps, p Principal, teamID models.TeamID, sourceID models.SourceID, scope models.TokenScope) (AuthorizedSource, error) {
	if err := requireTeamMember(ctx, db, p, teamID); err != nil {
		return AuthorizedSource{}, err
	}
	linked, err := db.TeamHasSource(ctx, teamID, sourceID)
	if err != nil {
		return AuthorizedSource{}, fmt.Errorf("checking team source link: %w", err)
	}
	if !linked {
		return AuthorizedSource{}, ErrSourceNotInTeam
	}
	if err := p.Require(scope); err != nil {
		return AuthorizedSource{}, err
	}
	return AuthorizedSource{teamID: teamID, sourceID: sourceID, userID: p.User.ID}, nil
}

func requireTeamMember(ctx context.Context, db store.StoreOps, p Principal, teamID models.TeamID) error {
	if p.User == nil {
		return ErrNotTeamMember
	}
	if p.User.Role == models.UserRoleAdmin {
		return nil
	}
	member, err := db.GetTeamMember(ctx, teamID, p.User.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, models.ErrNotFound) {
			return ErrNotTeamMember
		}
		return fmt.Errorf("checking team membership: %w", err)
	}
	if member == nil {
		return ErrNotTeamMember
	}
	return nil
}
