package alerts

import (
	"context"
	"fmt"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// handleTriggered and handleResolved evaluate one ungrouped value without a
// query. They keep the entry points that manager_test.go and
// delivery_regression_test.go drive; Transition decides the state either way.
func (m *Manager) handleTriggered(ctx context.Context, alert *models.Alert, value float64) error {
	return m.evaluateValue(ctx, alert, value)
}

func (m *Manager) handleResolved(ctx context.Context, alert *models.Alert, value float64) error {
	return m.evaluateValue(ctx, alert, value)
}

func (m *Manager) evaluateValue(ctx context.Context, alert *models.Alert, value float64) error {
	// Callers reuse one alert value across ticks, so read the current eval_seq.
	stored, err := m.db.GetAlert(ctx, alert.ID)
	if err != nil {
		return fmt.Errorf("failed to read alert: %w", err)
	}
	return m.commitEvaluation(ctx, alert, stored.EvalSeq, []Sample{{Value: value}}, time.Now())
}
