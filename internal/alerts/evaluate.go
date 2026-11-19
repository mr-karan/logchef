package alerts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/jobs"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// evaluateAlert runs the rule's query over the lookback that ends at at, and
// commits the resulting transition.
// Every write is guarded by alert.EvalSeq, read with the rule, so an edit or
// manual resolve after the rule was loaded makes the evaluation write nothing.
// The next cycle evaluates the new rule.
func (m *Manager) evaluateAlert(ctx context.Context, alert *models.Alert, at time.Time) error {
	if alert == nil {
		return nil
	}

	query := strings.TrimSpace(alert.Query)
	if query == "" {
		m.log.Warn("alert query is empty; skipping evaluation", "alert_id", alert.ID, "query_language", alert.QueryLanguage, "editor_mode", alert.EditorMode)
		return nil
	}

	if m.datasource == nil {
		err := fmt.Errorf("datasource service is not configured")
		m.recordEvaluationError(ctx, alert, err)
		return err
	}

	timeout := models.DefaultQueryTimeoutSeconds
	result, err := m.datasource.EvaluateAlert(ctx, alert.SourceID, datasource.AlertQueryRequest{
		At:              at.UTC(),
		Language:        alert.QueryLanguage,
		Query:           query,
		LookbackSeconds: alert.LookbackSeconds,
		QueryTimeout:    &timeout,
	})
	if err != nil {
		m.recordEvaluationError(ctx, alert, fmt.Errorf("alert query failed: %w", err))
		return fmt.Errorf("alert query failed: %w", err)
	}

	samples, err := SamplesFromResult(result, alert.GroupBy, alert.QueryLanguage)
	if err != nil {
		m.recordEvaluationError(ctx, alert, fmt.Errorf("failed to extract alert result: %w", err))
		return fmt.Errorf("failed to extract alert result: %w", err)
	}
	return m.commitEvaluation(ctx, alert, alert.EvalSeq, samples, at)
}

// episode is the open state of one group: its newest open history row and
// its instance.
type episode struct {
	instance *models.AlertInstance
	history  *models.AlertHistoryEntry
}

// openEpisodes pairs each group's newest open history row with its instance.
// open is newest first.
func openEpisodes(instances []*models.AlertInstance, open []*models.AlertHistoryEntry) map[string]episode {
	byKey := make(map[string]*models.AlertInstance, len(instances))
	for _, instance := range instances {
		byKey[instance.GroupKey] = instance
	}
	episodes := make(map[string]episode, len(open))
	for _, entry := range open {
		if _, ok := episodes[entry.GroupKey]; ok {
			continue
		}
		episodes[entry.GroupKey] = episode{instance: currentInstance(byKey[entry.GroupKey], entry), history: entry}
	}
	return episodes
}

// currentInstance returns the instance of an open episode. A row without a
// matching instance predates instances; it is adopted in the row's state as
// notified, so nothing is sent twice.
func currentInstance(instance *models.AlertInstance, open *models.AlertHistoryEntry) *models.AlertInstance {
	if instance != nil && instance.HistoryID != nil && *instance.HistoryID == open.ID {
		return instance
	}
	historyID := open.ID
	state := models.AlertInstanceStateFiring
	if open.Status == models.AlertStatusNoData {
		state = models.AlertInstanceStateNoData
	}
	return &models.AlertInstance{
		AlertID:    open.AlertID,
		GroupKey:   open.GroupKey,
		Group:      open.Group,
		State:      state,
		Value:      open.Value,
		StateSince: open.TriggeredAt,
		HistoryID:  &historyID,
		Notified:   true,
	}
}

// openStep is one group's change into, or stay in, firing or no_data. A step
// that does not notify only records the evaluation on its instance.
type openStep struct {
	change   Change
	status   models.AlertStatus
	instance models.AlertInstance
	// history is the open episode, nil until a new one is written.
	history *models.AlertHistoryEntry
	// moved is set when an open episode changes between firing and no_data.
	moved        bool
	payload      map[string]any
	message      string
	notify       bool
	notification AlertNotification
}

// resolveStep is one group's closing change.
type resolveStep struct {
	change  Change
	history *models.AlertHistoryEntry
	message string
	notify  bool
	// discard cancels the episode's queued notifications; see discardsQueued.
	discard      bool
	notification AlertNotification
}

// evaluationPlan is what one evaluation writes, then sends.
type evaluationPlan struct {
	opens    []*openStep
	resolves []*resolveStep
	// tracks are the ok instances that the no_data and keep policies keep.
	tracks []models.AlertInstance
	// untracks are the group keys whose instance goes without an episode to close.
	untracks   []string
	state      models.AlertState
	at         time.Time
	newEpisode bool
	// outbox enqueues the plan's notifications in the commit. tag is the
	// eval_seq the commit sets, which tells apart the notifications of one
	// episode.
	outbox *outbox
	tag    string
}

func (m *Manager) commitEvaluation(ctx context.Context, alert *models.Alert, seq int64, samples []Sample, at time.Time) error {
	var mc *messageContext
	messages := func() *messageContext {
		if mc == nil {
			mc = m.newMessageContext(ctx, alert)
		}
		return mc
	}
	plan, err := m.planEvaluation(ctx, alert, samples, at, messages)
	if err == nil {
		plan.outbox, plan.tag = &outbox{alert: alert}, strconv.FormatInt(seq+1, 10)
	}
	if errors.Is(err, ErrTooManyGroups) {
		m.recordEvaluationError(ctx, alert, err)
		return err
	}
	if err != nil {
		return err
	}
	err = m.commit(ctx, alert.ID, seq, func(tx store.StoreOps) error { return plan.write(ctx, tx, alert.ID) })
	if err != nil {
		// Nothing was enqueued, so nothing is sent; the next tick evaluates again.
		if !models.IsNotFound(err) && !models.IsConflict(err) {
			m.log.Error("failed to commit alert evaluation", "alert_id", alert.ID, "error", err)
		}
		return nil
	}
	if plan.newEpisode {
		if err := m.pruneHistory(ctx, alert.ID); err != nil {
			m.log.Warn("failed to prune alert history", "alert_id", alert.ID, "error", err)
		}
	}
	if plan.outbox.enqueued > 0 {
		m.kickDrain(ctx)
	}
	return nil
}

// planEvaluation reads the tracked groups, runs Transition and decides what
// each group writes.
func (m *Manager) planEvaluation(ctx context.Context, alert *models.Alert, samples []Sample, at time.Time, messages func() *messageContext) (*evaluationPlan, error) {
	instances, err := m.db.ListAlertInstances(ctx, alert.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to list alert instances: %w", err)
	}
	open, err := m.db.ListOpenAlertHistory(ctx, alert.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to list open alert history: %w", err)
	}
	silences, err := m.db.ListAlertSilences(ctx, alert.SourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to list alert silences: %w", err)
	}
	silenced := func(group map[string]string) []int64 {
		ids, _ := silencedBy(m.log, alert, silences, group, at)
		return ids
	}
	episodes := openEpisodes(instances, open)
	stored, tracked := trackedGroups(instances, open, episodes)

	rule := Rule{
		Operator:  alert.ThresholdOperator,
		Threshold: alert.ThresholdValue,
		NoData:    alert.NoDataPolicy.OrZero(),
		Grouped:   len(alert.GroupBy) > 0,
	}
	changes, err := Transition(rule, tracked, samples, at)
	if err != nil {
		return nil, err
	}
	plan := &evaluationPlan{state: models.AlertStateResolved, at: at}
	for _, change := range changes {
		m.log.Debug("alert evaluation complete",
			"alert_id", alert.ID,
			"alert_name", alert.Name,
			"group_key", change.GroupKey,
			"value", change.Value,
			"sampled", change.Sampled,
			"threshold", alert.ThresholdValue,
			"operator", alert.ThresholdOperator,
			"from", change.From,
			"to", change.To)
		ep := episodes[change.GroupKey]
		switch {
		case change.To != models.AlertInstanceStateOK:
			current := ep.instance
			if current == nil {
				// A tracked ok group keeps its last value and sighting.
				current = stored[change.GroupKey]
			}
			plan.opens = append(plan.opens, m.planOpen(alert, rule, current, ep.history, change, at, silenced(change.Group), messages))
			plan.state = summaryState(plan.state, change.To)
		case change.Closes():
			muted := silenced(change.Group)
			if len(muted) > 0 {
				m.log.Debug("silenced alert resolve", "alert_id", alert.ID, "group_key", change.GroupKey, "silences", muted)
			}
			step := &resolveStep{
				change:  change,
				history: ep.history,
				message: resolveMessage(alert, change),
				// Only an announced episode resolves aloud, so a group that
				// fired and resolved inside a silence sends nothing.
				notify:  alert.NotifyOnResolve && ep.instance.Notified && len(muted) == 0,
				discard: discardsQueued(ep.instance.Notified, muted),
			}
			if step.notify {
				m.log.Info("alert resolved", "alert_id", alert.ID, "alert_name", alert.Name, "group_key", change.GroupKey, "value", change.Value, "threshold", alert.ThresholdValue)
				step.notification = resolvedNotification(step, at, messages())
			}
			plan.resolves = append(plan.resolves, step)
			if !change.Untracked {
				plan.tracks = append(plan.tracks, okInstance(alert.ID, nil, change, at))
			}
		case change.Untracked:
			if _, ok := stored[change.GroupKey]; ok {
				plan.untracks = append(plan.untracks, change.GroupKey)
			}
		case change.Sampled:
			plan.tracks = append(plan.tracks, okInstance(alert.ID, stored[change.GroupKey], change, at))
		}
	}
	return plan, nil
}

// trackedGroups indexes the stored instances by group key and lists the
// Transition input: each open episode's instance, and each ok instance, which
// the no_data and keep policies keep without an episode.
func trackedGroups(instances []*models.AlertInstance, open []*models.AlertHistoryEntry, episodes map[string]episode) (map[string]*models.AlertInstance, []Instance) {
	stored := make(map[string]*models.AlertInstance, len(instances))
	tracked := make([]Instance, 0, len(instances)+len(episodes))
	for _, entry := range open {
		if ep, ok := episodes[entry.GroupKey]; ok && ep.history == entry {
			tracked = append(tracked, trackedInstance(ep.instance))
		}
	}
	for _, instance := range instances {
		stored[instance.GroupKey] = instance
		if _, ok := episodes[instance.GroupKey]; !ok && instance.State == models.AlertInstanceStateOK {
			tracked = append(tracked, trackedInstance(instance))
		}
	}
	return stored, tracked
}

// trackedInstance is the Transition input of a stored instance.
func trackedInstance(instance *models.AlertInstance) Instance {
	lastSeen := instance.StateSince
	if instance.LastSeenAt != nil {
		lastSeen = *instance.LastSeenAt
	}
	return Instance{GroupKey: instance.GroupKey, Group: instance.Group, State: instance.State, LastSeen: lastSeen}
}

// summaryState folds one group's state into the rule's last_state: firing
// when any group fires, else no_data when any group has no data.
func summaryState(current models.AlertState, group models.AlertInstanceState) models.AlertState {
	switch {
	case group == models.AlertInstanceStateFiring:
		return models.AlertStateFiring
	case group == models.AlertInstanceStateNoData && current != models.AlertStateFiring:
		return models.AlertStateNoData
	default:
		return current
	}
}

func resolveMessage(alert *models.Alert, change Change) string {
	switch {
	case change.Forgotten:
		return fmt.Sprintf("group forgotten after %d hours without data", int(ForgetAfter.Hours()))
	case !change.Sampled && change.Untracked && change.GroupKey == "" && len(alert.GroupBy) > 0:
		return fmt.Sprintf("alert %s resolved: the query returns rows again", alert.Name)
	default:
		return fmt.Sprintf("alert %s resolved with value %.4f", alert.Name, change.Value)
	}
}

// write commits the plan inside the evaluation's eval_seq transaction: the
// open episodes, the closed episodes, their delivery rows, the ok instances
// and the rule's summary state.
func (plan *evaluationPlan) write(ctx context.Context, tx store.StoreOps, alertID models.AlertID) error {
	for _, step := range plan.opens {
		if err := plan.writeOpen(ctx, tx, alertID, step); err != nil {
			return err
		}
	}
	for _, step := range plan.resolves {
		if err := tx.ResolveAlertGroup(ctx, alertID, step.change.GroupKey, step.message, plan.at); err != nil {
			return err
		}
		if step.discard {
			if err := discardQueued(ctx, tx, alertID, step.history.ID); err != nil {
				return err
			}
		}
		if step.notify {
			if err := plan.outbox.add(ctx, tx, step.notification, step.history, plan.tag); err != nil {
				return err
			}
		}
	}
	for _, groupKey := range plan.untracks {
		if err := tx.DeleteAlertInstance(ctx, alertID, groupKey); err != nil {
			return err
		}
	}
	for i := range plan.tracks {
		if err := tx.UpsertAlertInstance(ctx, &plan.tracks[i]); err != nil {
			return err
		}
	}
	return tx.MarkAlertEvaluated(ctx, alertID, plan.state)
}

func (plan *evaluationPlan) writeOpen(ctx context.Context, tx store.StoreOps, alertID models.AlertID, step *openStep) error {
	switch {
	case step.history == nil:
		var value *float64
		if step.status == models.AlertStatusTriggered {
			value = &step.change.Value
		}
		history, err := tx.InsertAlertHistory(ctx, alertID, step.change.GroupKey, step.change.Group, step.status, value, step.message, step.payload)
		if err != nil {
			return err
		}
		step.history = history
		step.instance.StateSince = plan.at
		step.instance.HistoryID = &history.ID
		plan.newEpisode = true
	case step.moved:
		if err := tx.SetOpenAlertHistoryStatus(ctx, step.history.ID, step.status, step.message); err != nil {
			return err
		}
		if err := tx.UpdateAlertHistoryPayload(ctx, step.history.ID, step.payload); err != nil {
			return err
		}
		step.instance.StateSince = plan.at
	case step.payload != nil:
		if err := tx.UpdateAlertHistoryPayload(ctx, step.history.ID, step.payload); err != nil {
			return err
		}
	}
	if err := tx.UpsertAlertInstance(ctx, &step.instance); err != nil {
		return err
	}
	if !step.notify {
		return nil
	}
	step.notification.TriggeredAt = step.history.TriggeredAt
	return plan.outbox.add(ctx, tx, step.notification, step.history, plan.tag)
}

// resolvedNotification is the resolved message of a group whose episode the
// evaluation at at closes.
func resolvedNotification(step *resolveStep, at time.Time, mc *messageContext) AlertNotification {
	resolvedAt := at.UTC()
	entry := *step.history
	entry.Message = step.message
	entry.ResolvedAt = &resolvedAt
	entry.Status = models.AlertStatusResolved
	if entry.Value == nil && step.change.Sampled {
		value := step.change.Value
		entry.Value = &value
	}
	value := step.change.Value
	if !step.change.Sampled && entry.Value != nil {
		value = *entry.Value
	}
	return mc.resolvedNotification(&entry, value, nil)
}

// planOpen decides what one group in firing or no_data writes and whether it
// notifies. A new episode, and an open one that moves between firing and
// no_data, owes a notification. An open episode owes one while its instance is
// not notified, which is how a notification that a silence held back goes out
// once the silence ends. A group that a silence mutes writes its episode with
// silenced_by in the payload and stays not notified. A notification goes into
// the delivery queue in the evaluation's commit, which marks the instance
// notified.
func (m *Manager) planOpen(alert *models.Alert, rule Rule, current *models.AlertInstance, prev *models.AlertHistoryEntry, change Change, at time.Time, silencedBy []int64, messages func() *messageContext) *openStep {
	status := models.AlertStatusTriggered
	if change.To == models.AlertInstanceStateNoData {
		status = models.AlertStatusNoData
	}
	step := &openStep{change: change, status: status, instance: openInstance(alert.ID, rule, current, change, at), history: prev}
	step.moved = prev != nil && change.From != change.To
	if prev == nil {
		step.instance.Notified = false
		step.instance.HistoryID = nil
	}
	if step.moved {
		step.instance.Notified = false
	} else if prev != nil && current.Notified {
		m.log.Debug("suppressing duplicate alert notification", "alert_id", alert.ID, "group_key", change.GroupKey)
		return step
	}

	if len(silencedBy) > 0 {
		return m.planSilencedOpen(alert, step, silencedBy)
	}

	value := change.Value
	if status == models.AlertStatusNoData {
		m.log.Info("alert has no data", "alert_id", alert.ID, "alert_name", alert.Name, "group_key", change.GroupKey, "severity", alert.Severity)
	} else {
		m.log.Info("alert triggered",
			"alert_id", alert.ID,
			"alert_name", alert.Name,
			"group_key", change.GroupKey,
			"severity", alert.Severity,
			"value", value,
			"threshold", alert.ThresholdValue,
			"operator", alert.ThresholdOperator)
	}
	step.message = openMessage(alert, status, value)

	mc := messages()
	labels, annotations := mc.metadata(status, value, change.Group)
	notificationHistory := &models.AlertHistoryEntry{TriggeredAt: at, Message: step.message, GroupKey: change.GroupKey, Group: change.Group}
	if prev != nil {
		entry := *prev
		if step.moved {
			entry.Message = step.message
		}
		notificationHistory = &entry
	}
	payload := make(map[string]any)
	if prev != nil {
		maps.Copy(payload, prev.Payload)
	}
	maps.Copy(payload, map[string]any{
		"labels":      copyStringMap(labels),
		"annotations": copyStringMap(annotations),
		"status":      string(status),
	})
	step.notify = true
	step.instance.Notified = true
	step.payload = payload
	step.notification = mc.notification(notificationHistory, labels, annotations, status, value)
	return step
}

// openMessage is the history message of a group in firing or no_data.
func openMessage(alert *models.Alert, status models.AlertStatus, value float64) string {
	if status == models.AlertStatusNoData {
		return fmt.Sprintf("alert %s has no data: the query returned no row", alert.Name)
	}
	return fmt.Sprintf("alert %s triggered with value %.4f", alert.Name, value)
}

// planSilencedOpen records a group in firing or no_data that a silence
// mutes: the episode is written, nothing is sent, and the instance stays not
// notified, so it owes its notification once the silence ends.
func (m *Manager) planSilencedOpen(alert *models.Alert, step *openStep, silencedBy []int64) *openStep {
	m.log.Info("alert changed state while silenced", "alert_id", alert.ID, "alert_name", alert.Name, "group_key", step.change.GroupKey, "status", step.status, "silences", silencedBy)
	step.instance.Notified = false
	step.message = openMessage(alert, step.status, step.change.Value)
	payload := make(map[string]any)
	if step.history != nil {
		maps.Copy(payload, step.history.Payload)
	}
	payload["status"] = string(step.status)
	payload["silenced_by"] = silencedBy
	step.payload = payload
	return step
}

// silencedBy returns the ids of the silences that mute a group of alert at
// at, and the earliest end of their current windows. A silence whose stored
// schedule cannot be read is logged and mutes nothing, so a broken silence
// never hides a notification.
func silencedBy(log *slog.Logger, alert *models.Alert, silences []*models.AlertSilence, group map[string]string, at time.Time) (ids []int64, until time.Time) {
	if len(silences) == 0 {
		return nil, time.Time{}
	}
	labels := SilenceLabels(alert, group)
	for _, silence := range silences {
		if !Matches(silence, alert, labels) {
			continue
		}
		end, active, err := activeUntil(silence, at)
		if err != nil {
			log.Error("alert silence cannot be evaluated; it mutes nothing", "silence_id", silence.ID, "alert_id", alert.ID, "error", err)
			continue
		}
		if active {
			ids = append(ids, int64(silence.ID))
			if until.IsZero() || end.Before(until) {
				until = end
			}
		}
	}
	return ids, until
}

// openInstance is the instance after an evaluation that left the group in
// firing or no_data. A new episode gets its start and history id when the row
// is written. The value is the one read, or 0 that the zero policy compared;
// a group without a row under the other policies keeps its last value.
func openInstance(alertID models.AlertID, rule Rule, current *models.AlertInstance, change Change, at time.Time) models.AlertInstance {
	instance := models.AlertInstance{AlertID: alertID, GroupKey: change.GroupKey, Group: change.Group}
	if current != nil {
		instance = *current
	}
	instance.State = change.To
	if change.Sampled || rule.NoData == models.AlertNoDataZero {
		value := change.Value
		instance.Value = &value
	}
	if change.Sampled {
		seen := at
		instance.LastSeenAt = &seen
	}
	return instance
}

// okInstance is the instance of an ok group that the no_data and keep
// policies track. current is its stored ok instance, nil when the group was
// untracked or just closed an episode.
func okInstance(alertID models.AlertID, current *models.AlertInstance, change Change, at time.Time) models.AlertInstance {
	instance := models.AlertInstance{AlertID: alertID, GroupKey: change.GroupKey, Group: change.Group, State: models.AlertInstanceStateOK, StateSince: at}
	if current != nil && current.State == models.AlertInstanceStateOK {
		instance = *current
	}
	if change.Sampled {
		value := change.Value
		seen := at
		instance.Value = &value
		instance.LastSeenAt = &seen
	}
	return instance
}

// commit runs fn in one transaction behind the job claim and the eval_seq
// guard. It writes nothing and returns models.ErrConflict when a later claim
// owns the rule's job, or when a person edited or resolved the rule after seq
// was read. Callers treat both as a discarded result.
func (m *Manager) commit(ctx context.Context, alertID models.AlertID, seq int64, fn func(tx store.StoreOps) error) error {
	err := m.db.WithTx(ctx, func(tx store.StoreOps) error {
		if err := holdClaim(ctx, tx); err != nil {
			return err
		}
		if err := tx.AdvanceAlertEvalSeq(ctx, alertID, seq); err != nil {
			return err
		}
		return fn(tx)
	})
	switch {
	case errors.Is(err, jobs.ErrLeaseLost):
		m.log.Warn("a later claim owns this alert's evaluation; result discarded", "alert_id", alertID)
		return models.ErrConflict
	case models.IsConflict(err):
		m.log.Debug("alert changed during evaluation; result discarded", "alert_id", alertID)
	}
	return err
}

// pruneHistory deletes the oldest closed history rows behind the job claim. A
// run whose claim was taken over leaves pruning to the newer run.
func (m *Manager) pruneHistory(ctx context.Context, alertID models.AlertID) error {
	err := m.db.WithTx(ctx, func(tx store.StoreOps) error {
		if err := holdClaim(ctx, tx); err != nil {
			return err
		}
		return tx.PruneAlertHistory(ctx, alertID, m.cfg.HistoryLimit)
	})
	if errors.Is(err, jobs.ErrLeaseLost) {
		return nil
	}
	return err
}

func evaluationLogLevel(ctx context.Context, err error) slog.Level {
	if errors.Is(ctx.Err(), context.Canceled) && errors.Is(err, context.Canceled) {
		return slog.LevelDebug
	}
	return slog.LevelError
}

func (m *Manager) recordEvaluationError(ctx context.Context, alert *models.Alert, evalErr error) {
	if alert == nil || evalErr == nil {
		return
	}
	if errors.Is(ctx.Err(), context.Canceled) && errors.Is(evalErr, context.Canceled) {
		return
	}
	// Evaluation deadlines must not prevent recording genuine failures.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	errorMessage := fmt.Sprintf("Evaluation failed: %v", evalErr)
	errorPayload := map[string]any{
		"error":          evalErr.Error(),
		"query_language": string(alert.QueryLanguage),
		"editor_mode":    string(alert.EditorMode),
		"query":          alert.Query,
		"status":         string(models.AlertStatusError),
	}

	// Failed evaluations still count toward the alert's evaluation frequency.
	err := m.commit(ctx, alert.ID, alert.EvalSeq, func(tx store.StoreOps) error {
		if err := tx.TouchAlertEvaluated(ctx, alert.ID); err != nil {
			return err
		}
		_, err := tx.InsertAlertHistory(ctx, alert.ID, "", nil, models.AlertStatusError, nil, errorMessage, errorPayload)
		return err
	})
	if models.IsConflict(err) {
		return
	}
	if err != nil {
		m.log.Log(ctx, evaluationLogLevel(ctx, err), "failed to insert error history entry", "alert_id", alert.ID, "error", err)
		return
	}

	// Prune old history entries to prevent unbounded growth from repeated errors
	if pruneErr := m.pruneHistory(ctx, alert.ID); pruneErr != nil {
		m.log.Warn("failed to prune alert history after error", "alert_id", alert.ID, "error", pruneErr)
	}
}
