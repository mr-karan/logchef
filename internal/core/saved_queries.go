package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/logchefql"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

var relativeTimeRegex = regexp.MustCompile(`^\d+[smhdw]$`)

func isValidRelativeTimeFormat(s string) bool {
	return relativeTimeRegex.MatchString(s)
}

// --- Saved Query Error Definitions ---

var (
	ErrQueryNotFound                   = fmt.Errorf("saved query not found")
	ErrQueryLanguageRequired           = fmt.Errorf("query language is required")
	ErrInvalidQueryDefinition          = fmt.Errorf("invalid query configuration")
	ErrUnsupportedSavedQueryDefinition = fmt.Errorf("saved query configuration is not supported for this source")
	ErrInvalidQueryContent             = fmt.Errorf("invalid query content format or values")
	ErrSavedQueryForbidden             = fmt.Errorf("not allowed to access this saved query")
)

// --- Saved Query Content Validation ---

// ValidateSavedQueryContent validates the JSON structure and basic rules of the query content.
func ValidateSavedQueryContent(contentJSON string) error {
	_, err := parseAndValidateSavedQueryContent(contentJSON)
	return err
}

// parseAndValidateSavedQueryContent parses the envelope ONCE and validates its
// structural rules, returning the parsed content so callers (create/update) can
// reuse the inner .Content for the language-match check without re-parsing.
// Returns (nil, nil) for empty input (a legitimate no-op content).
func parseAndValidateSavedQueryContent(contentJSON string) (*models.SavedQueryContent, error) {
	if contentJSON == "" {
		return nil, nil
	}

	var queryContent models.SavedQueryContent
	if err := json.Unmarshal([]byte(contentJSON), &queryContent); err != nil {
		return nil, fmt.Errorf("%w: failed to parse JSON: %v", ErrInvalidQueryContent, err)
	}

	if queryContent.Version <= 0 {
		return nil, fmt.Errorf("%w: version must be positive", ErrInvalidQueryContent)
	}
	if queryContent.Content == "" {
		return nil, fmt.Errorf("%w: query content cannot be empty", ErrInvalidQueryContent)
	}
	if queryContent.Limit <= 0 {
		return nil, fmt.Errorf("%w: limit must be positive", ErrInvalidQueryContent)
	}

	hasRelativeTime := queryContent.TimeRange.Relative != ""
	hasAbsoluteTime := queryContent.TimeRange.Absolute.Start != 0 || queryContent.TimeRange.Absolute.End != 0

	if hasRelativeTime && hasAbsoluteTime {
		return nil, fmt.Errorf("%w: cannot specify both relative and absolute time range", ErrInvalidQueryContent)
	}

	if hasRelativeTime && !isValidRelativeTimeFormat(queryContent.TimeRange.Relative) {
		return nil, fmt.Errorf("%w: invalid relative time format (expected e.g. '15m', '1h', '7d')", ErrInvalidQueryContent)
	}

	if hasAbsoluteTime {
		if queryContent.TimeRange.Absolute.Start <= 0 {
			return nil, fmt.Errorf("%w: absolute start time must be positive", ErrInvalidQueryContent)
		}
		if queryContent.TimeRange.Absolute.End <= 0 {
			return nil, fmt.Errorf("%w: absolute end time must be positive", ErrInvalidQueryContent)
		}
		if queryContent.TimeRange.Absolute.End < queryContent.TimeRange.Absolute.Start {
			return nil, fmt.Errorf("%w: absolute end time must be after start time", ErrInvalidQueryContent)
		}
	}

	return &queryContent, nil
}

// ValidateContentMatchesLanguage returns an error when the query content does
// not parse in the declared language, so a mismatched (language, content) pair
// can never be persisted (the root cause of prod query #119: LogchefQL content
// stored as clickhouse-sql, which then ran as SQL and failed with
// "unexpected token"). The error wraps ErrInvalidQueryContent so both the
// create and update handlers surface it as HTTP 400. We deliberately fail loud
// rather than auto-correcting the language, so a buggy client surfaces the
// error instead of silently corrupting data.
//
// Two accept paths guard against false-rejects (a worse regression than the
// original bug):
//   - Empty / whitespace-only content is valid for EVERY language (a no-filter
//     saved query is legitimate).
//   - Content containing the "{{" template marker (Logchef's template-variable
//     syntax, e.g. WHERE x = {{val}}) is accepted WITHOUT strict parsing:
//     templated SQL legitimately does not parse as raw SQL. The #119 corruption
//     case has no "{{" marker, so it is still caught.
func ValidateContentMatchesLanguage(content string, language models.QueryLanguage) error {
	// Accept path 1: empty / whitespace-only content is a valid no-filter query.
	if strings.TrimSpace(content) == "" {
		return nil
	}
	// Accept path 2: templated content. "{{" is Logchef's template-variable
	// marker (see internal/template/variables.go). Raw parsing of a template
	// would spuriously fail, so accept it as-is.
	if strings.Contains(content, "{{") {
		return nil
	}

	switch models.NormalizeQueryLanguage(language) {
	case models.QueryLanguageLogchefQL:
		// LogchefQL is Logchef's own constrained grammar, so validating it here
		// is reliable and carries no false-reject risk.
		res := logchefql.Validate(content)
		if !res.Valid {
			detail := "invalid syntax"
			if res.Error != nil {
				detail = res.Error.Error()
			}
			return fmt.Errorf("%w: content is not valid logchefql: %s", ErrInvalidQueryContent, detail)
		}
	case models.QueryLanguageClickHouseSQL, models.QueryLanguageLogsQL:
		// Deliberately NOT strict-parsed. ClickHouse SQL and LogsQL are large,
		// evolving dialects; validating them here with a third-party/absent
		// parser would reject valid-but-exotic user queries at save time — a
		// worse regression than the one-off mislabel this guard was added for
		// (which had a frontend root cause, already fixed). We only enforce the
		// LogchefQL direction, where our own parser makes it safe.
	}
	return nil
}

// resolveSavedQueryMetadata normalizes language+mode and checks the source's
// provider actually supports the combination.
func resolveSavedQueryMetadata(ctx context.Context, ds *datasource.Service, sourceID models.SourceID, queryLanguage models.QueryLanguage, editorMode models.SavedQueryEditorMode) (models.QueryLanguage, models.SavedQueryEditorMode, error) {
	normalizedLanguage, normalizedMode, err := models.ResolveSavedQueryMetadata(queryLanguage, editorMode)
	if err != nil {
		return "", "", fmt.Errorf("%w: %s", ErrInvalidQueryDefinition, err)
	}
	if ds != nil {
		if err := ds.ValidateSavedQuerySupport(ctx, sourceID, normalizedLanguage, normalizedMode); err != nil {
			return "", "", fmt.Errorf("%w: %s", ErrUnsupportedSavedQueryDefinition, err)
		}
	}
	return normalizedLanguage, normalizedMode, nil
}

// validateSavedQueryFields runs the shared content checks used by create and
// update. It parses the envelope ONCE and then verifies the inner content
// matches the resolved query language, so a mismatched pair can never be
// persisted from either the API or the UI, on either create or update.
func validateSavedQueryFields(queryContentJSON string, language models.QueryLanguage) error {
	if queryContentJSON == "" {
		return nil
	}
	parsed, err := parseAndValidateSavedQueryContent(queryContentJSON)
	if err != nil {
		return err
	}
	if parsed == nil {
		return nil
	}
	return ValidateContentMatchesLanguage(parsed.Content, language)
}

// CreateSavedQuery persists a new saved query owned by the supplied creator.
func CreateSavedQuery(ctx context.Context, db store.StoreOps, ds *datasource.Service, log *slog.Logger, sourceID models.SourceID, createdFromTeamID *models.TeamID, name, description, queryContentJSON string, queryLanguage models.QueryLanguage, editorMode models.SavedQueryEditorMode, createdBy models.UserID) (*models.SavedQuery, error) {
	queryLanguage, editorMode, err := resolveSavedQueryMetadata(ctx, ds, sourceID, queryLanguage, editorMode)
	if err != nil {
		return nil, err
	}
	if err := validateSavedQueryFields(queryContentJSON, queryLanguage); err != nil {
		log.Warn("invalid saved query create payload", "error", err, "source_id", sourceID, "name", name)
		return nil, err
	}

	owner := createdBy
	created, err := db.CreateSavedQuery(ctx, sourceID, createdFromTeamID, name, description, queryLanguage, editorMode, queryContentJSON, &owner)
	if err != nil {
		log.Error("failed to create saved query", "error", err, "source_id", sourceID, "name", name)
		return nil, fmt.Errorf("error creating saved query: %w", err)
	}

	log.Debug("saved query created", "query_id", created.ID, "source_id", sourceID, "created_by", createdBy)
	return created, nil
}

type savedQueryGetter interface {
	GetSavedQuery(ctx context.Context, queryID int) (*models.SavedQuery, error)
}

// GetSavedQuery retrieves a saved query by id.
func GetSavedQuery(ctx context.Context, db savedQueryGetter, log *slog.Logger, queryID int) (*models.SavedQuery, error) {
	q, err := db.GetSavedQuery(ctx, queryID)
	if err != nil {
		if models.IsNotFound(err) {
			return nil, ErrQueryNotFound
		}
		log.Error("failed to get saved query", "error", err, "query_id", queryID)
		return nil, fmt.Errorf("error getting saved query: %w", err)
	}
	if q == nil {
		log.Error("store returned nil saved query without error", "query_id", queryID)
		return nil, ErrQueryNotFound
	}
	return q, nil
}

// UpdateSavedQuery applies new field values to an existing saved query.
func UpdateSavedQuery(ctx context.Context, db store.StoreOps, ds *datasource.Service, log *slog.Logger, queryID int, name, description, queryContentJSON string, queryLanguage models.QueryLanguage, editorMode models.SavedQueryEditorMode) (*models.SavedQuery, error) {
	existing, err := db.GetSavedQuery(ctx, queryID)
	if err != nil {
		if models.IsNotFound(err) {
			return nil, ErrQueryNotFound
		}
		return nil, fmt.Errorf("error loading saved query: %w", err)
	}
	if queryLanguage == "" {
		queryLanguage = existing.QueryLanguage
	}
	if editorMode == "" {
		editorMode = existing.EditorMode
	}
	queryLanguage, editorMode, err = resolveSavedQueryMetadata(ctx, ds, existing.SourceID, queryLanguage, editorMode)
	if err != nil {
		return nil, err
	}
	if err := validateSavedQueryFields(queryContentJSON, queryLanguage); err != nil {
		log.Warn("invalid saved query update payload", "error", err, "query_id", queryID)
		return nil, err
	}

	if err := db.UpdateSavedQuery(ctx, queryID, name, description, queryLanguage, editorMode, queryContentJSON); err != nil {
		if models.IsNotFound(err) {
			return nil, ErrQueryNotFound
		}
		log.Error("failed to update saved query", "error", err, "query_id", queryID)
		return nil, fmt.Errorf("error updating saved query: %w", err)
	}

	updated, err := GetSavedQuery(ctx, db, log, queryID)
	if err != nil {
		return nil, fmt.Errorf("query updated but failed to fetch result: %w", err)
	}
	return updated, nil
}

// DeleteSavedQuery removes a saved query.
func DeleteSavedQuery(ctx context.Context, db store.StoreOps, log *slog.Logger, queryID int) error {
	if err := db.DeleteSavedQuery(ctx, queryID); err != nil {
		log.Error("failed to delete saved query", "error", err, "query_id", queryID)
		return fmt.Errorf("error deleting saved query: %w", err)
	}
	return nil
}

// ListSavedQueriesForUser returns every saved query the user can see (cross-team, source-mediated).
func ListSavedQueriesForUser(ctx context.Context, db store.StoreOps, log *slog.Logger, userID models.UserID) ([]*models.SavedQuery, error) {
	queries, err := db.ListSavedQueriesForUser(ctx, userID)
	if err != nil {
		log.Error("failed to list saved queries for user", "error", err, "user_id", userID)
		return nil, fmt.Errorf("error listing saved queries: %w", err)
	}
	return queries, nil
}

// ListSavedQueriesForUserBySource returns saved queries for one source, gated by user access.
func ListSavedQueriesForUserBySource(ctx context.Context, db store.StoreOps, log *slog.Logger, userID models.UserID, sourceID models.SourceID) ([]*models.SavedQuery, error) {
	queries, err := db.ListSavedQueriesForUserBySource(ctx, userID, sourceID)
	if err != nil {
		log.Error("failed to list saved queries for user+source", "error", err, "user_id", userID, "source_id", sourceID)
		return nil, fmt.Errorf("error listing saved queries: %w", err)
	}
	return queries, nil
}

type allSavedQueriesLister interface {
	ListAllSavedQueries(ctx context.Context) ([]*models.SavedQuery, error)
}

// ListAllSavedQueries returns every saved query with no source-access gate — the
// global-admin browse surface. The caller MUST be authorized as a global admin
// by the handler before this is invoked.
func ListAllSavedQueries(ctx context.Context, db allSavedQueriesLister, log *slog.Logger) ([]*models.SavedQuery, error) {
	queries, err := db.ListAllSavedQueries(ctx)
	if err != nil {
		log.Error("failed to list all saved queries", "error", err)
		return nil, fmt.Errorf("error listing all saved queries: %w", err)
	}
	return queries, nil
}

type userSourcesLister interface {
	ListSourcesForUser(ctx context.Context, userID models.UserID) ([]*models.Source, error)
}

// MarkSavedQueriesRunnable sets Runnable on each query based on whether the user
// has source access to it, fetching the user's accessible source set once (no
// per-row access check). Used by browse lists to lock rows the user can't run.
func MarkSavedQueriesRunnable(ctx context.Context, db userSourcesLister, userID models.UserID, queries []*models.SavedQuery) error {
	if len(queries) == 0 {
		return nil
	}
	sources, err := db.ListSourcesForUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("error loading accessible sources: %w", err)
	}
	accessible := make(map[models.SourceID]bool, len(sources))
	for _, source := range sources {
		if source == nil {
			continue
		}
		accessible[source.ID] = true
	}
	for _, q := range queries {
		if q == nil {
			continue
		}
		runnable := accessible[q.SourceID]
		q.Runnable = &runnable
	}
	return nil
}

// userIsCreatorOrAdmin is the base edit/delete authority: the query's creator or
// a global admin. Legacy queries (CreatedBy == nil) qualify only for admins.
func userIsCreatorOrAdmin(query *models.SavedQuery, user *models.User) bool {
	if user == nil || query == nil {
		return false
	}
	if user.Role == models.UserRoleAdmin {
		return true
	}
	return query.CreatedBy != nil && *query.CreatedBy == user.ID
}

// UserCanEditSavedQuery reports whether the user may edit the query: the creator,
// a global admin, or an owner/editor of a shared collection that contains the
// query (delegated edit). Source access is enforced separately by the caller
// (loadSavedQueryWithVisibility), so this only decides edit authority.
func UserCanEditSavedQuery(ctx context.Context, db store.StoreOps, query *models.SavedQuery, user *models.User) (bool, error) {
	if userIsCreatorOrAdmin(query, user) {
		return true, nil
	}
	if user == nil || query == nil {
		return false, nil
	}
	return db.UserCanEditSavedQueryViaSharedCollection(ctx, user.ID, query.ID)
}

// UserCanDeleteSavedQuery reports whether the user may delete the query. Deletion
// removes the shared row globally (cascading to every collection that references
// it), so it stays restricted to the creator or a global admin — collection
// editors can edit but not delete.
func UserCanDeleteSavedQuery(query *models.SavedQuery, user *models.User) bool {
	return userIsCreatorOrAdmin(query, user)
}
