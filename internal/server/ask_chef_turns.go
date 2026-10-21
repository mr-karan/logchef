package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/mr-karan/logchef/internal/askchef"
	"github.com/mr-karan/logchef/pkg/models"
)

const (
	// askChefHeartbeat is how often a running turn writes an SSE comment, so
	// proxies keep the stream open and a gone client shows up as a failed
	// write during long tool calls.
	askChefHeartbeat = 15 * time.Second
	// askChefTurnLease bounds a turn's registry entry, even when its stream
	// writer never runs. It outlasts the turn's own deadline.
	askChefTurnLease = askchef.TurnTimeout + 15*time.Second
)

var errAskChefClientGone = errors.New("ask chef client is gone")

// askChefTurns holds the running Ask Chef turn of each user. A new turn
// from a user preempts that user's running turn.
type askChefTurns struct {
	mu      sync.Mutex
	running map[models.UserID]*askChefTurn
}

// askChefTurn is one registered turn. Its address is the token that lets a
// turn remove only its own entry, never the turn that preempted it.
type askChefTurn struct {
	cancel context.CancelCauseFunc
}

// start registers a turn for userID, preempts the turn that user already
// runs, and returns the new turn's context. The context ends when cancel is
// called, on preemption, or after lease, and the turn leaves the registry
// when it ends.
func (r *askChefTurns) start(parent context.Context, userID models.UserID, lease time.Duration) (context.Context, context.CancelCauseFunc, bool) {
	base, cancel := context.WithCancelCause(parent)
	ctx, stopLease := context.WithTimeout(base, lease)
	turn := &askChefTurn{cancel: cancel}

	r.mu.Lock()
	if r.running == nil {
		r.running = make(map[models.UserID]*askChefTurn)
	}
	previous := r.running[userID]
	r.running[userID] = turn
	r.mu.Unlock()
	if previous != nil {
		previous.cancel(askchef.ErrPreempted)
	}

	context.AfterFunc(ctx, func() {
		r.mu.Lock()
		if r.running[userID] == turn {
			delete(r.running, userID)
		}
		r.mu.Unlock()
		stopLease()
		cancel(nil)
	})
	return ctx, cancel, previous != nil
}

// askChefStream writes one turn's events and heartbeats to the response. A
// failed write means the client is gone, so it cancels the turn.
type askChefStream struct {
	mu     sync.Mutex
	w      *bufio.Writer
	cancel context.CancelCauseFunc
	log    *slog.Logger
	failed bool
}

// event writes one event. It returns false when the client is gone.
func (s *askChefStream) event(ev askchef.Event) bool {
	data, err := json.Marshal(ev)
	if err != nil {
		s.log.Error("error encoding ask chef event", "event", ev.Name(), "error", err)
		s.cancel(fmt.Errorf("encoding %s event: %w", ev.Name(), err))
		return false
	}
	return s.write(fmt.Sprintf("event: %s\ndata: %s\n\n", ev.Name(), data))
}

func (s *askChefStream) write(frame string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed {
		return false
	}
	_, err := s.w.WriteString(frame)
	if err == nil {
		err = s.w.Flush()
	}
	if err != nil {
		s.failed = true
		s.cancel(errAskChefClientGone)
		return false
	}
	return true
}

// heartbeat writes an SSE comment every interval until ctx ends or the
// returned stop is called. stop waits for the heartbeat to finish.
func (s *askChefStream) heartbeat(ctx context.Context, every time.Duration) (stop func()) {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !s.write(": ping\n\n") {
					return
				}
			}
		}
	})
	return func() {
		close(done)
		wg.Wait()
	}
}
