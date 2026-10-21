package askchef

import (
	"context"
	"errors"
	"log/slog"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

var (
	// ErrAlertNotInSource means the alert watches another source.
	ErrAlertNotInSource = errors.New("alert does not belong to this source")
	// ErrAlertEntryNotFound means the alert has no history entry with that ID.
	ErrAlertEntryNotFound = errors.New("alert history entry not found")
)

// AlertContext is the alert the user asks about. Entry is nil when the
// request named no history entry.
type AlertContext struct {
	Alert *models.Alert
	Entry *models.AlertHistoryEntry
}

// LoadAlertContext reads the alert of ref after checking that p can see it
// and that it watches sourceID. An alert p cannot see returns
// core.ErrAlertNotFound.
func LoadAlertContext(ctx context.Context, db store.StoreOps, log *slog.Logger, p access.Principal, sourceID models.SourceID, ref AlertRef) (*AlertContext, error) {
	alert, err := core.GetAlertForPrincipal(ctx, db, log, p, ref.AlertID)
	if err != nil {
		return nil, err
	}
	if alert.SourceID != sourceID {
		return nil, ErrAlertNotInSource
	}
	alertCtx := &AlertContext{Alert: alert}
	if ref.HistoryID == 0 {
		return alertCtx, nil
	}
	entry, err := db.GetAlertHistoryEntry(ctx, alert.ID, ref.HistoryID)
	if errors.Is(err, models.ErrNotFound) {
		return nil, ErrAlertEntryNotFound
	}
	if err != nil {
		return nil, err
	}
	alertCtx.Entry = entry
	return alertCtx, nil
}
