package core

// Visibility checks for objects bound to a source. A user sees a saved query
// or alert only through a team that has its source. Global admins get no
// bypass here; edit rules (UserCanEditSavedQuery, UserCanEditAlert) are
// separate. These functions check visibility only; callers check scope.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

var (
	// ErrAccessCheck wraps a failure to read source access from the store.
	ErrAccessCheck = errors.New("failed to verify source access")
	// ErrSourceAccessDenied means no team of the user has the source.
	ErrSourceAccessDenied = errors.New("no team you belong to has access to this source")
)

// GetSavedQueryForPrincipal returns the saved query if the principal can see
// it. A query the principal cannot see returns ErrQueryNotFound, so callers
// do not learn that it exists.
func GetSavedQueryForPrincipal(ctx context.Context, db store.StoreOps, log *slog.Logger, p access.Principal, queryID int) (*models.SavedQuery, error) {
	query, err := GetSavedQuery(ctx, db, log, queryID)
	if err != nil {
		return nil, err
	}
	if err := requireSourceVisible(ctx, db, p, query.SourceID); err != nil {
		if errors.Is(err, ErrSourceAccessDenied) {
			return nil, ErrQueryNotFound
		}
		return nil, err
	}
	return query, nil
}

// GetAlertForPrincipal returns the alert if the principal can see it. An
// alert the principal cannot see returns ErrAlertNotFound.
func GetAlertForPrincipal(ctx context.Context, db store.StoreOps, log *slog.Logger, p access.Principal, alertID models.AlertID) (*models.Alert, error) {
	alert, err := GetAlert(ctx, db, log, alertID)
	if err != nil {
		return nil, err
	}
	if err := requireSourceVisible(ctx, db, p, alert.SourceID); err != nil {
		if errors.Is(err, ErrSourceAccessDenied) {
			return nil, ErrAlertNotFound
		}
		return nil, err
	}
	return alert, nil
}

// ListAlertHistoryForPrincipal returns the history of an alert the principal
// can see.
func ListAlertHistoryForPrincipal(ctx context.Context, db store.StoreOps, log *slog.Logger, p access.Principal, alertID models.AlertID, limit int) ([]*models.AlertHistoryEntry, error) {
	alert, err := GetAlertForPrincipal(ctx, db, log, p, alertID)
	if err != nil {
		return nil, err
	}
	return ListAlertHistory(ctx, db, alert.ID, limit)
}

// ListAlertsBySourceForPrincipal returns the alerts of one source. It returns
// ErrSourceAccessDenied when the principal cannot see the source.
func ListAlertsBySourceForPrincipal(ctx context.Context, db store.StoreOps, p access.Principal, sourceID models.SourceID) ([]*models.Alert, error) {
	if err := requireSourceVisible(ctx, db, p, sourceID); err != nil {
		return nil, err
	}
	return ListAlertsBySource(ctx, db, sourceID)
}

// TestAlertQueryForPrincipal runs an alert test query. The principal needs
// alerts:write and must see the source through a team; global admins get no
// bypass. It returns ErrSourceAccessDenied when the source is not visible.
func TestAlertQueryForPrincipal(ctx context.Context, db store.StoreOps, ds *datasource.Service, p access.Principal, sourceID models.SourceID, req *models.TestAlertQueryRequest) (*models.TestAlertQueryResponse, error) {
	if err := p.Require(models.TokenScopeAlertsWrite); err != nil {
		return nil, err
	}
	if err := requireSourceVisible(ctx, db, p, sourceID); err != nil {
		return nil, err
	}
	return testAlertQuery(ctx, db, ds, sourceID, req)
}

func requireSourceVisible(ctx context.Context, db store.StoreOps, p access.Principal, sourceID models.SourceID) error {
	if p.User == nil {
		return ErrSourceAccessDenied
	}
	visible, err := db.UserHasSourceAccess(ctx, p.User.ID, sourceID)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrAccessCheck, err)
	}
	if !visible {
		return ErrSourceAccessDenied
	}
	return nil
}
