// Package store defines the backend-agnostic contract for logchef's application
// metadata — users, teams, sources, sessions, saved queries, collections,
// saved views, alerts, background jobs, scheduled reports, API tokens, system settings, export jobs, user preferences, query
// shares, OAuth authorization state and audit events. (Log data lives in ClickHouse and is not part of this contract.)
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
	// GetUserForUpdate reads a user and, inside WithTx, blocks other full-row
	// writers of that user until the transaction ends.
	GetUserForUpdate(ctx context.Context, id models.UserID) (*models.User, error)
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	UpdateUser(ctx context.Context, user *models.User) error
	// UpdateUserLastLogin sets only last_login_at, so a login never rewrites
	// a concurrent role or status change.
	UpdateUserLastLogin(ctx context.Context, id models.UserID, at time.Time) error
	ListUsers(ctx context.Context) ([]*models.User, error)
	ListServiceAccounts(ctx context.Context) ([]*models.User, error)
	CountAdminUsers(ctx context.Context) (int, error)
	// LockMembershipWrites serializes provisioning, cascading deletes, and admin removals until WithTx ends.
	LockMembershipWrites(ctx context.Context) error
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

// SavedViewStore persists saved Explorer views (RFC 0007). Scope, visibility
// and edit rules live in core; these methods are the raw persistence surface.
// A name is unique, and at most one view is the default, per scope, owner and
// source; a write that breaks either returns models.ErrConflict.
type SavedViewStore interface {
	// CreateSavedView inserts view with its IsDefault and Managed flags as
	// given and fills ID, Revision and the timestamps.
	CreateSavedView(ctx context.Context, view *models.SavedView) error
	// GetSavedView returns models.ErrNotFound when no view has the id.
	GetSavedView(ctx context.Context, id models.SavedViewID) (*models.SavedView, error)
	// ListSavedViews returns, in one query, userID's personal views, teamID's
	// team views and the source views of sourceID: personal first, then team,
	// then source, each ordered by name (bytewise) and id.
	ListSavedViews(ctx context.Context, sourceID models.SourceID, userID models.UserID, teamID models.TeamID) ([]*models.SavedView, error)
	// UpdateSavedView writes name, description and definition with a
	// compare-and-swap on view.Revision, the revision the caller loaded. On
	// success view holds the stored row with the next revision. A stale
	// revision changes no row and returns models.ErrConflict; a missing row
	// returns models.ErrNotFound. Scope, owner and flags never change here.
	UpdateSavedView(ctx context.Context, view *models.SavedView) error
	// DeleteSavedView returns models.ErrNotFound when no view has the id.
	DeleteSavedView(ctx context.Context, id models.SavedViewID) error
	// SetSavedViewDefault clears the default of the view's scope, owner and
	// source and sets it on the view, in one transaction (inside WithTx it
	// joins the caller's). It returns the view that lost the flag, or nil. A
	// caller that must not clear a managed default calls it inside WithTx and
	// returns an error to roll back. A concurrent swap that committed another
	// default first returns models.ErrConflict.
	SetSavedViewDefault(ctx context.Context, id models.SavedViewID) (*models.ClearedSavedViewDefault, error)
	// ClearSavedViewDefault clears the view's default flag. It returns
	// models.ErrNotFound when no view has the id.
	ClearSavedViewDefault(ctx context.Context, id models.SavedViewID) error
	// CountSavedViewsInScope counts the views of one scope of one source.
	CountSavedViewsInScope(ctx context.Context, key models.SavedViewScopeKey) (int, error)
	// ListManagedSavedViews returns every view that provisioning owns,
	// ordered by source, name (bytewise) and id.
	ListManagedSavedViews(ctx context.Context) ([]*models.SavedView, error)
}

// CollectionStore persists collections (curated groups of saved queries) plus
// their direct membership, team shares, and items.
type CollectionStore interface {
	CreateCollection(ctx context.Context, name, description string, isPersonal bool, createdBy models.UserID) (*models.Collection, error)
	GetCollection(ctx context.Context, collectionID int) (*models.Collection, error)
	GetPersonalCollection(ctx context.Context, userID models.UserID) (*models.Collection, error)
	UpdateCollection(ctx context.Context, collectionID int, name, description string) error
	DeleteCollection(ctx context.Context, collectionID int) error
	ListCollectionsForUser(ctx context.Context, userID models.UserID) ([]*models.Collection, error)
	AddCollectionMember(ctx context.Context, collectionID int, userID models.UserID, role models.CollectionRole, addedBy *models.UserID) error
	GetCollectionMember(ctx context.Context, collectionID int, userID models.UserID) (*models.CollectionMember, error)
	ListCollectionEditors(ctx context.Context, collectionIDs []int) ([]*models.CollectionMember, error)
	ListCollectionMembers(ctx context.Context, collectionID int) ([]*models.CollectionMember, error)
	RemoveCollectionMember(ctx context.Context, collectionID int, userID models.UserID) error
	AddCollectionTeam(ctx context.Context, collectionID int, teamID models.TeamID, addedBy *models.UserID) error
	RemoveCollectionTeam(ctx context.Context, collectionID int, teamID models.TeamID) error
	ListCollectionTeams(ctx context.Context, collectionID int) ([]*models.CollectionTeam, error)
	// UserHasCollectionTeamAccess reports whether the user currently belongs to
	// any team the collection is shared with.
	UserHasCollectionTeamAccess(ctx context.Context, collectionID int, userID models.UserID) (bool, error)
	// LockCollection holds the collection row until WithTx ends, so that the
	// authorization, the mutation and the response selection of a link,
	// member or share write see one state. It returns models.ErrNotFound when
	// the collection does not exist. Outside WithTx it only checks existence.
	LockCollection(ctx context.Context, collectionID int) error
	// AddCollectionItem inserts a link and reports whether a row was inserted.
	// An existing link of any kind is left as it is.
	AddCollectionItem(ctx context.Context, collectionID, savedQueryID, sortOrder int, kind models.CollectionLinkKind, addedBy *models.UserID) (bool, error)
	// GetCollectionItemLink returns models.ErrNotFound when no link exists.
	GetCollectionItemLink(ctx context.Context, collectionID, savedQueryID int) (*models.CollectionItemLink, error)
	// SetCollectionItemKind converts a link and reports whether a row changed.
	// A link already of that kind, or no link, changes nothing.
	SetCollectionItemKind(ctx context.Context, collectionID, savedQueryID int, kind models.CollectionLinkKind) (bool, error)
	// RemoveCollectionItem deletes a link of either kind and reports whether
	// a row was deleted.
	RemoveCollectionItem(ctx context.Context, collectionID, savedQueryID int) (bool, error)
	// RemoveCollectionPin deletes a link only when it is a pin and reports
	// whether a row was deleted. A managed link is left as it is.
	RemoveCollectionPin(ctx context.Context, collectionID, savedQueryID int) (bool, error)
	ListCollectionItems(ctx context.Context, collectionID int) ([]*models.CollectionItem, error)
	// ListSharedCollectionPins lists every pin in a shared collection, oldest
	// first, without the collection's editors.
	ListSharedCollectionPins(ctx context.Context) ([]*models.CollectionLinkReportRow, error)
	// UserCanEditSavedQueryViaSharedCollection reports whether the user owns
	// or edits a shared collection that manages the query.
	UserCanEditSavedQueryViaSharedCollection(ctx context.Context, userID models.UserID, queryID int) (bool, error)
}

// DashboardStore persists dashboards (a saved grid of visualization panels).
// Every write of the panel blob derives the list columns from it with
// models.SummarizeDashboardPanels and writes them in the same statement.
type DashboardStore interface {
	CreateDashboard(ctx context.Context, dashboard *models.Dashboard) error
	GetDashboard(ctx context.Context, id int) (*models.Dashboard, error)
	// ListDashboardSummaries returns every dashboard's summary from the derived
	// columns, newest-updated first. It never reads panels_json.
	ListDashboardSummaries(ctx context.Context) ([]*models.DashboardSummary, error)
	// ListStaleDashboardSummaryInputs returns up to limit rows with id above
	// afterID whose derived columns are stale, in id order, with their blob.
	ListStaleDashboardSummaryInputs(ctx context.Context, afterID, limit int) ([]models.DashboardSummaryInput, error)
	// RepairDashboardSummary writes summary for the row only if it still holds
	// expectedRevision and is still stale. It returns the affected-row count
	// and changes neither revision nor updated_at.
	RepairDashboardSummary(ctx context.Context, id, expectedRevision int, summary models.DashboardPanelsSummary) (int64, error)
	// UpdateDashboard writes name, description, panels and the derived columns
	// with a compare-and-swap on dashboard.Revision, the revision the caller
	// loaded.
	// On success dashboard.Revision is the new revision. When the stored
	// revision differs it returns models.ErrConflict, and when the row does
	// not exist models.ErrNotFound.
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
	MarkAlertEvaluated(ctx context.Context, alertID models.AlertID) error
	TouchAlertEvaluated(ctx context.Context, alertID models.AlertID) error
	MarkAlertTriggered(ctx context.Context, alertID models.AlertID) error
	// InsertAlertHistory records an entry for one group; groupKey is "" and
	// group nil for an ungrouped rule and for evaluation errors.
	InsertAlertHistory(ctx context.Context, alertID models.AlertID, groupKey string, group map[string]string, status models.AlertStatus, value *float64, message string, payload map[string]any) (*models.AlertHistoryEntry, error)
	GetLatestUnresolvedAlertHistory(ctx context.Context, alertID models.AlertID) (*models.AlertHistoryEntry, error)
	// ListOpenAlertHistory returns every open episode of the alert, newest first.
	ListOpenAlertHistory(ctx context.Context, alertID models.AlertID) ([]*models.AlertHistoryEntry, error)
	// GetAlertHistoryEntry returns models.ErrNotFound when the alert has no
	// entry with that ID.
	GetAlertHistoryEntry(ctx context.Context, alertID models.AlertID, historyID int64) (*models.AlertHistoryEntry, error)
	RecoverAlertDeliveries(ctx context.Context) error
	// ResolveAlertHistory resolves every unresolved firing entry of the alert
	// that owns historyID, deletes the alert's instances and advances its
	// eval_seq, so an evaluation that read the old state commits nothing.
	ResolveAlertHistory(ctx context.Context, historyID int64, message string) error
	// ResolveAlertGroup resolves the open episode of one group, deletes the
	// group's instance, advances eval_seq and sets last_state from the
	// instances that remain. It returns models.ErrNotFound when the group has
	// no open episode.
	ResolveAlertGroup(ctx context.Context, alertID models.AlertID, groupKey, message string) error
	UpdateAlertHistoryPayload(ctx context.Context, historyID int64, payload map[string]any) error
	ListAlertHistory(ctx context.Context, alertID models.AlertID, limit int) ([]*models.AlertHistoryEntry, error)
	// PruneAlertHistory keeps every open episode and the newest keep closed entries.
	PruneAlertHistory(ctx context.Context, alertID models.AlertID, keep int) error
	// AdvanceAlertEvalSeq increments eval_seq (models.Alert.EvalSeq) when it
	// still equals seq, and returns models.ErrConflict when another writer
	// advanced it first. UpdateAlert and ResolveAlertHistory advance it too.
	AdvanceAlertEvalSeq(ctx context.Context, alertID models.AlertID, seq int64) error
	ListAlertInstances(ctx context.Context, alertID models.AlertID) ([]*models.AlertInstance, error)
	// CountAlertInstances returns the instance counts of each given alert
	// that has instances.
	CountAlertInstances(ctx context.Context, alertIDs []models.AlertID) (map[models.AlertID]models.AlertInstanceCounts, error)
	UpsertAlertInstance(ctx context.Context, instance *models.AlertInstance) error
	// MarkAlertInstanceNotified sets notified on the instance whose open
	// episode is historyID, and returns models.ErrNotFound when there is none.
	MarkAlertInstanceNotified(ctx context.Context, alertID models.AlertID, groupKey string, historyID int64) error
}

// JobStore persists the background jobs that internal/jobs schedules and
// leases. Every time comparison uses the database clock, so clock skew
// between replicas does not matter. The methods that take a lease token
// change a job only while that token is current, that is, while no later
// claim exists; they report whether a row changed.
type JobStore interface {
	// UpsertJob creates the job due now, or updates its definition. An
	// update that changes the schedule makes the job due now; any other
	// update keeps its next run and its lease.
	UpsertJob(ctx context.Context, def models.JobDefinition) error
	// DeleteJob removes the job. A missing job is not an error.
	DeleteJob(ctx context.Context, kind, key string) error
	// GetJob returns models.ErrNotFound when the job does not exist.
	GetJob(ctx context.Context, kind, key string) (*models.ScheduledJob, error)
	// KickJob makes the job due now. It returns models.ErrNotFound when the
	// job does not exist.
	KickJob(ctx context.Context, kind, key string) error
	// ClaimJobs leases to owner up to limit enabled jobs of kind that are
	// due and not leased, oldest due first. Each claim raises the job's lease
	// token and sets LastStartedAt to the claim time. On Postgres concurrent
	// claims receive disjoint jobs and skip a job whose row a transaction
	// holds through HoldJob.
	ClaimJobs(ctx context.Context, kind, owner string, lease time.Duration, limit int) ([]*models.ScheduledJob, error)
	// HoldJob reports whether token is current. Inside WithTx a true result
	// keeps the job row locked until the transaction ends, so no claim can
	// raise the token before the transaction commits.
	HoldJob(ctx context.Context, id, token int64) (bool, error)
	// FinishJob records a run's outcome and clears the lease. It sets the
	// next run only while the job's schedule equals the one in res.
	FinishJob(ctx context.Context, id, token int64, res models.JobRunResult) (bool, error)
	// ReleaseJob clears the lease and keeps the next run and the last outcome.
	ReleaseJob(ctx context.Context, id, token int64) (bool, error)
	// DeleteClaimedJob deletes the job.
	DeleteClaimedJob(ctx context.Context, id, token int64) (bool, error)
}

// QueryHistoryStore persists per-user query execution history. Recording is
// best-effort (callers fire-and-forget on the query path) and self-pruning:
// RecordQueryHistory caps each user's history at models.QueryHistoryPerUserCap.
type QueryHistoryStore interface {
	RecordQueryHistory(ctx context.Context, entry *models.QueryHistory) error
	ListQueryHistory(ctx context.Context, userID models.UserID, limit int) ([]*models.QueryHistory, error)
	// ListQueryActivity returns the most recent query_history rows across all
	// users (newest first, capped at limit), enriched with user email and
	// source name. It backs the admin recent-activity view; because
	// query_history is capped per user, this is a recent window, not all-time.
	ListQueryActivity(ctx context.Context, limit int) ([]models.QueryActivityRecord, error)

	// IncrementQueryStats upserts one executed query into the non-pruned
	// query_stats_daily rollup: it adds 1 to query_count and durationMs to
	// total_duration_ms for the (bucketDate,userID,teamID,sourceID,language)
	// key. bucketDate is 'YYYY-MM-DD' (UTC). Called at record time (best-effort)
	// so all-time usage analytics stay correct despite query_history pruning.
	IncrementQueryStats(ctx context.Context, bucketDate string, userID models.UserID, teamID models.TeamID, sourceID models.SourceID, language models.QueryLanguage, durationMs int64) error
	// TopSourcesByQueries returns sources ordered by total query count desc
	// (capped at limit) over rollup rows with bucket_date >= since. source_name
	// is "" when the source row is gone (LEFT JOIN); avg_duration_ms is
	// total_duration_ms/query_count (integer, 0 when count 0).
	TopSourcesByQueries(ctx context.Context, since string, limit int) ([]models.SourceQueryStat, error)
	// TopUsersByQueries returns users ordered by total query count desc (capped
	// at limit) over rollup rows with bucket_date >= since.
	TopUsersByQueries(ctx context.Context, since string, limit int) ([]models.UserQueryStat, error)
	// QueryVolumeByDay returns per-day total query counts (ascending by date)
	// over rollup rows with bucket_date >= since.
	QueryVolumeByDay(ctx context.Context, since string) ([]models.DailyQueryVolume, error)
}

// ReportStore persists scheduled reports (RFC 0008), their recipients and
// their runs. Every time it compares or writes for a claim or a cooldown is
// the database clock, so replicas agree.
type ReportStore interface {
	// CreateReport inserts an active report and fills r from the stored row.
	CreateReport(ctx context.Context, r *models.Report) error
	// GetReport returns models.ErrNotFound when the report does not exist.
	GetReport(ctx context.Context, id models.ReportID) (*models.Report, error)
	// ListReports returns every report, by name.
	ListReports(ctx context.Context) ([]*models.Report, error)
	ListReportsByOwner(ctx context.Context, owner models.UserID) ([]*models.Report, error)
	// ListReportsForRecipient returns the reports that name the user directly,
	// a team the user belongs to, or the user's address ignoring case.
	ListReportsForRecipient(ctx context.Context, userID models.UserID) ([]*models.Report, error)
	// UpdateReport writes the fields a user edits: name, source, schedule,
	// timezone, range, variables and options. It leaves the owner, the state
	// and the counters alone, and fills r from the stored row. A missing
	// report returns models.ErrNotFound.
	UpdateReport(ctx context.Context, r *models.Report) error
	// DeleteReport returns models.ErrNotFound when the report does not exist.
	DeleteReport(ctx context.Context, id models.ReportID) error
	// PauseReport sets the report paused with a non-empty reason.
	PauseReport(ctx context.Context, id models.ReportID, reason models.ReportPauseReason) error
	// ResumeReport makes the report active and resets its failure streak.
	ResumeReport(ctx context.Context, id models.ReportID) error
	// ClaimReport makes owner the owner and lifts a pause for an inactive
	// owner. Who may claim is decided by the caller.
	ClaimReport(ctx context.Context, id models.ReportID, owner models.UserID) error
	// TakeReportCooldown takes the manual-run or preview cooldown with one
	// conditional update and reports whether it did. False means the last
	// one was less than models.ReportCooldown ago, or the report is gone.
	TakeReportCooldown(ctx context.Context, id models.ReportID, kind models.ReportCooldownKind) (bool, error)

	ListReportRecipients(ctx context.Context, id models.ReportID) ([]models.ReportRecipient, error)
	// ReplaceReportRecipients replaces every recipient row of the report in
	// one transaction that holds the report, so concurrent replaces never
	// merge. A duplicate target returns models.ErrConflict and a missing
	// report models.ErrNotFound.
	ReplaceReportRecipients(ctx context.Context, id models.ReportID, recipients []models.ReportRecipient) error
	// RemoveReportUserRecipient deletes the direct user row of userID and
	// returns models.ErrNotFound when there is none.
	RemoveReportUserRecipient(ctx context.Context, id models.ReportID, userID models.UserID) error

	// CreateReportRun inserts a running run as the claim. A scheduled run
	// whose tick already has a row returns models.ErrReportTickTaken; any
	// other run while one is running returns models.ErrReportRunInFlight.
	CreateReportRun(ctx context.Context, claim models.ReportRunClaim) (*models.ReportRun, error)
	// SkipReportTick records a scheduled tick as skipped without running it.
	// A tick that already has a row returns models.ErrReportTickTaken.
	SkipReportTick(ctx context.Context, id models.ReportID, scheduledFor time.Time, reason models.ReportRunReason) (*models.ReportRun, error)
	// FinishReportRun writes a running run's result, updates the report's
	// failure streak, keeps the newest models.ReportRunHistoryLimit runs and
	// returns the streak. A run that is not running returns
	// models.ErrNotFound.
	FinishReportRun(ctx context.Context, runID models.ReportRunID, res models.ReportRunResult) (int, error)
	// ListReportRuns returns up to limit runs, newest first, with ids below
	// before; before 0 starts at the newest.
	ListReportRuns(ctx context.Context, id models.ReportID, before models.ReportRunID, limit int) ([]*models.ReportRun, error)
	// InterruptStaleReportRuns marks running runs whose deadline passed more
	// than grace ago interrupted, and returns how many.
	InterruptStaleReportRuns(ctx context.Context, grace time.Duration) (int64, error)
}

// ExportJobStore persists asynchronous CSV/export job records.
type ExportJobStore interface {
	ReapInterruptedExportJobs(ctx context.Context, maxAge time.Duration, owner models.ExportJobOwner) ([]models.ReapedExportJob, error)
	CreateExportJob(ctx context.Context, job *models.ExportJob) error
	GetExportJob(ctx context.Context, id string) (*models.ExportJob, error)
	UpdateExportJobRunning(ctx context.Context, id string) error
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

	ListManagedSources(ctx context.Context) ([]*models.Source, error)
	ListManagedTeams(ctx context.Context) ([]*models.Team, error)
	// ListUsersForProvisioning loads the users referenced by provisioning.
	ListUsersForProvisioning(ctx context.Context, emails []string) ([]*models.User, error)
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
	ListTeamMembersWithDetails(ctx context.Context, teamID models.TeamID) ([]*models.TeamMember, error)

	AddTeamSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) error
	RemoveTeamSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) error
	ListTeamSources(ctx context.Context, teamID models.TeamID) ([]*models.Source, error)
	ListSourceTeams(ctx context.Context, sourceID models.SourceID) ([]*models.Team, error)
	ListSourcesForUser(ctx context.Context, userID models.UserID) ([]*models.Source, error)
	TeamHasSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) (bool, error)
	UserHasSourceAccess(ctx context.Context, userID models.UserID, sourceID models.SourceID) (bool, error)
}

// SettingsStore persists system settings.
type SettingsStore interface {
	GetSetting(ctx context.Context, key string) (string, error)
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

// OAuthStore persists Logchef's OAuth authorization server state: pending
// authorization requests, grants and token hashes.
// Every code and token argument is a models.OAuthSecretHash; plaintext never
// reaches storage. now is passed in so every expiry check uses the caller's
// clock.
//
// ConsumeAuthCode, CheckRefreshToken and RotateRefreshToken must be called on
// the root store, not inside WithTx: a replay revokes the grant, and that
// revocation must commit even though the caller then fails the request.
type OAuthStore interface {
	CreateOAuthAuthRequest(ctx context.Context, req *models.OAuthAuthRequest) error
	GetOAuthAuthRequest(ctx context.Context, id models.OAuthAuthRequestID) (*models.OAuthAuthRequest, error)
	// ApproveOAuthAuthRequest records the first decision as an approval by
	// userID and creates the grant. It returns models.ErrNotFound when the
	// request is unknown, expired or already decided.
	ApproveOAuthAuthRequest(ctx context.Context, id models.OAuthAuthRequestID, userID models.UserID, now time.Time) (*models.OAuthGrant, error)
	// DenyOAuthAuthRequest records the first decision as a denial by userID.
	// It returns models.ErrNotFound like ApproveOAuthAuthRequest.
	DenyOAuthAuthRequest(ctx context.Context, id models.OAuthAuthRequestID, userID models.UserID, now time.Time) error
	// SaveOAuthAuthCode attaches the code to an approved request that has no
	// code yet. It returns models.ErrNotFound otherwise.
	SaveOAuthAuthCode(ctx context.Context, id models.OAuthAuthRequestID, codeHash models.OAuthSecretHash, expiresAt time.Time) error
	// ConsumeAuthCode marks the code used, once. A second use of a consumed
	// code revokes its grant and returns models.ErrOAuthCodeReplay. Unknown,
	// expired or revoked codes return models.ErrOAuthInvalidGrant.
	ConsumeAuthCode(ctx context.Context, codeHash models.OAuthSecretHash, now time.Time) (*models.OAuthAuthRequest, error)

	// IssueOAuthTokens stores a new access token, and a refresh token when
	// issue.RefreshHash is set, for an active grant. A revoked grant returns
	// models.ErrOAuthInvalidGrant.
	IssueOAuthTokens(ctx context.Context, grantID models.OAuthGrantID, issue models.OAuthTokenIssue, now time.Time) error
	// CheckRefreshToken reports whether a refresh token can be used, without
	// consuming it. A consumed token revokes its grant (RefreshReplay).
	CheckRefreshToken(ctx context.Context, tokenHash models.OAuthSecretHash, now time.Time) (models.RefreshOutcome, error)
	// RotateRefreshToken consumes oldHash and stores the new access and
	// refresh tokens in one transaction. issue.RefreshHash is required. A
	// consumed oldHash revokes the grant and commits (RefreshReplay).
	RotateRefreshToken(ctx context.Context, oldHash models.OAuthSecretHash, issue models.OAuthTokenIssue, now time.Time) (models.RefreshOutcome, error)
	// AuthenticateOAuthAccessToken returns the grant and user for an
	// unexpired access token whose grant is active, whose resource equals
	// resource, and whose user is active and human. Anything else returns
	// models.ErrNotFound.
	AuthenticateOAuthAccessToken(ctx context.Context, idHash models.OAuthSecretHash, resource string, now time.Time) (*models.OAuthGrant, *models.User, error)
	TouchOAuthGrant(ctx context.Context, id models.OAuthGrantID, now time.Time) error

	// RevokeGrant revokes one of userID's active grants, or returns
	// models.ErrNotFound.
	RevokeGrant(ctx context.Context, id models.OAuthGrantID, userID models.UserID, now time.Time) error
	// RevokeGrantByToken revokes the client's grant that owns the access-token
	// ID hash or refresh-token hash. An unknown token is not an error
	// (RFC 7009 section 2.2).
	RevokeGrantByToken(ctx context.Context, tokenHash models.OAuthSecretHash, clientID models.OAuthClientID, now time.Time) error
	// ListGrantsForUser returns userID's active grants, newest first.
	ListGrantsForUser(ctx context.Context, userID models.UserID) ([]*models.OAuthGrant, error)
	// DeleteExpiredOAuthRows removes expired requests and access
	// tokens. Refresh rows, consumed ones included, stay until no
	// token of their grant is live, so replays are still detected.
	DeleteExpiredOAuthRows(ctx context.Context, now time.Time) error
}

// AuditStore persists audit events. It has no update and no delete by ID:
// events leave the table only through PruneAuditEvents.
type AuditStore interface {
	// InsertAuditEvent stores e and sets e.OccurredAt to the stored value,
	// UTC at models.AuditTimePrecision.
	InsertAuditEvent(ctx context.Context, e *models.AuditEvent) error
	// ListAuditEvents returns up to f.Limit events that match f, ordered by
	// occurred_at then id, after f.Cursor when it is set.
	ListAuditEvents(ctx context.Context, f models.AuditFilter) ([]models.AuditEvent, error)
	// CountAuditEvents counts the events that match f, stopping at maxRows. It
	// ignores f.Cursor, f.Ascending and f.Limit.
	CountAuditEvents(ctx context.Context, f models.AuditFilter, maxRows int) (int, error)
	// PruneAuditEvents deletes up to batch of the oldest events that are
	// older than before or beyond the keepRows newest, and returns how many
	// it deleted. A zero before or keepRows disables that rule.
	PruneAuditEvents(ctx context.Context, before time.Time, keepRows, batch int) (int, error)
}

// StoreOps is the full set of data operations across every domain, with no
// lifecycle (Close) or transaction control (WithTx). It is what a WithTx
// callback receives, and what consumers should accept when they don't manage
// the connection lifecycle themselves.
//
// It composes one interface per domain; together they cover every metadata
// domain. Every method speaks pkg/models types — no sqlc or driver types leak
// through.
type StoreOps interface {
	SessionStore
	UserPreferenceStore
	UserStore
	SourceStore
	SavedQueryStore
	SavedViewStore
	CollectionStore
	DashboardStore
	AlertStore
	JobStore
	ReportStore
	QueryHistoryStore
	ExportJobStore
	QueryShareStore
	ProvisioningStore
	TeamStore
	SettingsStore
	TokenStore
	OAuthStore
	AuditStore
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
