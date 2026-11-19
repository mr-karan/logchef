package storetest

import (
	"context"
	"encoding/json"
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
// each time, like a separate replica. The subtests share the queue, so a row
// a subtest leaves reserved or waiting to retry stays so for an hour, past
// any test timeout, and no later subtest reserves it.
func RunDeliveries(t *testing.T, open Opener, hooks LeaseHooks) {
	t.Run("EnqueueIsIdempotent", func(t *testing.T) { testEnqueueIdempotent(t, open(t)) })
	t.Run("ResolvedWaitsForFiring", func(t *testing.T) { testDeliveryOrdering(t, open(t), open(t)) })
	t.Run("StaleReservationChangesNothing", func(t *testing.T) { testStaleReservation(t, open(t), open(t), hooks) })
	t.Run("ExpireEmailAttempts", func(t *testing.T) { testExpireEmailAttempts(t, open(t), hooks) })
	t.Run("ReleaseUnsent", func(t *testing.T) { testReleaseUnsent(t, open(t)) })
	t.Run("ReleaseWithDelay", func(t *testing.T) { testReleaseWithDelay(t, open(t)) })
	t.Run("Cancel", func(t *testing.T) { testCancelDeliveries(t, open(t), open(t)) })
	t.Run("Summaries", func(t *testing.T) { testDeliverySummaries(t, open(t)) })
	t.Run("Prune", func(t *testing.T) { testPruneDeliveries(t, open(t)) })
	t.Run("OverLimitMessageStartsFailed", func(t *testing.T) { testOverLimitMessage(t, open(t)) })
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

// reserve takes every due row for heldLease.
func reserve(t *testing.T, s store.DeliveryStore, owner string) []*models.NotificationDelivery {
	t.Helper()
	rows, err := s.ReserveDeliveries(context.Background(), owner, heldLease, 50)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func expireReservation(t *testing.T, hooks LeaseHooks, id int64) {
	t.Helper()
	if err := hooks.ExpireDeliveryReservation(context.Background(), id); err != nil {
		t.Fatalf("expire the reservation of delivery %d: %v", id, err)
	}
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
	rows := reserve(t, s, "owner")
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
	first := reserve(t, a, "a")
	if len(first) != 2 || first[0].Event != "firing" || first[0].SequenceKey != "group" || first[1].SequenceKey != "other" {
		t.Fatalf("first reservation = %+v, want both firing rows and not the resolved", first)
	}
	if rows := reserve(t, b, "b"); len(rows) != 0 {
		t.Fatalf("second replica reserved %+v while the firing row is reserved", rows)
	}
	// A retry keeps the firing row pending, so the resolved still waits.
	if ok, err := a.FinishDelivery(ctx, first[0].ID, first[0].ReservedToken, models.DeliveryOutcome{Status: models.DeliveryPending, Error: "status 500"}); err != nil || !ok {
		t.Fatalf("FinishDelivery = %v, %v", ok, err)
	}
	if rows := reserve(t, b, "b"); len(rows) != 1 || rows[0].ID != first[0].ID {
		t.Fatalf("after the firing retry, reserved %+v, want the firing row again and not the resolved", rows)
	}
	again := mustGetDelivery(t, b, first[0].ID)
	if ok, err := b.FinishDelivery(ctx, again.ID, again.ReservedToken, models.DeliveryOutcome{Status: models.DeliverySent}); err != nil || !ok {
		t.Fatalf("FinishDelivery = %v, %v", ok, err)
	}
	if rows := reserve(t, a, "a"); len(rows) != 1 || rows[0].Event != "resolved" {
		t.Fatalf("after the firing was sent, reserved %+v, want the resolved", rows)
	}
}

// testStaleReservation: a drain whose reservation ended writes its result and
// changes nothing; the newer drain's retry state stands.
func testStaleReservation(t *testing.T, a, b store.Store, hooks LeaseHooks) {
	ctx := context.Background()
	dest := deliveryDestination(t, a)
	enqueue(t, a, delivery(dest, "row", notify.EventFiring))
	stale := reserve(t, a, "a")
	if len(stale) != 1 {
		t.Fatalf("reserved %d rows, want 1", len(stale))
	}
	if rows := reserve(t, b, "b"); len(rows) != 0 {
		t.Fatalf("reserved a row whose reservation is current: %+v", rows)
	}
	expireReservation(t, hooks, stale[0].ID)
	current := reserve(t, b, "b")
	if len(current) != 1 || current[0].ReservedToken != stale[0].ReservedToken+1 || current[0].Attempts != 2 {
		t.Fatalf("takeover reservation = %+v, want the row with a raised token and two attempts", current)
	}
	if ok, err := b.FinishDelivery(ctx, current[0].ID, current[0].ReservedToken, models.DeliveryOutcome{Status: models.DeliveryPending, RetryAfter: time.Hour, Error: "newer"}); err != nil || !ok {
		t.Fatalf("newer FinishDelivery = %v, %v", ok, err)
	}
	if ok, err := a.FinishDelivery(ctx, stale[0].ID, stale[0].ReservedToken, models.DeliveryOutcome{Status: models.DeliverySent}); err != nil || ok {
		t.Fatalf("stale FinishDelivery = %v, %v; want no row written", ok, err)
	}
	if ok, err := a.MarkDeliveryAttempting(ctx, stale[0].ID, stale[0].ReservedToken); err != nil || ok {
		t.Fatalf("stale MarkDeliveryAttempting = %v, %v; want no row written", ok, err)
	}
	row := mustGetDelivery(t, a, stale[0].ID)
	if row.Status != models.DeliveryPending || row.LastError != "newer" || row.SentAt != nil || time.Until(row.NextAttemptAt) < 50*time.Minute {
		t.Errorf("row after the stale write = %+v, want the newer drain's retry state", row)
	}
}

func testExpireEmailAttempts(t *testing.T, s store.Store, hooks LeaseHooks) {
	ctx := context.Background()
	dest := deliveryDestination(t, s)
	enqueue(t, s, delivery(dest, "mail", notify.EventFiring), delivery(dest, "mail", notify.EventResolved))
	rows := reserve(t, s, "a")
	if ok, err := s.MarkDeliveryAttempting(ctx, rows[0].ID, rows[0].ReservedToken); err != nil || !ok {
		t.Fatalf("MarkDeliveryAttempting = %v, %v", ok, err)
	}
	if n, err := s.ExpireEmailAttempts(ctx, "interrupted"); err != nil || n != 0 {
		t.Fatalf("ExpireEmailAttempts inside the reservation = %d, %v; want 0", n, err)
	}
	// The resolved row waits while its firing row is attempting.
	expireReservation(t, hooks, rows[0].ID)
	if got := reserve(t, s, "b"); len(got) != 0 {
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
	if got := reserve(t, s, "b"); len(got) != 1 || got[0].Event != "resolved" {
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
	rows := reserve(t, s, "a")
	if len(rows) != 2 {
		t.Fatalf("reserved %d rows, want 2", len(rows))
	}
	if ok, err := s.FinishDelivery(ctx, rows[0].ID, rows[0].ReservedToken, models.DeliveryOutcome{Status: models.DeliverySent}); err != nil || !ok {
		t.Fatalf("FinishDelivery = %v, %v", ok, err)
	}
	if ok, err := s.FinishDelivery(ctx, rows[1].ID, rows[1].ReservedToken, models.DeliveryOutcome{Status: models.DeliveryPending, RetryAfter: time.Hour, Error: "status 503"}); err != nil || !ok {
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
	rows := reserve(t, s, "a")
	if len(rows) != 1 || rows[0].Attempts != 1 {
		t.Fatalf("reserved %+v, want one row with one attempt", rows)
	}
	if ok, err := s.ReleaseDelivery(ctx, rows[0].ID, rows[0].ReservedToken, 0); err != nil || !ok {
		t.Fatalf("ReleaseDelivery = %v, %v", ok, err)
	}
	again := reserve(t, s, "b")
	if len(again) != 1 || again[0].Attempts != 1 {
		t.Fatalf("after the release, reserved %+v, want the row at once with one attempt", again)
	}
	if ok, err := s.ReleaseDelivery(ctx, rows[0].ID, rows[0].ReservedToken, 0); err != nil || ok {
		t.Errorf("a stale ReleaseDelivery = %v, %v; want no row written", ok, err)
	}
}

// testReleaseWithDelay: a row released with a delay keeps its attempts, is
// not due before the delay ends, and holds back the later rows of its
// sequence meanwhile.
func testReleaseWithDelay(t *testing.T, s store.Store) {
	ctx := context.Background()
	dest := deliveryDestination(t, s)
	enqueue(t, s, delivery(dest, "held", notify.EventFiring), delivery(dest, "held", notify.EventResolved))
	var firing *models.NotificationDelivery
	for _, row := range reserve(t, s, "a") {
		if row.SequenceKey != "held" {
			continue
		}
		if firing != nil || row.Event != string(notify.EventFiring) {
			t.Fatalf("reserved %s of sequence held, want the firing alone", row.Event)
		}
		firing = row
	}
	if firing == nil {
		t.Fatal("the firing was not reserved")
	}
	if ok, err := s.ReleaseDelivery(ctx, firing.ID, firing.ReservedToken, time.Hour); err != nil || !ok {
		t.Fatalf("ReleaseDelivery = %v, %v", ok, err)
	}
	for _, row := range reserve(t, s, "b") {
		if row.SequenceKey == "held" {
			t.Fatalf("during the delay, reserved the %s, want nothing of sequence held: the firing waits and its resolved behind it", row.Event)
		}
	}
	row := mustGetDelivery(t, s, firing.ID)
	if row.Status != models.DeliveryPending || row.Attempts != 0 || row.NextAttemptAt.Before(time.Now().Add(50*time.Minute)) {
		t.Errorf("held row = status %s, attempts %d, next attempt %s; want pending, 0, about an hour from now", row.Status, row.Attempts, row.NextAttemptAt)
	}
}

// testCancelDeliveries: cancelling a producer ref ends its pending rows as
// failed, even a reserved one whose drain then cannot write its outcome,
// and leaves sent rows and other refs alone.
func testCancelDeliveries(t *testing.T, a, b store.Store) {
	ctx := context.Background()
	dest := deliveryDestination(t, a)
	// The database is shared with the other tests, so rows are picked by
	// sequence key.
	pick := func(rows []*models.NotificationDelivery, seq string) *models.NotificationDelivery {
		t.Helper()
		for _, row := range rows {
			if row.SequenceKey == seq {
				return row
			}
		}
		t.Fatalf("reserved %d rows, none of sequence %q", len(rows), seq)
		return nil
	}
	sent := delivery(dest, "cancel-sent", notify.EventFiring)
	sent.ProducerRef = "cancel-episode"
	enqueue(t, a, sent)
	first := pick(reserve(t, a, "a"), "cancel-sent")
	if ok, err := a.FinishDelivery(ctx, first.ID, first.ReservedToken, models.DeliveryOutcome{Status: models.DeliverySent}); err != nil || !ok {
		t.Fatalf("FinishDelivery = %v, %v", ok, err)
	}
	enqueue(t, a, delivery(dest, "cancel-episode", notify.EventFiring), delivery(dest, "cancel-other", notify.EventFiring))
	rows := reserve(t, a, "a")
	reserved, other := pick(rows, "cancel-episode"), pick(rows, "cancel-other")
	n, err := b.CancelDeliveries(ctx, "test", "cancel-episode", "cancelled")
	if err != nil || n != 1 {
		t.Fatalf("CancelDeliveries = %d, %v; want the one pending row of the ref", n, err)
	}
	if ok, err := a.FinishDelivery(ctx, reserved.ID, reserved.ReservedToken, models.DeliveryOutcome{Status: models.DeliverySent}); err != nil || ok {
		t.Errorf("FinishDelivery after the cancel = %v, %v; want no row written", ok, err)
	}
	for _, want := range []struct {
		id     int64
		status models.DeliveryStatus
	}{{first.ID, models.DeliverySent}, {reserved.ID, models.DeliveryFailed}, {other.ID, models.DeliveryPending}} {
		row := mustGetDelivery(t, a, want.id)
		if row.Status != want.status {
			t.Errorf("row %d (%s) status = %s, want %s", row.ID, row.SequenceKey, row.Status, want.status)
		}
	}
	if row := mustGetDelivery(t, a, reserved.ID); row.LastError != "cancelled" {
		t.Errorf("cancelled row last_error = %q, want %q", row.LastError, "cancelled")
	}
}

// testOverLimitMessage: a message over 256 KiB is stored failed, without an
// attempt, and a message of exactly 256 KiB waits as usual. Only pending rows
// count as pending. Other subtests share the table, so counts are relative.
func testOverLimitMessage(t *testing.T, s store.Store) {
	ctx := context.Background()
	dest := deliveryDestination(t, s)
	const limit = 256 << 10 // RFC 0006, "Limits": delivery message
	at, over := delivery(dest, "at", notify.EventFiring), delivery(dest, "over", notify.EventFiring)
	at.Message.Title = titleForSize(t, at.Message, limit)
	over.Message.Title = titleForSize(t, over.Message, limit+1)
	before := countPending(t, s)
	enqueue(t, s, at, over)

	if n := countPending(t, s); n != before+1 {
		t.Fatalf("pending = %d, want %d", n, before+1)
	}
	summaries, err := s.ListDeliverySummaries(ctx, "test", []string{"at", "over"})
	if err != nil {
		t.Fatal(err)
	}
	if waiting := summaries["at"]; len(waiting) != 1 || waiting[0].Status != models.DeliveryPending {
		t.Errorf("summary at the limit = %+v, want one pending row", waiting)
	}
	failed := summaries["over"]
	if len(failed) != 1 || failed[0].Status != models.DeliveryFailed || failed[0].Attempts != 0 ||
		failed[0].LastError != "not sent: the message is 262145 bytes, over the 262144-byte limit" {
		t.Errorf("summary over the limit = %+v, want one failed row naming the limit", failed)
	}
	if _, err := s.CancelDeliveries(ctx, "test", "at", "test done"); err != nil {
		t.Fatal(err)
	}
	if n := countPending(t, s); n != before {
		t.Fatalf("pending after the cancel = %d, want %d", n, before)
	}
}

func countPending(t *testing.T, s store.DeliveryStore) int64 {
	t.Helper()
	n, err := s.CountPendingDeliveries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// titleForSize returns a title that makes msg encode to exactly size bytes.
func titleForSize(t *testing.T, msg notify.Message, size int) string {
	t.Helper()
	msg.Title = ""
	body, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Repeat("x", size-len(body))
}
