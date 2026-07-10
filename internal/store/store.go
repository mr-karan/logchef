// Package store defines the backend-agnostic contract for logchef's application
// metadata — users, teams, sources, sessions, saved queries, collections,
// alerts, API tokens, system settings, export jobs, user preferences and query
// shares. (Log data lives in ClickHouse and is not part of this contract.)
//
// Concrete implementations live in sub-packages — store/sqlite (the default,
// single-binary backend) and store/postgres (for multi-replica deployments) —
// and are selected at startup from config. Implementations keep their query
// generator (sqlc) and driver types internal: every method here speaks in
// pkg/models types and primitives, and translates driver errors into the
// canonical backend-neutral sentinels in pkg/models (models.ErrNotFound,
// models.ErrConflict).
//
// Callers should depend on the narrowest interface they need (e.g. SessionStore)
// rather than the full Store, which is assembled here for wiring.
package store

import (
	"context"
	"io"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// TxRunner runs work inside a single transaction. The callback receives a
// transaction-scoped StoreOps whose reads and writes all share that transaction
// (so read-after-write within the callback is consistent); it commits when fn
// returns nil and rolls back on any error or panic (then re-panics).
//
// The callback gets StoreOps, not Store, on purpose: a tx-scoped handle must not
// be Close()'d or re-enter WithTx (nested transactions are unsupported). The
// handle is valid only for the duration of fn. Implementations must not expose
// the underlying driver transaction, and must not silently retry fn.
type TxRunner interface {
	WithTx(ctx context.Context, fn func(tx StoreOps) error) error
}

// SessionStore persists authentication sessions.
type SessionStore interface {
	CreateSession(ctx context.Context, session *models.Session) error
	GetSession(ctx context.Context, id models.SessionID) (*models.Session, error)
	DeleteSession(ctx context.Context, id models.SessionID) error
	DeleteUserSessions(ctx context.Context, userID models.UserID) error
	CountUserSessions(ctx context.Context, userID models.UserID) (int, error)
	// DeleteExpiredSessions removes all sessions whose expiry is at or before
	// the given time. Used by the periodic session sweeper.
	DeleteExpiredSessions(ctx context.Context, before time.Time) error
}

// UserPreferenceStore persists per-user UI preferences as an opaque JSON blob.
type UserPreferenceStore interface {
	GetUserPreferencesJSON(ctx context.Context, userID models.UserID) (string, error)
	UpsertUserPreferencesJSON(ctx context.Context, userID models.UserID, preferencesJSON string) error
}

// UserStore persists user accounts (interactive and service accounts).
type UserStore interface {
	CreateUser(ctx context.Context, user *models.User) error
	GetUser(ctx context.Context, id models.UserID) (*models.User, error)
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	UpdateUser(ctx context.Context, user *models.User) error
	ListUsers(ctx context.Context) ([]*models.User, error)
	ListServiceAccounts(ctx context.Context) ([]*models.User, error)
	CountAdminUsers(ctx context.Context) (int, error)
	// SetUserPasswordHash stores (or clears, with "") the bcrypt hash used by
	// local email+password authentication.
	SetUserPasswordHash(ctx context.Context, id models.UserID, hash string) error
	DeleteUser(ctx context.Context, id models.UserID) error
}

// SourceStore persists log-source connection metadata (ClickHouse coordinates,
// TLS, schema). The log data itself lives in ClickHouse, not here.
type SourceStore interface {
	CreateSource(ctx context.Context, source *models.Source) error
	GetSource(ctx context.Context, id models.SourceID) (*models.Source, error)
	GetSourceByIdentityKey(ctx context.Context, identityKey string) (*models.Source, error)
	ListSources(ctx context.Context) ([]*models.Source, error)
	UpdateSource(ctx context.Context, source *models.Source) error
	DeleteSource(ctx context.Context, id models.SourceID) error
}

// SavedQueryStore persists named, reusable queries. Visibility/edit rules are
// enforced in core, not here — these methods are the raw persistence surface.
type SavedQueryStore interface {
	CreateSavedQuery(ctx context.Context, sourceID models.SourceID, createdFromTeamID *models.TeamID, name, description string, queryLanguage models.QueryLanguage, editorMode models.SavedQueryEditorMode, queryContent string, createdBy *models.UserID) (*models.SavedQuery, error)
	GetSavedQuery(ctx context.Context, queryID int) (*models.SavedQuery, error)
	UpdateSavedQuery(ctx context.Context, queryID int, name, description string, queryLanguage models.QueryLanguage, editorMode models.SavedQueryEditorMode, queryContent string) error
	DeleteSavedQuery(ctx context.Context, queryID int) error
	ListSavedQueriesForUser(ctx context.Context, userID models.UserID) ([]*models.SavedQuery, error)
	ListSavedQueriesForUserBySource(ctx context.Context, userID models.UserID, sourceID models.SourceID) ([]*models.SavedQuery, error)
	ListAllSavedQueries(ctx context.Context) ([]*models.SavedQuery, error)
}

// CollectionStore persists collections (curated groups of saved queries) plus
// their membership and items.
type CollectionStore interface {
	CreateCollection(ctx context.Context, name, description string, isPersonal bool, createdBy models.UserID) (*models.Collection, error)
	GetCollection(ctx context.Context, collectionID int) (*models.Collection, error)
	GetPersonalCollection(ctx context.Context, userID models.UserID) (*models.Collection, error)
	UpdateCollection(ctx context.Context, collectionID int, name, description string) error
	DeleteCollection(ctx context.Context, collectionID int) error
	ListCollectionsForUser(ctx context.Context, userID models.UserID) ([]*models.Collection, error)
	AddCollectionMember(ctx context.Context, collectionID int, userID models.UserID, role models.CollectionRole, addedBy *models.UserID) error
	GetCollectionMember(ctx context.Context, collectionID int, userID models.UserID) (*models.CollectionMember, error)
	ListCollectionMembers(ctx context.Context, collectionID int) ([]*models.CollectionMember, error)
	RemoveCollectionMember(ctx context.Context, collectionID int, userID models.UserID) error
	AddCollectionItem(ctx context.Context, collectionID, savedQueryID, sortOrder int, addedBy *models.UserID) error
	RemoveCollectionItem(ctx context.Context, collectionID, savedQueryID int) error
	ListCollectionItems(ctx context.Context, collectionID int) ([]*models.CollectionItem, error)
	UserCanEditSavedQueryViaSharedCollection(ctx context.Context, userID models.UserID, queryID int) (bool, error)
}

// DashboardStore persists dashboards (a saved grid of visualization panels).
// The panel blob is validated in models/core, not here — these methods are the
// raw persistence surface. Reads/mutations on a missing id return
// models.ErrNotFound.
type DashboardStore interface {
	CreateDashboard(ctx context.Context, dashboard *models.Dashboard) error
	GetDashboard(ctx context.Context, id int) (*models.Dashboard, error)
	ListDashboards(ctx context.Context) ([]*models.Dashboard, error)
	UpdateDashboard(ctx context.Context, dashboard *models.Dashboard) error
	DeleteDashboard(ctx context.Context, id int) error
}

// AlertStore persists alert definitions and their evaluation history.
type AlertStore interface {
	CreateAlert(ctx context.Context, alert *models.Alert) error
	UpdateAlert(ctx context.Context, alert *models.Alert) error
	DeleteAlert(ctx context.Context, alertID models.AlertID) error
	GetAlert(ctx context.Context, alertID models.AlertID) (*models.Alert, error)
	ListAlertsBySource(ctx context.Context, sourceID models.SourceID) ([]*models.Alert, error)
	ListAlertsForUser(ctx context.Context, userID models.UserID) ([]*models.Alert, error)
	ListActiveAlertsDue(ctx context.Context) ([]*models.Alert, error)
	MarkAlertEvaluated(ctx context.Context, alertID models.AlertID) error
	MarkAlertTriggered(ctx context.Context, alertID models.AlertID) error
	InsertAlertHistory(ctx context.Context, alertID models.AlertID, status models.AlertStatus, value *float64, message string, payload map[string]any) (*models.AlertHistoryEntry, error)
	GetLatestUnresolvedAlertHistory(ctx context.Context, alertID models.AlertID) (*models.AlertHistoryEntry, error)
	ResolveAlertHistory(ctx context.Context, historyID int64, message string) error
	UpdateAlertHistoryPayload(ctx context.Context, historyID int64, payload map[string]any) error
	ListAlertHistory(ctx context.Context, alertID models.AlertID, limit int) ([]*models.AlertHistoryEntry, error)
	PruneAlertHistory(ctx context.Context, alertID models.AlertID, keep int) error
}

// ExportJobStore persists asynchronous CSV/export job records.
type ExportJobStore interface {
	CreateExportJob(ctx context.Context, job *models.ExportJob) error
	GetExportJob(ctx context.Context, id string) (*models.ExportJob, error)
	UpdateExportJobRunning(ctx context.Context, id string, updatedAt time.Time) error
	CompleteExportJob(ctx context.Context, id, fileName, filePath string, rowsExported int, bytesWritten int64, completedAt time.Time) error
	FailExportJob(ctx context.Context, id, errorMessage string, updatedAt time.Time) error
	ListExpiredExportJobPaths(ctx context.Context, before time.Time) ([]string, error)
	DeleteExpiredExportJobs(ctx context.Context, before time.Time) error
}

// QueryShareStore persists shareable query links and resolves the team a user
// should run a shared query under.
type QueryShareStore interface {
	CreateQueryShare(ctx context.Context, share *models.QueryShare) error
	GetQueryShare(ctx context.Context, token string) (*models.QueryShare, error)
	TouchQueryShare(ctx context.Context, token string, accessedAt time.Time) error
	DeleteQueryShare(ctx context.Context, token string) error
	GetUserTeamForSource(ctx context.Context, userID models.UserID, sourceID models.SourceID) (models.TeamID, error)
	PruneExpiredQueryShares(ctx context.Context, before time.Time) error
}

// ProvisioningStore backs declarative provisioning: it answers whether an
// entity is managed (and therefore must not be mutated through the API) and
// provides the managed-entity reads/marks the reconciler needs.
type ProvisioningStore interface {
	IsSourceManaged(ctx context.Context, id models.SourceID) (bool, error)
	IsTeamManaged(ctx context.Context, id models.TeamID) (bool, error)
	IsUserManaged(ctx context.Context, id models.UserID) (bool, error)

	ListManagedSources(ctx context.Context) ([]*models.Source, error)
	ListManagedTeams(ctx context.Context) ([]*models.Team, error)
	// GetSourceByNameForProvisioning looks a source up by name (managed or not),
	// returning models.ErrNotFound when absent.
	GetSourceByNameForProvisioning(ctx context.Context, name string) (*models.Source, error)
	SetSourceManaged(ctx context.Context, id models.SourceID, managed bool, secretRef string) error
	SetTeamManaged(ctx context.Context, id models.TeamID, managed bool) error
	SetUserManaged(ctx context.Context, id models.UserID, managed bool) error
}

// TeamStore persists teams, their membership, and the team↔source links that
// gate which sources a user can reach.
type TeamStore interface {
	CreateTeam(ctx context.Context, team *models.Team) error
	GetTeam(ctx context.Context, teamID models.TeamID) (*models.Team, error)
	GetTeamByName(ctx context.Context, name string) (*models.Team, error)
	UpdateTeam(ctx context.Context, team *models.Team) error
	DeleteTeam(ctx context.Context, teamID models.TeamID) error
	ListTeams(ctx context.Context) ([]*models.Team, error)
	ListUserTeams(ctx context.Context, userID models.UserID) ([]*models.Team, error)
	ListTeamsForUser(ctx context.Context, userID models.UserID) ([]*models.UserTeamDetails, error)

	AddTeamMember(ctx context.Context, teamID models.TeamID, userID models.UserID, role models.TeamRole) error
	GetTeamMember(ctx context.Context, teamID models.TeamID, userID models.UserID) (*models.TeamMember, error)
	UpdateTeamMemberRole(ctx context.Context, teamID models.TeamID, userID models.UserID, role models.TeamRole) error
	RemoveTeamMember(ctx context.Context, teamID models.TeamID, userID models.UserID) error
	ListTeamMembers(ctx context.Context, teamID models.TeamID) ([]*models.TeamMember, error)
	ListTeamMembersWithDetails(ctx context.Context, teamID models.TeamID) ([]*models.TeamMember, error)

	AddTeamSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) error
	RemoveTeamSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) error
	ListTeamSources(ctx context.Context, teamID models.TeamID) ([]*models.Source, error)
	ListSourceTeams(ctx context.Context, sourceID models.SourceID) ([]*models.Team, error)
	ListSourcesForUser(ctx context.Context, userID models.UserID) ([]*models.Source, error)
	TeamHasSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) (bool, error)
	UserHasSourceAccess(ctx context.Context, userID models.UserID, sourceID models.SourceID) (bool, error)
}

// SettingsStore persists system settings. The typed getters return a default
// when a key is absent or unparseable; the list/upsert/delete methods manage
// the raw entries.
type SettingsStore interface {
	GetSetting(ctx context.Context, key string) (string, error)
	GetSettingWithDefault(ctx context.Context, key, defaultValue string) string
	GetBoolSetting(ctx context.Context, key string, defaultValue bool) bool
	GetIntSetting(ctx context.Context, key string, defaultValue int) int
	GetFloat64Setting(ctx context.Context, key string, defaultValue float64) float64
	GetDurationSetting(ctx context.Context, key string, defaultValue time.Duration) time.Duration
	ListSettings(ctx context.Context) ([]*models.SystemSetting, error)
	ListSettingsByCategory(ctx context.Context, category string) ([]*models.SystemSetting, error)
	UpsertSetting(ctx context.Context, key, value, valueType, category, description string, isSensitive bool) error
	DeleteSetting(ctx context.Context, key string) error
}

// TokenStore persists API tokens. Scope serialization and expiry computation
// live in the implementation; callers work in models.APIToken throughout.
type TokenStore interface {
	CreateAPIToken(ctx context.Context, token *models.APIToken) (int, error)
	GetAPIToken(ctx context.Context, id int) (*models.APIToken, error)
	GetAPITokenByHash(ctx context.Context, tokenHash string) (*models.APIToken, error)
	ListAPITokensForUser(ctx context.Context, userID models.UserID) ([]*models.APIToken, error)
	UpdateAPITokenLastUsed(ctx context.Context, id int) error
	DeleteAPIToken(ctx context.Context, id int, userID models.UserID) error
}

// StoreOps is the full set of data operations across every domain, with no
// lifecycle (Close) or transaction control (WithTx). It is what a WithTx
// callback receives, and what consumers should accept when they don't manage
// the connection lifecycle themselves.
//
// It composes one interface per domain; together they cover all 14 metadata
// domains. Every method speaks pkg/models types — no sqlc or driver types leak
// through.
type StoreOps interface {
	SessionStore
	UserPreferenceStore
	UserStore
	SourceStore
	SavedQueryStore
	CollectionStore
	DashboardStore
	AlertStore
	ExportJobStore
	QueryShareStore
	ProvisioningStore
	TeamStore
	SettingsStore
	TokenStore
}

// Store is the complete metadata contract a backend (store/sqlite,
// store/postgres) implements: all data operations, plus lifecycle and
// transactions. Consumers should accept the narrowest interface they need —
// a single domain (e.g. SessionStore), StoreOps, or full Store.
type Store interface {
	io.Closer
	TxRunner
	StoreOps
}
