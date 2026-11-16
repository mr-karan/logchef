package reports

import (
	"context"
	"errors"
	"net/mail"
	"sync"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

const (
	// PanelQueryTimeout bounds one panel query.
	PanelQueryTimeout = models.DefaultQueryTimeoutSeconds * time.Second
	// HandlerSlack is the time between the query deadline and the handler
	// deadline: render, delivery and the final update.
	HandlerSlack = 5 * time.Minute
	// deliveryReserve ends delivery this long before the handler deadline,
	// so the final update still fits.
	deliveryReserve = 15 * time.Second
	// FinishTimeout bounds the final update, on a context the handler
	// deadline does not cancel.
	FinishTimeout = 10 * time.Second
	// InterruptGrace is how long after its deadline a running run is marked
	// interrupted. A live run finishes at most FinishTimeout after it.
	InterruptGrace = 2 * time.Minute
	// panelPool is how many panel queries of one run run at once, as on the
	// dashboard view.
	panelPool = 4
	// maxDeliveryAttempts is the attempts per recipient for a transient
	// failure.
	maxDeliveryAttempts = 3
)

// deliveryBackoff is the wait before the second and the third attempt.
var deliveryBackoff = [maxDeliveryAttempts - 1]time.Duration{2 * time.Second, 8 * time.Second}

// HandlerBudget is the time from the claim to the handler deadline.
func HandlerBudget(queryBudget time.Duration) time.Duration {
	return queryBudget + HandlerSlack
}

// Deadlines splits a run's handler deadline into its phases.
type Deadlines struct {
	Query    time.Time
	Delivery time.Time
	Handler  time.Time
}

func DeadlinesFrom(handler time.Time) Deadlines {
	return Deadlines{Query: handler.Add(-HandlerSlack), Delivery: handler.Add(-deliveryReserve), Handler: handler}
}

// ErrQueryBudget fails a panel that was still running, or had not started,
// at the query deadline.
var ErrQueryBudget = errors.New("timeout: the run's query budget ran out")

// PanelJob runs one panel's query as the report owner and renders it.
type PanelJob struct {
	ID    string
	Title string
	Run   func(ctx context.Context) (PanelOutput, error)
}

// PanelOutput is a panel that ran: the email form and what the run history
// records about its query.
type PanelOutput struct {
	Panel      Panel
	Rows       int
	Truncation models.Truncation
	// Empty means the query matched no rows: no table rows, no stat rows,
	// or a count of zero.
	Empty bool
	// CSV is the result as a file, for a saved query that attaches it.
	CSV *CSVFile
}

// PanelResult is one panel's output or error, in job order.
type PanelResult struct {
	Job    PanelJob
	Output PanelOutput
	Err    error
}

// RunPanels runs the jobs, panelPool at a time, until queryDeadline. A job
// that is still running or has not started at the deadline fails with
// ErrQueryBudget; the others keep their results.
func RunPanels(ctx context.Context, queryDeadline time.Time, jobs []PanelJob) []PanelResult {
	ctx, cancel := context.WithDeadline(ctx, queryDeadline)
	defer cancel()
	out := make([]PanelResult, len(jobs))
	slots := make(chan struct{}, panelPool)
	var wg sync.WaitGroup
	for i, job := range jobs {
		out[i].Job = job
		if ctx.Err() != nil {
			out[i].Err = ErrQueryBudget
			continue
		}
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			out[i].Err = ErrQueryBudget
			continue
		}
		wg.Go(func() {
			defer func() { <-slots }()
			jobCtx, jobCancel := context.WithTimeout(ctx, PanelQueryTimeout)
			defer jobCancel()
			res, err := job.Run(jobCtx)
			switch {
			case err != nil && ctx.Err() != nil:
				out[i].Err = ErrQueryBudget
			case err != nil:
				out[i].Err = err
			default:
				out[i].Output = res
			}
		})
	}
	wg.Wait()
	return out
}

// Recipient is one resolved address of a run.
type Recipient struct {
	Address mail.Address
	Class   models.ReportDeliveryClass
	// Target is a user id or an address, and Via is "direct", "team:<id>"
	// or "email", as the run history stores them.
	Target string
	Via    string
}

// Delivery delivers one message per recipient with the retries of RFC 0008.
type Delivery struct {
	Mailer Mailer
	// Sleep waits between attempts; nil waits on a timer.
	Sleep func(ctx context.Context, d time.Duration) error
}

// Deliver sends msg to r. A transient failure is retried after 2 and 8
// seconds while the retry fits before deadline; a permanent one fails at
// once; an unknown one is never retried, so a recipient gets an edition at
// most once.
func (d Delivery) Deliver(ctx context.Context, r Recipient, msg MailMessage, deadline time.Time) models.ReportDelivery {
	out := models.ReportDelivery{Class: r.Class, Target: r.Target, Via: r.Via}
	if !time.Now().Before(deadline) {
		out.Status, out.Reason = models.ReportDeliverySkipped, string(models.ReportReasonDeadline)
		return out
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	for {
		out.Attempts++
		err := d.Mailer.Send(ctx, r.Address, msg)
		if err == nil {
			out.Status, out.Reason = models.ReportDeliverySent, ""
			return out
		}
		out.Status, out.Reason = models.ReportDeliveryFailed, err.Error()
		mailErr, ok := errors.AsType[*MailError](err)
		if ok {
			out.Reason = mailErr.Err.Error()
		}
		switch {
		case ok && mailErr.Class == MailUnknown:
			out.Status = models.ReportDeliveryUnknown
			return out
		case ok && mailErr.Class == MailPermanent, out.Attempts == maxDeliveryAttempts:
			return out
		}
		wait := deliveryBackoff[out.Attempts-1]
		if time.Now().Add(wait).After(deadline) || d.sleep(ctx, wait) != nil {
			return out
		}
	}
}

func (d Delivery) sleep(ctx context.Context, wait time.Duration) error {
	if d.Sleep != nil {
		return d.Sleep(ctx, wait)
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// deliveryCounts tallies a run's deliveries by what they mean for its
// status.
type deliveryCounts struct {
	sent, failed, unknown, deadline, notConfigured int
}

func countDeliveries(deliveries []models.ReportDelivery) deliveryCounts {
	var c deliveryCounts
	for i := range deliveries {
		d := &deliveries[i]
		switch d.Status {
		case models.ReportDeliverySent:
			c.sent++
		case models.ReportDeliveryFailed:
			c.failed++
			if d.Reason == ErrMailNotConfigured.Error() {
				c.notConfigured++
			}
		case models.ReportDeliveryUnknown:
			c.unknown++
		case models.ReportDeliverySkipped:
			if d.Reason == string(models.ReportReasonDeadline) {
				c.deadline++
			}
		}
	}
	return c
}

// DeliveryOutcome decides the status of a run whose email was built, from
// its panels and its deliveries.
func DeliveryOutcome(panels []models.ReportPanelOutcome, deliveries []models.ReportDelivery) (models.ReportRunStatus, models.ReportRunReason) {
	c := countDeliveries(deliveries)
	if c.sent == 0 {
		switch {
		case c.unknown > 0:
			return models.ReportRunPartial, models.ReportReasonDeliveryUnknown
		case c.notConfigured > 0 && c.notConfigured == c.failed:
			return models.ReportRunFailed, models.ReportReasonSMTPNotConfigured
		case c.failed > 0:
			return models.ReportRunFailed, models.ReportReasonAllDeliveriesFailed
		case c.deadline > 0:
			return models.ReportRunFailed, models.ReportReasonTimeout
		}
		return models.ReportRunFailed, models.ReportReasonNoAccess
	}
	for i := range panels {
		if panels[i].Status == models.ReportPanelFailed {
			return models.ReportRunPartial, models.ReportReasonPanelFailed
		}
	}
	for i := range panels {
		if panels[i].Status == models.ReportPanelLocked {
			return models.ReportRunPartial, models.ReportReasonPanelLocked
		}
	}
	switch {
	case c.failed > 0:
		return models.ReportRunPartial, models.ReportReasonDeliveryFailed
	case c.unknown > 0:
		return models.ReportRunPartial, models.ReportReasonDeliveryUnknown
	case c.deadline > 0:
		return models.ReportRunPartial, models.ReportReasonDeadline
	}
	return models.ReportRunSent, models.ReportReasonNone
}
