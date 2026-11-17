package postgres

import (
	"context"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/mr-karan/logchef/internal/store/postgres/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

func (s *Store) EnqueueDelivery(ctx context.Context, d *models.NotificationDelivery) (bool, error) {
	n, err := s.q.EnqueueDelivery(ctx, sqlc.EnqueueDeliveryParams{
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
		return false, fmt.Errorf("error enqueueing delivery %s: %w", d.DedupKey, err)
	}
	return n == 1, nil
}

func (s *Store) ExpireEmailAttempts(ctx context.Context, errText string) (int64, error) {
	n, err := s.q.ExpireEmailAttempts(ctx, text(errText))
	if err != nil {
		return 0, fmt.Errorf("error expiring email delivery attempts: %w", err)
	}
	return n, nil
}

func (s *Store) ReserveDeliveries(ctx context.Context, owner string, lease time.Duration, limit int) ([]*models.NotificationDelivery, error) {
	if limit < 1 || limit > math.MaxInt32 {
		return nil, fmt.Errorf("error reserving deliveries: limit %d out of range", limit)
	}
	rows, err := s.q.ReserveDeliveries(ctx, sqlc.ReserveDeliveriesParams{
		Owner:        text(owner),
		LeaseSeconds: lease.Seconds(),
		MaxRows:      int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("error reserving deliveries: %w", err)
	}
	out := make([]*models.NotificationDelivery, len(rows))
	for i := range rows {
		out[i] = deliveryToModel(&rows[i])
	}
	// RETURNING does not keep the subquery's order.
	slices.SortFunc(out, func(a, b *models.NotificationDelivery) int { return int(a.ID - b.ID) })
	return out, nil
}

func (s *Store) MarkDeliveryAttempting(ctx context.Context, id, token int64) (bool, error) {
	n, err := s.q.MarkDeliveryAttempting(ctx, sqlc.MarkDeliveryAttemptingParams{ID: id, ReservedToken: token})
	if err != nil {
		return false, fmt.Errorf("error marking delivery %d attempting: %w", id, err)
	}
	return n == 1, nil
}

func (s *Store) ReleaseDelivery(ctx context.Context, id, token int64, after time.Duration) (bool, error) {
	n, err := s.q.ReleaseDelivery(ctx, sqlc.ReleaseDeliveryParams{DelaySeconds: after.Seconds(), ID: id, ReservedToken: token})
	if err != nil {
		return false, fmt.Errorf("error releasing delivery %d: %w", id, err)
	}
	return n == 1, nil
}

func (s *Store) CancelDeliveries(ctx context.Context, producer, producerRef, errText string) (int64, error) {
	n, err := s.q.CancelDeliveries(ctx, sqlc.CancelDeliveriesParams{LastError: text(errText), Producer: producer, ProducerRef: producerRef})
	if err != nil {
		return 0, fmt.Errorf("error cancelling deliveries of %s %s: %w", producer, producerRef, err)
	}
	return n, nil
}

func (s *Store) FinishDelivery(ctx context.Context, id, token int64, out models.DeliveryOutcome) (bool, error) {
	n, err := s.q.FinishDelivery(ctx, sqlc.FinishDeliveryParams{
		Status:        string(out.Status),
		RetrySeconds:  out.RetryAfter.Seconds(),
		LastError:     text(out.Error),
		ID:            id,
		ReservedToken: token,
	})
	if err != nil {
		return false, fmt.Errorf("error finishing delivery %d: %w", id, err)
	}
	return n == 1, nil
}

func (s *Store) PruneDeliveries(ctx context.Context, age time.Duration) (int64, error) {
	n, err := s.q.PruneDeliveries(ctx, age.Seconds())
	if err != nil {
		return 0, fmt.Errorf("error pruning deliveries: %w", err)
	}
	return n, nil
}

func (s *Store) RecordDestinationDelivery(ctx context.Context, id models.DestinationID, status models.DeliveryStatus, errText string) error {
	err := s.q.RecordDestinationDelivery(ctx, sqlc.RecordDestinationDeliveryParams{Status: text(string(status)), LastError: text(errText), ID: int64(id)})
	if err != nil {
		return fmt.Errorf("error recording delivery of destination %d: %w", id, err)
	}
	return nil
}

func (s *Store) GetDelivery(ctx context.Context, id int64) (*models.NotificationDelivery, error) {
	row, err := s.q.GetDelivery(ctx, id)
	if err != nil {
		if notFound(err) {
			return nil, fmt.Errorf("error getting delivery %d: %w", id, models.ErrNotFound)
		}
		return nil, fmt.Errorf("error getting delivery %d: %w", id, err)
	}
	return deliveryToModel(&row), nil
}

func (s *Store) ListDeliverySummaries(ctx context.Context, producer string, refs []string) (map[string][]models.DeliverySummary, error) {
	out := make(map[string][]models.DeliverySummary)
	if len(refs) == 0 {
		return out, nil
	}
	rows, err := s.q.ListDeliverySummaries(ctx, sqlc.ListDeliverySummariesParams{Producer: producer, Refs: refs})
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
			LastError:       textStr(r.LastError),
			SentAt:          tsPtr(r.SentAt),
			CreatedAt:       r.CreatedAt.Time,
		})
	}
	return out, nil
}

func deliveryToModel(r *sqlc.NotificationDelivery) *models.NotificationDelivery {
	return &models.NotificationDelivery{
		ID:            r.ID,
		DestinationID: models.DestinationID(r.DestinationID),
		Producer:      r.Producer,
		ProducerRef:   r.ProducerRef,
		Recipient:     r.Recipient,
		SequenceKey:   r.SequenceKey,
		Event:         r.Event,
		DedupKey:      r.DedupKey,
		MessageJSON:   r.MessageJson,
		Status:        models.DeliveryStatus(r.Status),
		Attempts:      int(r.Attempts),
		NextAttemptAt: r.NextAttemptAt.Time,
		ReservedToken: r.ReservedToken,
		ReservedUntil: tsPtr(r.ReservedUntil),
		LastError:     textStr(r.LastError),
		CreatedAt:     r.CreatedAt.Time,
		SentAt:        tsPtr(r.SentAt),
	}
}
