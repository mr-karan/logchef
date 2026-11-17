package storetest

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/notify"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// RunDeliveries exercises the notification delivery queue (RFC 0006 unit D2)
// against one database. open must return a new store on the same database
// each time, like a separate replica.
func RunDeliveries(t *testing.T, open Opener) {
	t.Run("EnqueueIsIdempotent", func(t *testing.T) { testEnqueueIdempotent(t, open(t)) })
	t.Run("ResolvedWaitsForFiring", func(t *testing.T) { testDeliveryOrdering(t, open(t), open(t)) })
	t.Run("StaleReservationChangesNothing", func(t *testing.T) { testStaleReservation(t, open(t), open(t)) })
	t.Run("ExpireEmailAttempts", func(t *testing.T) { testExpireEmailAttempts(t, open(t)) })
	t.Run("ReleaseUnsent", func(t *testing.T) { testReleaseUnsent(t, open(t)) })
	t.Run("Summaries", func(t *testing.T) { testDeliverySummaries(t, open(t)) })
	t.Run("Prune", func(t *testing.T) { testPruneDeliveries(t, open(t)) })
	t.Run("TwoDrainsSendEachRowOnce", func(t *testing.T) { testTwoDrains(t, open(t), open(t)) })
}

// deliveryHookURL is a placeholder receiver; the tests never send to it.
const deliveryHookURL = "https://hooks.example.com/x"

func deliveryDestination(t *testing.T, s store.Store) models.DestinationID {
	t.Helper()
	d := &models.NotificationDestination{Name: "hook", Type: models.DestinationTypeWebhook, Scope: models.DestinationScopeGlobal,
		Config: models.DestinationConfig{PayloadVersion: 1}, Secret: deliveryHookURL, IsActive: true}
	if err := s.CreateNotificationDestination(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	return d.ID
}

func enqueue(t *testing.T, s store.Store, deliveries ...notify.Delivery) {
	t.Helper()
	ctx := context.Background()
	if err := s.WithTx(ctx, func(tx store.StoreOps) error { return notify.Enqueue(ctx, tx, deliveries) }); err != nil {
		t.Fatal(err)
	}
}

// delivery is one row of sequence seq announcing event.
func delivery(dest models.DestinationID, seq string, event notify.Event) notify.Delivery {
	return notify.Delivery{
		DestinationID: dest, Producer: "test", ProducerRef: seq, SequenceKey: seq,
		DedupKey: seq + ":" + string(event), Message: notify.Message{Event: event, Title: seq},
	}
}

func reserve(t *testing.T, s store.DeliveryStore, owner string, lease time.Duration) []*models.NotificationDelivery {
	t.Helper()
	rows, err := s.ReserveDeliveries(context.Background(), owner, lease, 50)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func mustGetDelivery(t *testing.T, s store.DeliveryStore, id int64) *models.NotificationDelivery {
	t.Helper()
	row, err := s.GetDelivery(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func testEnqueueIdempotent(t *testing.T, s store.Store) {
	dest := deliveryDestination(t, s)
	row := delivery(dest, "a", notify.EventFiring)
	enqueue(t, s, row)
	row.Message.Title = "changed"
	enqueue(t, s, row)
	rows := reserve(t, s, "owner", time.Minute)
	if len(rows) != 1 || rows[0].Status != models.DeliveryPending || rows[0].Attempts != 1 || rows[0].Event != "firing" {
		t.Fatalf("rows after two enqueues of one dedup key = %+v, want one reserved row", rows)
	}
	if want := `{"event":"firing","title":"a","text":""}`; rows[0].MessageJSON != want {
		t.Errorf("message = %s, want the first enqueue's", rows[0].MessageJSON)
	}
}

// testDeliveryOrdering: a resolved row is never reserved while its firing row
// is pending, reserved or not, by either replica.
func testDeliveryOrdering(t *testing.T, a, b store.Store) {
	ctx := context.Background()
	dest := deliveryDestination(t, a)
	enqueue(t, a, delivery(dest, "group", notify.EventFiring), delivery(dest, "group", notify.EventResolved), delivery(dest, "other", notify.EventFiring))
	first := reserve(t, a, "a", time.Minute)
	if len(first) != 2 || first[0].Event != "firing" || first[0].SequenceKey != "group" || first[1].SequenceKey != "other" {
		t.Fatalf("first reservation = %+v, want both firing rows and not the resolved", first)
	}
	if rows := reserve(t, b, "b", time.Minute); len(rows) != 0 {
		t.Fatalf("second replica reserved %+v while the firing row is reserved", rows)
	}
	// A retry keeps the firing row pending, so the resolved still waits.
	if ok, err := a.FinishDelivery(ctx, first[0].ID, first[0].ReservedToken, models.DeliveryOutcome{Status: models.DeliveryPending, Error: "status 500"}); err != nil || !ok {
		t.Fatalf("FinishDelivery = %v, %v", ok, err)
	}
	if rows := reserve(t, b, "b", time.Minute); len(rows) != 1 || rows[0].ID != first[0].ID {
		t.Fatalf("after the firing retry, reserved %+v, want the firing row again and not the resolved", rows)
	}
	again := mustGetDelivery(t, b, first[0].ID)
	if ok, err := b.FinishDelivery(ctx, again.ID, again.ReservedToken, models.DeliveryOutcome{Status: models.DeliverySent}); err != nil || !ok {
		t.Fatalf("FinishDelivery = %v, %v", ok, err)
	}
	if rows := reserve(t, a, "a", time.Minute); len(rows) != 1 || rows[0].Event != "resolved" {
		t.Fatalf("after the firing was sent, reserved %+v, want the resolved", rows)
	}
}

// testStaleReservation: a drain whose reservation ended writes its result and
// changes nothing; the newer drain's retry state stands.
func testStaleReservation(t *testing.T, a, b store.Store) {
	ctx := context.Background()
	dest := deliveryDestination(t, a)
	enqueue(t, a, delivery(dest, "row", notify.EventFiring))
	stale := reserve(t, a, "a", 50*time.Millisecond)
	if len(stale) != 1 {
		t.Fatalf("reserved %d rows, want 1", len(stale))
	}
	if rows := reserve(t, b, "b", time.Minute); len(rows) != 0 {
		t.Fatalf("reserved a row whose reservation is current: %+v", rows)
	}
	waitLeaseExpiry()
	current := reserve(t, b, "b", time.Minute)
	if len(current) != 1 || current[0].ReservedToken != stale[0].ReservedToken+1 || current[0].Attempts != 2 {
		t.Fatalf("takeover reservation = %+v, want the row with a raised token and two attempts", current)
	}
	if ok, err := b.FinishDelivery(ctx, current[0].ID, current[0].ReservedToken, models.DeliveryOutcome{Status: models.DeliveryPending, RetryAfter: 10 * time.Minute, Error: "newer"}); err != nil || !ok {
		t.Fatalf("newer FinishDelivery = %v, %v", ok, err)
	}
	if ok, err := a.FinishDelivery(ctx, stale[0].ID, stale[0].ReservedToken, models.DeliveryOutcome{Status: models.DeliverySent}); err != nil || ok {
		t.Fatalf("stale FinishDelivery = %v, %v; want no row written", ok, err)
	}
	if ok, err := a.MarkDeliveryAttempting(ctx, stale[0].ID, stale[0].ReservedToken); err != nil || ok {
		t.Fatalf("stale MarkDeliveryAttempting = %v, %v; want no row written", ok, err)
	}
	row := mustGetDelivery(t, a, stale[0].ID)
	if row.Status != models.DeliveryPending || row.LastError != "newer" || row.SentAt != nil || time.Until(row.NextAttemptAt) < 9*time.Minute {
		t.Errorf("row after the stale write = %+v, want the newer drain's retry state", row)
	}
}

func testExpireEmailAttempts(t *testing.T, s store.Store) {
	ctx := context.Background()
	dest := deliveryDestination(t, s)
	enqueue(t, s, delivery(dest, "mail", notify.EventFiring), delivery(dest, "mail", notify.EventResolved))
	rows := reserve(t, s, "a", 50*time.Millisecond)
	if ok, err := s.MarkDeliveryAttempting(ctx, rows[0].ID, rows[0].ReservedToken); err != nil || !ok {
		t.Fatalf("MarkDeliveryAttempting = %v, %v", ok, err)
	}
	if n, err := s.ExpireEmailAttempts(ctx, "interrupted"); err != nil || n != 0 {
		t.Fatalf("ExpireEmailAttempts inside the reservation = %d, %v; want 0", n, err)
	}
	// The resolved row waits while its firing row is attempting.
	waitLeaseExpiry()
	if got := reserve(t, s, "b", time.Minute); len(got) != 0 {
		t.Fatalf("reserved %+v while the firing row is attempting", got)
	}
	if n, err := s.ExpireEmailAttempts(ctx, "interrupted"); err != nil || n != 1 {
		t.Fatalf("ExpireEmailAttempts = %d, %v; want 1", n, err)
	}
	row := mustGetDelivery(t, s, rows[0].ID)
	if row.Status != models.DeliveryUnknown || row.LastError != "interrupted" || row.ReservedToken != rows[0].ReservedToken+1 {
		t.Errorf("expired row = %+v, want unknown with a raised token", row)
	}
	if ok, err := s.FinishDelivery(ctx, rows[0].ID, rows[0].ReservedToken, models.DeliveryOutcome{Status: models.DeliverySent}); err != nil || ok {
		t.Errorf("the stalled worker's FinishDelivery = %v, %v; want no row written", ok, err)
	}
	if got := reserve(t, s, "b", time.Minute); len(got) != 1 || got[0].Event != "resolved" {
		t.Errorf("after the firing became unknown, reserved %+v, want only the resolved", got)
	}
}

// sentAndRetrying enqueues rows for refs <alert>:10 and <alert>:11 and leaves
// the first sent and the second waiting for a retry.
func sentAndRetrying(t *testing.T, s store.Store, alert string) []*models.NotificationDelivery {
	t.Helper()
	ctx := context.Background()
	dest := deliveryDestination(t, s)
	enqueue(t, s, delivery(dest, alert+":10", notify.EventFiring), delivery(dest, alert+":11", notify.EventFiring))
	rows := reserve(t, s, "a", time.Minute)
	if len(rows) != 2 {
		t.Fatalf("reserved %d rows, want 2", len(rows))
	}
	if ok, err := s.FinishDelivery(ctx, rows[0].ID, rows[0].ReservedToken, models.DeliveryOutcome{Status: models.DeliverySent}); err != nil || !ok {
		t.Fatalf("FinishDelivery = %v, %v", ok, err)
	}
	if ok, err := s.FinishDelivery(ctx, rows[1].ID, rows[1].ReservedToken, models.DeliveryOutcome{Status: models.DeliveryPending, RetryAfter: time.Minute, Error: "status 503"}); err != nil || !ok {
		t.Fatalf("FinishDelivery = %v, %v", ok, err)
	}
	if err := s.RecordDestinationDelivery(ctx, dest, models.DeliverySent, ""); err != nil {
		t.Errorf("RecordDestinationDelivery: %v", err)
	}
	return rows
}

func testDeliverySummaries(t *testing.T, s store.Store) {
	ctx := context.Background()
	sentAndRetrying(t, s, "1")
	summaries, err := s.ListDeliverySummaries(ctx, "test", []string{"1:10", "1:11", "1:12"})
	if err != nil {
		t.Fatal(err)
	}
	sent, retry := summaries["1:10"], summaries["1:11"]
	if len(summaries) != 2 || len(sent) != 1 || len(retry) != 1 {
		t.Fatalf("summaries = %+v, want one row for each of the two refs", summaries)
	}
	if sent[0].Status != models.DeliverySent || sent[0].SentAt == nil || sent[0].DestinationName != "hook" || sent[0].DestinationType != models.DestinationTypeWebhook || sent[0].Attempts != 1 {
		t.Errorf("sent summary = %+v", sent[0])
	}
	if retry[0].Status != models.DeliveryPending || retry[0].LastError != "status 503" || retry[0].SentAt != nil {
		t.Errorf("retry summary = %+v", retry[0])
	}
	if got, err := s.ListDeliverySummaries(ctx, "test", nil); err != nil || len(got) != 0 {
		t.Errorf("summaries of no refs = %+v, %v", got, err)
	}
}

func testPruneDeliveries(t *testing.T, s store.Store) {
	ctx := context.Background()
	rows := sentAndRetrying(t, s, "2")
	time.Sleep(20 * time.Millisecond)
	// Earlier subtests share the database, so only this test's rows are
	// checked.
	if _, err := s.PruneDeliveries(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetDelivery(ctx, rows[0].ID); err != nil {
		t.Fatalf("a sent row younger than the age was pruned: %v", err)
	}
	if n, err := s.PruneDeliveries(ctx, 0); err != nil || n < 1 {
		t.Fatalf("PruneDeliveries = %d, %v; want the sent row deleted", n, err)
	}
	if _, err := s.GetDelivery(ctx, rows[0].ID); !models.IsNotFound(err) {
		t.Errorf("sent row after pruning: %v, want not found", err)
	}
	if _, err := s.GetDelivery(ctx, rows[1].ID); err != nil {
		t.Errorf("pending row pruned: %v", err)
	}
}

// recordingSender counts the sends of each delivery and keeps their order.
type recordingSender struct {
	mu    sync.Mutex
	sends map[int64]int
	order []string
}

func (r *recordingSender) Send(_ context.Context, _ *models.NotificationDestination, _ string, deliveryID int64, msg notify.Message) error {
	time.Sleep(time.Millisecond)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sends[deliveryID]++
	r.order = append(r.order, msg.Title+":"+string(msg.Event))
	return nil
}

// testTwoDrains: two replicas drain one queue at once; every row reaches the
// receiver once, and each group's resolved follows its firing.
func testTwoDrains(t *testing.T, a, b store.Store) {
	dest := deliveryDestination(t, a)
	const groups = 60
	var rows []notify.Delivery
	for i := range groups {
		seq := fmt.Sprintf("group-%d", i)
		rows = append(rows, delivery(dest, seq, notify.EventFiring), delivery(dest, seq, notify.EventResolved))
	}
	enqueue(t, a, rows...)

	rec := &recordingSender{sends: map[int64]int{}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var wg sync.WaitGroup
	for _, s := range []store.Store{a, b} {
		drainer := notify.NewDrainer(notify.DrainerOptions{Store: s, Logger: logger, Senders: map[models.DestinationType]notify.Sender{models.DestinationTypeWebhook: rec}})
		wg.Go(func() {
			// A drain ends when it finds nothing to reserve, which can happen
			// while the other replica holds the rest, so each drains a few
			// times.
			for range 5 {
				if err := drainer.Drain(context.Background()); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	final := notify.NewDrainer(notify.DrainerOptions{Store: a, Logger: logger, Senders: map[models.DestinationType]notify.Sender{models.DestinationTypeWebhook: rec}})
	if err := final.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.sends) != 2*groups {
		t.Fatalf("%d rows were sent, want %d", len(rec.sends), 2*groups)
	}
	for id, n := range rec.sends {
		if n != 1 {
			t.Errorf("delivery %d reached the receiver %d times", id, n)
		}
	}
	seen := map[string]bool{}
	for _, entry := range rec.order {
		i := strings.LastIndex(entry, ":")
		seq, event := entry[:i], entry[i+1:]
		if event == "resolved" && !seen[seq] {
			t.Errorf("%s resolved went out before its firing", seq)
		}
		seen[seq] = true
	}
}

// testReleaseUnsent: a released row is free to reserve at once and has back
// the attempt its reservation counted; a stale token releases nothing.
func testReleaseUnsent(t *testing.T, s store.Store) {
	ctx := context.Background()
	dest := deliveryDestination(t, s)
	enqueue(t, s, delivery(dest, "release", notify.EventFiring))
	rows := reserve(t, s, "a", time.Minute)
	if len(rows) != 1 || rows[0].Attempts != 1 {
		t.Fatalf("reserved %+v, want one row with one attempt", rows)
	}
	if ok, err := s.ReleaseDelivery(ctx, rows[0].ID, rows[0].ReservedToken); err != nil || !ok {
		t.Fatalf("ReleaseDelivery = %v, %v", ok, err)
	}
	again := reserve(t, s, "b", time.Minute)
	if len(again) != 1 || again[0].Attempts != 1 {
		t.Fatalf("after the release, reserved %+v, want the row at once with one attempt", again)
	}
	if ok, err := s.ReleaseDelivery(ctx, rows[0].ID, rows[0].ReservedToken); err != nil || ok {
		t.Errorf("a stale ReleaseDelivery = %v, %v; want no row written", ok, err)
	}
}
