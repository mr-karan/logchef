package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/mr-karan/logchef/internal/jobs"
	"github.com/mr-karan/logchef/internal/metrics"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// DeliverKind is the job that drains the delivery queue. It has one job, with
// the key DrainKey.
const (
	DeliverKind jobs.Kind = "notify.deliver"
	DrainKey    string    = "default"
)

const (
	// drainInterval is how often the drain runs when nothing kicks it.
	drainInterval = 10 * time.Second
	// drainTimeout is the drain handler's deadline.
	drainTimeout = 2 * time.Minute
	// reservationLease is how long a drain owns the rows it reserved. It
	// outlasts sendTimeout, so a send that keeps its deadline is not taken
	// over.
	reservationLease = 2 * time.Minute
	sendTimeout      = 60 * time.Second
	batchSize        = 50
	sendWorkers      = 4
	// maxAttempts is the reservations a row gets before it fails.
	maxAttempts = 5
	// keepFinished is how long sent, failed and unknown rows are kept.
	keepFinished = 30 * 24 * time.Hour
	// writeTimeout bounds each outcome write, which outlives a cancelled run.
	writeTimeout = 10 * time.Second
	// minHold is the shortest wait of a held row, so a hold that ends
	// within the run cannot spin the reservation loop.
	minHold = time.Second
	// holdCheckRetry is the wait of a row whose hold check failed.
	holdCheckRetry = drainInterval
)

// retryDelays are the waits after the first four failed attempts.
var retryDelays = [maxAttempts - 1]time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute}

// errInterruptedSMTP is stored on an email row found attempting after its
// reservation ended.
const errInterruptedSMTP = "interrupted during SMTP"

// DrainJob is the stored definition of the drain. Upsert it once at startup.
func DrainJob() jobs.Spec {
	return jobs.Spec{Kind: DeliverKind, Key: DrainKey, Schedule: jobs.Every(drainInterval), Misfire: jobs.RunOnce, Enabled: true}
}

// Delivery is one notification to enqueue: one destination, or one recipient
// of an email destination.
type Delivery struct {
	DestinationID models.DestinationID
	Producer      string
	ProducerRef   string
	Recipient     string
	SequenceKey   string
	DedupKey      string
	Message       Message
}

// Enqueue writes the delivery rows inside the caller's transaction, due now,
// in order. A row whose dedup key exists is skipped.
func Enqueue(ctx context.Context, tx store.DeliveryStore, deliveries []Delivery) error {
	for i := range deliveries {
		d := &deliveries[i]
		body, err := json.Marshal(d.Message)
		if err != nil {
			return fmt.Errorf("notify: encode delivery %s: %w", d.DedupKey, err)
		}
		if _, err := tx.EnqueueDelivery(ctx, &models.NotificationDelivery{
			DestinationID: d.DestinationID,
			Producer:      d.Producer,
			ProducerRef:   d.ProducerRef,
			Recipient:     d.Recipient,
			SequenceKey:   d.SequenceKey,
			Event:         string(d.Message.Event),
			DedupKey:      d.DedupKey,
			MessageJSON:   string(body),
		}); err != nil {
			return err
		}
	}
	return nil
}

// HoldFunc decides, just before a reserved row is sent, whether it must not
// go out yet, such as an alert row whose group a silence mutes now. A
// positive wait holds the row back for that long. A held row keeps the
// attempts it has left and blocks the later rows of its sequence, as any
// pending row does.
type HoldFunc func(ctx context.Context, row *models.NotificationDelivery) (wait time.Duration, err error)

// DrainerOptions configures a Drainer. Senders maps each destination type to
// its sender; a type without one fails its rows. Hold is optional.
type DrainerOptions struct {
	Store   store.Store
	Senders map[models.DestinationType]Sender
	Hold    HoldFunc
	Logger  *slog.Logger
}

// Drainer is the handler of the drain job. A run reserves due rows, sends
// them and records each outcome under the row's reservation token, so a
// drain whose reservation was taken over changes nothing.
type Drainer struct {
	db      store.Store
	senders map[models.DestinationType]Sender
	hold    HoldFunc
	log     *slog.Logger
	owner   string
	// lease and sendTimeout are the reservation and per-send deadline; tests
	// shorten them.
	lease       time.Duration
	sendTimeout time.Duration
}

func NewDrainer(opts DrainerOptions) *Drainer {
	owner := uuid.NewString()
	return &Drainer{
		db:          opts.Store,
		senders:     opts.Senders,
		hold:        opts.Hold,
		log:         opts.Logger.With("component", "notify_drain", "owner", owner),
		owner:       owner,
		lease:       reservationLease,
		sendTimeout: sendTimeout,
	}
}

// Kind is the drain's registration for a job runner.
func (d *Drainer) Kind() jobs.KindSpec {
	return jobs.KindSpec{
		Kind:        DeliverKind,
		Handler:     func(ctx context.Context, _ jobs.Job) error { return d.Drain(ctx) },
		Timeout:     drainTimeout,
		Concurrency: 1,
	}
}

// Drain runs once: it ends email attempts that a crash interrupted, deletes
// old finished rows, then sends due rows batch by batch until none is due.
func (d *Drainer) Drain(ctx context.Context) error {
	if n, err := d.db.ExpireEmailAttempts(ctx, errInterruptedSMTP); err != nil {
		return err
	} else if n > 0 {
		d.log.Warn("marked interrupted email deliveries unknown", "rows", n)
		for range n {
			metrics.RecordNotificationDelivery(string(models.DestinationTypeEmail), string(models.DeliveryUnknown))
		}
	}
	if _, err := d.db.PruneDeliveries(ctx, keepFinished); err != nil {
		d.log.Warn("failed to prune finished deliveries", "error", err)
	}
	destinations := newDestinationCache(d.db)
	for ctx.Err() == nil {
		rows, err := d.db.ReserveDeliveries(ctx, d.owner, d.lease, batchSize)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		d.sendBatch(ctx, destinations, rows)
	}
	return ctx.Err()
}

func (d *Drainer) sendBatch(ctx context.Context, destinations *destinationCache, rows []*models.NotificationDelivery) {
	work := make(chan *models.NotificationDelivery)
	var wg sync.WaitGroup
	for range min(sendWorkers, len(rows)) {
		wg.Go(func() {
			for row := range work {
				d.send(ctx, destinations, row)
			}
		})
	}
	for _, row := range rows {
		work <- row
	}
	close(work)
	wg.Wait()
}

// send delivers one reserved row and records its outcome. A row that the
// run's end reaches before its send starts is released unsent.
func (d *Drainer) send(ctx context.Context, destinations *destinationCache, row *models.NotificationDelivery) {
	log := d.log.With("delivery_id", row.ID, "destination_id", row.DestinationID)
	if ctx.Err() != nil {
		d.release(ctx, log, row, 0)
		return
	}
	dest, err := destinations.get(ctx, row.DestinationID)
	if err != nil {
		log.Error("failed to load the delivery's destination", "error", err)
		d.release(ctx, log, row, 0)
		return
	}
	var msg Message
	sender := d.senders[dest.Type]
	switch {
	case !dest.IsActive:
		err = Permanent(fmt.Errorf("destination %q is inactive", dest.Name))
	case sender == nil:
		err = Permanent(fmt.Errorf("destination type %q has no sender yet", dest.Type))
	default:
		if decodeErr := json.Unmarshal([]byte(row.MessageJSON), &msg); decodeErr != nil {
			err = Permanent(fmt.Errorf("stored message cannot be read: %w", decodeErr))
		}
	}
	if err == nil {
		err = d.attempt(ctx, log, dest, sender, row, msg)
		if held, ok := errors.AsType[*heldError](err); ok {
			d.release(ctx, log, row, held.wait)
			return
		}
		switch {
		case errors.Is(err, errNotStarted):
			d.release(ctx, log, row, 0)
			return
		case errors.Is(err, errReservationLost):
			metrics.RecordNotificationDelivery(string(dest.Type), "reservation_lost")
			return
		}
	}
	d.finish(ctx, log, dest, row, outcome(row.Attempts, err))
}

// heldError is a send that the hold function kept back for wait.
type heldError struct{ wait time.Duration }

func (e *heldError) Error() string { return fmt.Sprintf("notify: delivery held back for %s", e.wait) }

var (
	errReservationLost = errors.New("notify: a later reservation owns this delivery")
	// errNotStarted is a send that the run's end reached before it began.
	errNotStarted = errors.New("notify: the run ended before the send began")
)

// attempt sends the row. Right before the send it asks the hold function,
// and checks that the reservation is still current, so a row cancelled or
// taken over while it waited for a worker is not sent. An email row is
// instead marked attempting, durably and under the same token check, so that
// a crash during SMTP leaves it unknown instead of sending it again.
func (d *Drainer) attempt(ctx context.Context, log *slog.Logger, dest *models.NotificationDestination, sender Sender, row *models.NotificationDelivery, msg Message) error {
	if ctx.Err() != nil {
		return errNotStarted
	}
	if d.hold != nil {
		wait, err := d.hold(ctx, row)
		if err != nil {
			log.Error("failed to check whether the delivery is held back; it is tried again later", "error", err)
			return &heldError{wait: holdCheckRetry}
		}
		if wait > 0 {
			log.Debug("holding back a delivery", "wait", max(wait, minHold))
			return &heldError{wait: max(wait, minHold)}
		}
	}
	if dest.Type != models.DestinationTypeEmail {
		current, err := d.db.GetDelivery(ctx, row.ID)
		if err != nil {
			log.Error("failed to check the delivery's reservation; it is tried again later", "error", err)
			return &heldError{wait: holdCheckRetry}
		}
		if current.Status != models.DeliveryPending || current.ReservedToken != row.ReservedToken {
			log.Warn("the delivery was cancelled or taken over while it waited; sending nothing", "status", current.Status)
			return errReservationLost
		}
	}
	if dest.Type == models.DestinationTypeEmail {
		var marked bool
		var err error
		d.write(ctx, func(ctx context.Context) { marked, err = d.db.MarkDeliveryAttempting(ctx, row.ID, row.ReservedToken) })
		if err != nil {
			// Nothing was sent; the row is tried again when its lease ends.
			log.Error("failed to mark an email delivery attempting", "error", err)
			return errReservationLost
		}
		if !marked {
			log.Warn("a later reservation owns this email delivery; sending nothing")
			return errReservationLost
		}
	}
	sendCtx, cancel := context.WithTimeout(ctx, d.sendTimeout)
	defer cancel()
	return sender.Send(sendCtx, dest, row.Recipient, row.ID, msg)
}

// release hands an unsent row back, due again after the given wait, with the
// attempt its reservation counted, so a cancelled run or a hold spends none
// of its retries.
func (d *Drainer) release(ctx context.Context, log *slog.Logger, row *models.NotificationDelivery, after time.Duration) {
	var err error
	d.write(ctx, func(ctx context.Context) { _, err = d.db.ReleaseDelivery(ctx, row.ID, row.ReservedToken, after) })
	if err != nil {
		log.Error("failed to release an unsent delivery; it is tried again when its reservation ends", "error", err)
	}
}

// outcome classifies a send's error after the row's attempts-th attempt.
func outcome(attempts int, err error) models.DeliveryOutcome {
	switch {
	case err == nil:
		return models.DeliveryOutcome{Status: models.DeliverySent}
	case errors.Is(err, ErrOutcomeUnknown):
		return models.DeliveryOutcome{Status: models.DeliveryUnknown, Error: BoundedError(err)}
	case IsPermanent(err), attempts >= maxAttempts:
		return models.DeliveryOutcome{Status: models.DeliveryFailed, Error: BoundedError(err)}
	}
	delay := retryDelays[max(attempts, 1)-1]
	if r, ok := errors.AsType[*retryAfterError](err); ok {
		delay = r.after
	}
	return models.DeliveryOutcome{Status: models.DeliveryPending, RetryAfter: delay, Error: BoundedError(err)}
}

func (d *Drainer) finish(ctx context.Context, log *slog.Logger, dest *models.NotificationDestination, row *models.NotificationDelivery, out models.DeliveryOutcome) {
	var finished bool
	var err error
	d.write(ctx, func(ctx context.Context) { finished, err = d.db.FinishDelivery(ctx, row.ID, row.ReservedToken, out) })
	result := string(out.Status)
	if out.Status == models.DeliveryPending {
		result = "retry"
	}
	switch {
	case err != nil:
		log.Error("failed to record a delivery outcome; the row is tried again when its reservation ends", "status", out.Status, "error", err)
		return
	case !finished:
		log.Warn("a later reservation owns this delivery; its outcome was dropped", "status", out.Status)
		metrics.RecordNotificationDelivery(string(dest.Type), "reservation_lost")
		return
	}
	metrics.RecordNotificationDelivery(string(dest.Type), result)
	if out.Status != models.DeliverySent {
		log.Warn("notification delivery did not succeed", "status", out.Status, "attempts", row.Attempts, "error", out.Error)
	}
	status := out.Status
	if status == models.DeliveryPending {
		status = models.DeliveryFailed
	}
	d.write(ctx, func(ctx context.Context) { err = d.db.RecordDestinationDelivery(ctx, dest.ID, status, out.Error) })
	if err != nil {
		log.Warn("failed to record the destination's last delivery", "error", err)
	}
}

// write runs one outcome write with its own deadline: the write outlives a
// run that its deadline or a shutdown cancelled.
func (d *Drainer) write(ctx context.Context, fn func(ctx context.Context)) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
	defer cancel()
	fn(ctx)
}

// destinationCache reads each destination once per run.
type destinationCache struct {
	db   store.DestinationStore
	mu   sync.Mutex
	byID map[models.DestinationID]*models.NotificationDestination
}

func newDestinationCache(db store.DestinationStore) *destinationCache {
	return &destinationCache{db: db, byID: make(map[models.DestinationID]*models.NotificationDestination)}
}

func (c *destinationCache) get(ctx context.Context, id models.DestinationID) (*models.NotificationDestination, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d, ok := c.byID[id]; ok {
		return d, nil
	}
	d, err := c.db.GetNotificationDestination(ctx, id)
	if err != nil {
		return nil, err
	}
	c.byID[id] = d
	return d, nil
}
