package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"time"

	"github.com/mr-karan/logchef/internal/store/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

func (db *DB) EnqueueDelivery(ctx context.Context, d *models.NotificationDelivery) (bool, error) {
	n, err := db.writeQueries.EnqueueDelivery(ctx, sqlc.EnqueueDeliveryParams{
		DestinationID: int64(d.DestinationID),
		Producer:      d.Producer,
		ProducerRef:   d.ProducerRef,
		Recipient:     d.Recipient,
		SequenceKey:   d.SequenceKey,
		Event:         d.Event,
		DedupKey:      d.DedupKey,
		MessageJson:   d.MessageJSON,
	})
	if err != nil {
		return false, fmt.Errorf("error enqueueing delivery %s: %w", d.DedupKey, translateBusy(err))
	}
	return n == 1, nil
}

func (db *DB) ExpireEmailAttempts(ctx context.Context, errText string) (int64, error) {
	n, err := db.writeQueries.ExpireEmailAttempts(ctx, nullString(errText))
	if err != nil {
		return 0, fmt.Errorf("error expiring email delivery attempts: %w", translateBusy(err))
	}
	return n, nil
}

func (db *DB) ReserveDeliveries(ctx context.Context, owner string, lease time.Duration, limit int) ([]*models.NotificationDelivery, error) {
	rows, err := db.writeQueries.ReserveDeliveries(ctx, sqlc.ReserveDeliveriesParams{
		Owner:         nullString(owner),
		LeaseModifier: fmt.Sprintf("+%.3f seconds", lease.Seconds()),
		MaxRows:       int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("error reserving deliveries: %w", translateBusy(err))
	}
	out := make([]*models.NotificationDelivery, len(rows))
	for i := range rows {
		out[i] = deliveryFromSQLC(&rows[i])
	}
	// RETURNING does not keep the subquery's order.
	slices.SortFunc(out, func(a, b *models.NotificationDelivery) int { return int(a.ID - b.ID) })
	return out, nil
}

func (db *DB) MarkDeliveryAttempting(ctx context.Context, id, token int64) (bool, error) {
	n, err := db.writeQueries.MarkDeliveryAttempting(ctx, sqlc.MarkDeliveryAttemptingParams{ID: id, ReservedToken: token})
	if err != nil {
		return false, fmt.Errorf("error marking delivery %d attempting: %w", id, translateBusy(err))
	}
	return n == 1, nil
}

func (db *DB) ReleaseDelivery(ctx context.Context, id, token int64) (bool, error) {
	n, err := db.writeQueries.ReleaseDelivery(ctx, sqlc.ReleaseDeliveryParams{ID: id, ReservedToken: token})
	if err != nil {
		return false, fmt.Errorf("error releasing delivery %d: %w", id, translateBusy(err))
	}
	return n == 1, nil
}

func (db *DB) FinishDelivery(ctx context.Context, id, token int64, out models.DeliveryOutcome) (bool, error) {
	n, err := db.writeQueries.FinishDelivery(ctx, sqlc.FinishDeliveryParams{
		Status:        string(out.Status),
		RetryModifier: fmt.Sprintf("+%.3f seconds", out.RetryAfter.Seconds()),
		LastError:     nullString(out.Error),
		ID:            id,
		ReservedToken: token,
	})
	if err != nil {
		return false, fmt.Errorf("error finishing delivery %d: %w", id, translateBusy(err))
	}
	return n == 1, nil
}

func (db *DB) PruneDeliveries(ctx context.Context, age time.Duration) (int64, error) {
	n, err := db.writeQueries.PruneDeliveries(ctx, fmt.Sprintf("-%.3f seconds", age.Seconds()))
	if err != nil {
		return 0, fmt.Errorf("error pruning deliveries: %w", translateBusy(err))
	}
	return n, nil
}

func (db *DB) RecordDestinationDelivery(ctx context.Context, id models.DestinationID, status models.DeliveryStatus, errText string) error {
	err := db.writeQueries.RecordDestinationDelivery(ctx, sqlc.RecordDestinationDeliveryParams{
		Status:    nullString(string(status)),
		LastError: nullString(errText),
		ID:        int64(id),
	})
	if err != nil {
		return fmt.Errorf("error recording delivery of destination %d: %w", id, translateBusy(err))
	}
	return nil
}

func (db *DB) GetDelivery(ctx context.Context, id int64) (*models.NotificationDelivery, error) {
	row, err := db.readQueries.GetDelivery(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("error getting delivery %d: %w", id, translateNotFound(err))
	}
	return deliveryFromSQLC(&row), nil
}

func (db *DB) ListDeliverySummaries(ctx context.Context, producer string, refs []string) (map[string][]models.DeliverySummary, error) {
	out := make(map[string][]models.DeliverySummary)
	if len(refs) == 0 {
		return out, nil
	}
	rows, err := db.readQueries.ListDeliverySummaries(ctx, sqlc.ListDeliverySummariesParams{Producer: producer, Refs: refs})
	if err != nil {
		return nil, fmt.Errorf("error listing deliveries: %w", err)
	}
	for i := range rows {
		r := &rows[i]
		out[r.ProducerRef] = append(out[r.ProducerRef], models.DeliverySummary{
			DestinationID:   models.DestinationID(r.DestinationID),
			DestinationName: r.DestinationName,
			DestinationType: models.DestinationType(r.DestinationType),
			Recipient:       r.Recipient,
			Event:           r.Event,
			Status:          models.DeliveryStatus(r.Status),
			Attempts:        int(r.Attempts),
			LastError:       r.LastError.String,
			SentAt:          nullTimePtr(r.SentAt),
			CreatedAt:       r.CreatedAt,
		})
	}
	return out, nil
}

func deliveryFromSQLC(row *sqlc.NotificationDelivery) *models.NotificationDelivery {
	return &models.NotificationDelivery{
		ID:            row.ID,
		DestinationID: models.DestinationID(row.DestinationID),
		Producer:      row.Producer,
		ProducerRef:   row.ProducerRef,
		Recipient:     row.Recipient,
		SequenceKey:   row.SequenceKey,
		Event:         row.Event,
		DedupKey:      row.DedupKey,
		MessageJSON:   row.MessageJson,
		Status:        models.DeliveryStatus(row.Status),
		Attempts:      int(row.Attempts),
		NextAttemptAt: row.NextAttemptAt,
		ReservedToken: row.ReservedToken,
		ReservedUntil: nullTimePtr(row.ReservedUntil),
		LastError:     row.LastError.String,
		CreatedAt:     row.CreatedAt,
		SentAt:        nullTimePtr(row.SentAt),
	}
}

func nullTimePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}
