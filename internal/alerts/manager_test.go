package alerts

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// TestEvaluateAlertWithTimeoutIsolatesHungEvaluation proves the per-alert
// deadline bounds a single evaluation, so a wedged source (one whose evaluation
// never returns on its own) cannot freeze the sequential evaluation loop.
func TestEvaluateAlertWithTimeoutIsolatesHungEvaluation(t *testing.T) {
	t.Parallel()

	sawDeadline := make(chan bool, 1)
	m := &Manager{
		evalTimeout: 50 * time.Millisecond,
		evalFn: func(ctx context.Context, _ *models.Alert) error {
			_, ok := ctx.Deadline()
			sawDeadline <- ok
			// Simulate a hung source: block until the per-alert deadline fires.
			<-ctx.Done()
			return ctx.Err()
		},
	}

	done := make(chan error, 1)
	go func() {
		done <- m.evaluateAlertWithTimeout(context.Background(), &models.Alert{ID: 1})
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected context deadline exceeded, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("evaluateAlertWithTimeout did not return within the deadline; a hung source can freeze the loop")
	}

	if hadDeadline := <-sawDeadline; !hadDeadline {
		t.Fatal("evaluation context carried no deadline")
	}
}
