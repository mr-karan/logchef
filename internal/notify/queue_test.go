package notify

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/internal/store/sqlite/sqlitetest"
	"github.com/mr-karan/logchef/pkg/models"
)

// openStore is a fresh SQLite store.
func openStore(t *testing.T) *sqlite.DB {
	db, _ := openStoreRaw(t)
	return db
}

// openStoreRaw is a fresh SQLite store and a raw handle on its file, for
// moving rows in time.
func openStoreRaw(t *testing.T) (db *sqlite.DB, raw *sql.DB) {
	t.Helper()
	path := sqlitetest.Path(t)
	db, err := sqlite.New(context.Background(), sqlite.Options{Logger: discardLogger(), Config: config.SQLiteConfig{Path: path}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	raw, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	return db, raw
}

// queueEnv is a store with one destination and a drainer over real senders.
type queueEnv struct {
	db      *sqlite.DB
	raw     *sql.DB
	dest    *models.NotificationDestination
	drainer *Drainer
}

func newQueueEnv(t *testing.T, dest *models.NotificationDestination, senders map[models.DestinationType]Sender) *queueEnv {
	t.Helper()
	db, raw := openStoreRaw(t)
	if dest.Scope == "" {
		dest.Scope = models.DestinationScopeGlobal
	}
	if err := db.CreateNotificationDestination(t.Context(), dest); err != nil {
		t.Fatal(err)
	}
	return &queueEnv{db: db, raw: raw, dest: dest, drainer: NewDrainer(DrainerOptions{Store: db, Logger: discardLogger(), Senders: senders})}
}

// webhookEnv is a queue whose one destination is a webhook at url.
func webhookEnv(t *testing.T, url string) *queueEnv {
	t.Helper()
	allowlist, err := config.ParseWebhookAllowlist([]string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	sender := NewWebhookSender(WebhookSenderOptions{AllowedHosts: allowlist, Logger: discardLogger()})
	t.Cleanup(sender.client.CloseIdleConnections)
	dest := &models.NotificationDestination{Name: "hook", Type: models.DestinationTypeWebhook, Config: models.DestinationConfig{PayloadVersion: 1}, Secret: url, IsActive: true}
	return newQueueEnv(t, dest, map[models.DestinationType]Sender{models.DestinationTypeWebhook: sender})
}

// enqueue writes one row for each recipient ("" for a webhook), in order.
func (e *queueEnv) enqueue(t *testing.T, recipients ...string) {
	t.Helper()
	var rows []Delivery
	for i, recipient := range recipients {
		rows = append(rows, Delivery{
			DestinationID: e.dest.ID, Producer: "test", ProducerRef: "ref", Recipient: recipient,
			SequenceKey: fmt.Sprintf("seq:%d:%s", i, recipient), DedupKey: fmt.Sprintf("row-%d-%s", i, recipient), Message: testMessage,
		})
	}
	if err := e.db.WithTx(t.Context(), func(tx store.StoreOps) error { return Enqueue(t.Context(), tx, rows) }); err != nil {
		t.Fatal(err)
	}
}

func (e *queueEnv) drain(t *testing.T) {
	t.Helper()
	if err := e.drainer.Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// due makes every pending row due now.
func (e *queueEnv) due(t *testing.T) {
	t.Helper()
	if _, err := e.raw.Exec(`UPDATE notification_deliveries SET next_attempt_at = '2000-01-01T00:00:00.000Z' WHERE status = 'pending'`); err != nil {
		t.Fatal(err)
	}
}

func (e *queueEnv) row(t *testing.T, id int64) *models.NotificationDelivery {
	t.Helper()
	row, err := e.db.GetDelivery(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

// statusReceiver answers each request with the next status, then 204.
func statusReceiver(t *testing.T, statuses ...int) (*httptest.Server, *atomic.Int32, chan http.Header) {
	t.Helper()
	var calls atomic.Int32
	headers := make(chan http.Header, 16)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(calls.Add(1))
		headers <- r.Header.Clone()
		if n <= len(statuses) {
			w.WriteHeader(statuses[n-1])
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(receiver.Close)
	return receiver, &calls, headers
}

// TestDrainRetriesATransientFailure: a 500 then a 200 gives sent after one
// retry, and each request names its row.
func TestDrainRetriesATransientFailure(t *testing.T) {
	receiver, calls, headers := statusReceiver(t, http.StatusInternalServerError)
	e := webhookEnv(t, receiver.URL)
	e.enqueue(t, "")
	e.drain(t)
	first := e.row(t, 1)
	if first.Status != models.DeliveryPending || first.Attempts != 1 || !strings.Contains(first.LastError, "status 500") || time.Until(first.NextAttemptAt) < 20*time.Second {
		t.Fatalf("after a 500: %+v, want pending with the 30 second delay", first)
	}
	e.drain(t)
	if calls.Load() != 1 {
		t.Fatalf("a drain before the retry delay sent again: %d requests", calls.Load())
	}
	e.due(t)
	e.drain(t)
	row := e.row(t, 1)
	if row.Status != models.DeliverySent || row.Attempts != 2 || row.SentAt == nil || calls.Load() != 2 {
		t.Fatalf("after the retry: %+v and %d requests, want sent after two", row, calls.Load())
	}
	for range 2 {
		if id := (<-headers).Get("Logchef-Delivery-Id"); id != "1" {
			t.Errorf("Logchef-Delivery-Id = %q, want 1", id)
		}
	}
}

func TestDrainFailsAPermanentErrorAtOnce(t *testing.T) {
	receiver, calls, _ := statusReceiver(t, http.StatusBadRequest)
	e := webhookEnv(t, receiver.URL)
	e.enqueue(t, "")
	e.drain(t)
	e.due(t)
	e.drain(t)
	if row := e.row(t, 1); row.Status != models.DeliveryFailed || row.Attempts != 1 || !strings.Contains(row.LastError, "status 400") || calls.Load() != 1 {
		t.Fatalf("after a 400: %+v and %d requests, want failed after one", row, calls.Load())
	}
}

func TestDrainFailsAfterFiveAttempts(t *testing.T) {
	receiver, calls, _ := statusReceiver(t, 503, 503, 503, 503, 503, 503)
	e := webhookEnv(t, receiver.URL)
	e.enqueue(t, "")
	for range maxAttempts + 1 {
		e.due(t)
		e.drain(t)
	}
	if row := e.row(t, 1); row.Status != models.DeliveryFailed || row.Attempts != maxAttempts || calls.Load() != maxAttempts {
		t.Fatalf("after five 503s: %+v and %d requests, want failed after %d", row, calls.Load(), maxAttempts)
	}
}

func TestDrainHonorsRetryAfter(t *testing.T) {
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(receiver.Close)
	e := webhookEnv(t, receiver.URL)
	e.enqueue(t, "")
	e.drain(t)
	wait := time.Until(e.row(t, 1).NextAttemptAt)
	if wait < 9*time.Minute || wait > 10*time.Minute+time.Second {
		t.Fatalf("a 429 asking for an hour waits %v, want the 10 minute cap", wait)
	}
}

func TestDrainFailsAnInactiveDestination(t *testing.T) {
	receiver, calls, _ := statusReceiver(t)
	e := webhookEnv(t, receiver.URL)
	e.dest.IsActive = false
	if err := e.db.UpdateNotificationDestination(t.Context(), e.dest); err != nil {
		t.Fatal(err)
	}
	e.enqueue(t, "")
	e.drain(t)
	if row := e.row(t, 1); row.Status != models.DeliveryFailed || !strings.Contains(row.LastError, "inactive") || calls.Load() != 0 {
		t.Fatalf("inactive destination: %+v and %d requests, want failed and nothing sent", row, calls.Load())
	}
}

// TestWebhookCrashResendsOnce: a worker that dies after the receiver accepted
// a webhook and before its status write leaves the row reserved; once the
// reservation ends, the next drain sends it again. That is the documented
// at-least-once difference from email.
func TestWebhookCrashResendsOnce(t *testing.T) {
	receiver, calls, _ := statusReceiver(t)
	e := webhookEnv(t, receiver.URL)
	e.enqueue(t, "")
	rows, err := e.db.ReserveDeliveries(t.Context(), "dead", 50*time.Millisecond, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("reserve = %v, %v", rows, err)
	}
	if err := e.drainer.senders[models.DestinationTypeWebhook].Send(t.Context(), e.dest, "", rows[0].ID, testMessage); err != nil {
		t.Fatal(err)
	}
	e.drain(t)
	if calls.Load() != 1 {
		t.Fatalf("a drain inside the dead worker's reservation sent again: %d requests", calls.Load())
	}
	time.Sleep(100 * time.Millisecond)
	e.drain(t)
	if row := e.row(t, 1); row.Status != models.DeliverySent || calls.Load() != 2 {
		t.Fatalf("after the reservation ended: %+v and %d requests, want sent twice", row, calls.Load())
	}
}

// emailEnv is a queue whose one destination is an email destination, sent
// through server.
func emailEnv(t *testing.T, server *fakeSMTP) *queueEnv {
	t.Helper()
	dest := &models.NotificationDestination{Name: "mail", Type: models.DestinationTypeEmail, Config: models.DestinationConfig{Addresses: []string{"a@example.com", "b@example.com"}}, IsActive: true}
	e := newQueueEnv(t, dest, nil)
	opts := server.options()
	e.drainer.senders = map[models.DestinationType]Sender{models.DestinationTypeEmail: &EmailSender{users: e.db, mailer: func(context.Context) *Mailer { return NewMailer(opts) }}}
	return e
}

// TestEmailRetryReachesOnlyTheRefusedRecipient: the server accepts A and
// refuses B for now; the retry mails B only.
func TestEmailRetryReachesOnlyTheRefusedRecipient(t *testing.T) {
	server := startFakeSMTP(t)
	server.refuse["b@example.com"] = "450 mailbox busy"
	e := emailEnv(t, server)
	e.enqueue(t, "a@example.com", "b@example.com")
	e.drain(t)
	if a, b := e.row(t, 1), e.row(t, 2); a.Status != models.DeliverySent || b.Status != models.DeliveryPending {
		t.Fatalf("after the first drain: a %s, b %s %q; want sent and pending", a.Status, b.Status, b.LastError)
	}
	server.mu.Lock()
	delete(server.refuse, "b@example.com")
	server.mu.Unlock()
	e.due(t)
	e.drain(t)
	rcpts, received := server.snapshot()
	// The first drain's workers reach the server in either order.
	slices.Sort(rcpts)
	slices.Sort(received)
	if want := []string{"a@example.com", "b@example.com", "b@example.com"}; strings.Join(rcpts, ",") != strings.Join(want, ",") {
		t.Errorf("RCPT commands = %v, want %v", rcpts, want)
	}
	if want := []string{"a@example.com", "b@example.com"}; strings.Join(received, ",") != strings.Join(want, ",") {
		t.Errorf("messages = %v, want one each", received)
	}
	if b := e.row(t, 2); b.Status != models.DeliverySent || b.Attempts != 2 {
		t.Errorf("b after the retry = %+v, want sent on the second attempt", b)
	}
}

// TestEmailErrorAfterDataIsUnknown: a connection lost after the end of DATA
// may have delivered, so the row is unknown and never retried.
func TestEmailErrorAfterDataIsUnknown(t *testing.T) {
	server := startFakeSMTP(t)
	server.dropAfterData = true
	e := emailEnv(t, server)
	e.enqueue(t, "a@example.com")
	e.drain(t)
	e.due(t)
	e.drain(t)
	row := e.row(t, 1)
	if row.Status != models.DeliveryUnknown || row.Attempts != 1 {
		t.Fatalf("after a lost reply to DATA: %+v, want unknown after one attempt", row)
	}
	if _, received := server.snapshot(); len(received) != 1 {
		t.Errorf("server received %d messages, want 1", len(received))
	}
}

// TestEmailCrashIsUnknown: a worker accepted by the server and killed before
// its status write leaves the row attempting; once the reservation ends the
// next drain marks it unknown and the server has seen exactly one message.
func TestEmailCrashIsUnknown(t *testing.T) {
	server := startFakeSMTP(t)
	e := emailEnv(t, server)
	e.enqueue(t, "a@example.com")
	rows, err := e.db.ReserveDeliveries(t.Context(), "dead", 50*time.Millisecond, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("reserve = %v, %v", rows, err)
	}
	if ok, err := e.db.MarkDeliveryAttempting(t.Context(), rows[0].ID, rows[0].ReservedToken); err != nil || !ok {
		t.Fatalf("MarkDeliveryAttempting = %v, %v", ok, err)
	}
	if err := e.drainer.senders[models.DestinationTypeEmail].Send(t.Context(), e.dest, "a@example.com", rows[0].ID, testMessage); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	e.drain(t)
	e.due(t)
	e.drain(t)
	if row := e.row(t, 1); row.Status != models.DeliveryUnknown || row.LastError != errInterruptedSMTP {
		t.Fatalf("after the crash: %+v, want unknown", row)
	}
	if _, received := server.snapshot(); len(received) != 1 {
		t.Errorf("server received %d messages, want exactly 1", len(received))
	}
}

func TestEmailRecipientUserIsReadAtSendTime(t *testing.T) {
	server := startFakeSMTP(t)
	e := emailEnv(t, server)
	user := &models.User{Email: "first@example.com", FullName: "U", Role: models.UserRoleMember, Status: models.UserStatusActive}
	if err := e.db.CreateUser(t.Context(), user); err != nil {
		t.Fatal(err)
	}
	e.enqueue(t, UserRecipient(user.ID), UserRecipient(user.ID+100))
	user.Email = "moved@example.com"
	if err := e.db.UpdateUser(t.Context(), user); err != nil {
		t.Fatal(err)
	}
	e.drain(t)
	if _, received := server.snapshot(); strings.Join(received, ",") != "moved@example.com" {
		t.Errorf("messages = %v, want the user's current address", received)
	}
	if row := e.row(t, 2); row.Status != models.DeliveryFailed || !strings.Contains(row.LastError, "no longer exists") {
		t.Errorf("row of a missing user = %+v, want failed", row)
	}
}

// blockingSender holds every send until its context ends. It counts the
// sends of each row that began while the run was live, and the sends that a
// drain started after its run had ended.
type blockingSender struct {
	mu    sync.Mutex
	calls map[int64]int
	dead  int
}

func (b *blockingSender) Send(ctx context.Context, _ *models.NotificationDestination, _ string, deliveryID int64, _ Message) error {
	b.mu.Lock()
	if ctx.Err() != nil {
		b.dead++
	} else {
		b.calls[deliveryID]++
	}
	b.mu.Unlock()
	<-ctx.Done()
	return ctx.Err()
}

// TestCancelledDrainSpendsNoUnsentAttempt: a run that ends while rows wait
// for a worker hands them back unsent, so only rows a sender saw count an
// attempt, and none fails for want of attempts it never made.
func TestCancelledDrainSpendsNoUnsentAttempt(t *testing.T) {
	sender := &blockingSender{calls: map[int64]int{}}
	dest := &models.NotificationDestination{Name: "hook", Type: models.DestinationTypeWebhook, Config: models.DestinationConfig{PayloadVersion: 1}, Secret: "https://hooks.example.com/x", IsActive: true}
	e := newQueueEnv(t, dest, map[models.DestinationType]Sender{models.DestinationTypeWebhook: sender})
	recipients := make([]string, 20)
	e.enqueue(t, recipients...)
	for range maxAttempts + 1 {
		e.due(t)
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		_ = e.drainer.Drain(ctx)
		cancel()
	}
	sender.mu.Lock()
	defer sender.mu.Unlock()
	if sender.dead != 0 {
		t.Errorf("%d sends started after their run had ended", sender.dead)
	}
	for id := int64(1); id <= 20; id++ {
		row := e.row(t, id)
		if row.Attempts != sender.calls[id] {
			t.Errorf("row %d: %d attempts counted, %d sends made", id, row.Attempts, sender.calls[id])
		}
		if row.Status == models.DeliveryFailed && sender.calls[id] < maxAttempts {
			t.Errorf("row %d failed after %d sends", id, sender.calls[id])
		}
		if row.ReservedUntil != nil {
			t.Errorf("row %d is still reserved after the run ended", id)
		}
	}
}

// TestDrainHoldFailureSendsNothing: when the hold check fails, the row goes
// back unsent, with its attempt back, and is tried again after a wait.
func TestDrainHoldFailureSendsNothing(t *testing.T) {
	receiver, calls, _ := statusReceiver(t)
	e := webhookEnv(t, receiver.URL)
	e.drainer.hold = func(context.Context, *models.NotificationDelivery) (time.Duration, error) {
		return 0, fmt.Errorf("silences unreadable")
	}
	e.enqueue(t, "")
	e.drain(t)
	if n := calls.Load(); n != 0 {
		t.Fatalf("receiver got %d requests, want none", n)
	}
	if row := e.row(t, 1); row.Status != models.DeliveryPending || row.Attempts != 0 || !row.NextAttemptAt.After(time.Now()) {
		t.Errorf("row = status %s, attempts %d, next attempt %s; want pending, 0, later", row.Status, row.Attempts, row.NextAttemptAt)
	}
}

// TestDrainChecksEachRowWhenItsTurnComes: the hold check and the reservation
// check run when a worker is ready to send a row, not when the batch is
// reserved. A row that a slow send kept waiting goes out held if a silence
// started meanwhile, and not at all if it was cancelled meanwhile.
func TestDrainChecksEachRowWhenItsTurnComes(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= sendWorkers {
			<-release
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(receiver.Close)
	e := webhookEnv(t, receiver.URL)
	// sendWorkers rows that stall, then one row to hold and one to cancel.
	recipients := make([]string, sendWorkers+2)
	e.enqueue(t, recipients...)
	heldID, cancelledID := int64(sendWorkers+1), int64(sendWorkers+2)
	var silenced atomic.Bool
	e.drainer.hold = func(_ context.Context, row *models.NotificationDelivery) (time.Duration, error) {
		if row.ID == heldID && silenced.Load() {
			return time.Hour, nil
		}
		return 0, nil
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.drain(t)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for calls.Load() < sendWorkers && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := calls.Load(); got != sendWorkers {
		t.Fatalf("receiver got %d requests, want the %d stalled ones", got, sendWorkers)
	}
	silenced.Store(true)
	if _, err := e.raw.Exec(`UPDATE notification_deliveries SET status = 'failed', reserved_token = reserved_token + 1 WHERE id = ?`, cancelledID); err != nil {
		t.Fatal(err)
	}
	close(release)
	<-done
	if got := calls.Load(); got != sendWorkers {
		t.Errorf("receiver got %d requests, want only the %d stalled ones", got, sendWorkers)
	}
	if row := e.row(t, heldID); row.Status != models.DeliveryPending || row.Attempts != 0 {
		t.Errorf("held row = status %s, attempts %d; want pending with no attempt spent", row.Status, row.Attempts)
	}
	if row := e.row(t, cancelledID); row.Status != models.DeliveryFailed {
		t.Errorf("cancelled row status = %s, want failed", row.Status)
	}
}

// TestOverLimitMessageIsNotSent: a message over 256 KiB fails at enqueue and
// never reaches the receiver, the other row is sent, and the pending gauge
// counts only the row that waits.
func TestOverLimitMessageIsNotSent(t *testing.T) {
	receiver, calls, _ := statusReceiver(t)
	e := webhookEnv(t, receiver.URL)
	big := testMessage
	big.Title = strings.Repeat("x", 256<<10)
	rows := []Delivery{
		{DestinationID: e.dest.ID, Producer: "test", ProducerRef: "ref", SequenceKey: "seq", DedupKey: "small", Message: testMessage},
		{DestinationID: e.dest.ID, Producer: "test", ProducerRef: "ref", SequenceKey: "seq", DedupKey: "big", Message: big},
	}
	if err := e.db.WithTx(t.Context(), func(tx store.StoreOps) error { return Enqueue(t.Context(), tx, rows) }); err != nil {
		t.Fatal(err)
	}
	if got := e.drainer.Pending(); got != 1 {
		t.Fatalf("pending before the drain = %v, want 1", got)
	}
	e.drain(t)
	if calls.Load() != 1 || e.row(t, 1).Status != models.DeliverySent {
		t.Fatalf("%d requests, small row %+v; want the small row sent once", calls.Load(), e.row(t, 1))
	}
	if row := e.row(t, 2); row.Status != models.DeliveryFailed || row.Attempts != 0 || !strings.Contains(row.LastError, "over the 262144-byte limit") ||
		row.MessageJSON != `{"event":"firing","title":"","text":""}` {
		t.Fatalf("big row = %+v, want failed without an attempt, with the event only", row)
	}
	if got := e.drainer.Pending(); got != 0 {
		t.Fatalf("pending after the drain = %v, want 0", got)
	}
}
