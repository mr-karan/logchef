package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

var (
	// ErrDashboardNotFound is returned when a dashboard cannot be located.
	ErrDashboardNotFound = errors.New("dashboard not found")
	// ErrInvalidDashboard indicates the request payload failed validation.
	ErrInvalidDashboard = errors.New("invalid dashboard configuration")
)

// CreateDashboard validates and persists a new dashboard owned by createdBy.
func CreateDashboard(ctx context.Context, db store.StoreOps, log *slog.Logger, createdBy models.UserID, req *models.CreateDashboardRequest) (*models.Dashboard, error) {
	if req == nil {
		return nil, ErrInvalidDashboard
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidDashboard)
	}
	if err := models.ValidateDashboardPanels(req.Panels); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidDashboard, err)
	}

	owner := createdBy
	dashboard := &models.Dashboard{
		Name:        name,
		Description: strings.TrimSpace(req.Description),
		PanelsJSON:  req.Panels,
		CreatedBy:   &owner,
	}
	if err := db.CreateDashboard(ctx, dashboard); err != nil {
		log.Error("failed to create dashboard", "error", err, "created_by", createdBy)
		return nil, fmt.Errorf("failed to create dashboard: %w", err)
	}
	log.Info("dashboard created", "dashboard_id", dashboard.ID, "created_by", createdBy)
	return dashboard, nil
}

// GetDashboard retrieves a single dashboard by id.
func GetDashboard(ctx context.Context, db store.StoreOps, log *slog.Logger, id int) (*models.Dashboard, error) {
	dashboard, err := db.GetDashboard(ctx, id)
	if err != nil {
		if models.IsNotFound(err) {
			return nil, ErrDashboardNotFound
		}
		log.Error("failed to get dashboard", "dashboard_id", id, "error", err)
		return nil, fmt.Errorf("failed to get dashboard: %w", err)
	}
	return dashboard, nil
}

// ListDashboards returns every dashboard, newest-updated first.
func ListDashboards(ctx context.Context, db store.StoreOps) ([]*models.Dashboard, error) {
	dashboards, err := db.ListDashboards(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list dashboards: %w", err)
	}
	return dashboards, nil
}

// UpdateDashboard validates and persists changes to an existing dashboard.
func UpdateDashboard(ctx context.Context, db store.StoreOps, log *slog.Logger, id int, req *models.UpdateDashboardRequest) (*models.Dashboard, error) {
	if req == nil {
		return nil, ErrInvalidDashboard
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidDashboard)
	}
	if err := models.ValidateDashboardPanels(req.Panels); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidDashboard, err)
	}

	existing, err := db.GetDashboard(ctx, id)
	if err != nil {
		if models.IsNotFound(err) {
			return nil, ErrDashboardNotFound
		}
		log.Error("failed to load dashboard for update", "dashboard_id", id, "error", err)
		return nil, fmt.Errorf("failed to load dashboard: %w", err)
	}

	existing.Name = name
	existing.Description = strings.TrimSpace(req.Description)
	existing.PanelsJSON = req.Panels
	if err := db.UpdateDashboard(ctx, existing); err != nil {
		if models.IsNotFound(err) {
			return nil, ErrDashboardNotFound
		}
		log.Error("failed to update dashboard", "dashboard_id", id, "error", err)
		return nil, fmt.Errorf("failed to update dashboard: %w", err)
	}

	updated, err := db.GetDashboard(ctx, id)
	if err != nil {
		if models.IsNotFound(err) {
			return nil, ErrDashboardNotFound
		}
		return nil, fmt.Errorf("failed to reload dashboard: %w", err)
	}
	return updated, nil
}

// DeleteDashboard removes a dashboard by id.
func DeleteDashboard(ctx context.Context, db store.StoreOps, log *slog.Logger, id int) error {
	if err := db.DeleteDashboard(ctx, id); err != nil {
		if models.IsNotFound(err) {
			return ErrDashboardNotFound
		}
		log.Error("failed to delete dashboard", "dashboard_id", id, "error", err)
		return fmt.Errorf("failed to delete dashboard: %w", err)
	}
	return nil
}

// UserCanEditDashboard returns true if the user is the creator or a global
// admin. Dashboards with a nil CreatedBy (author deleted) are editable only by
// global admins. Mirrors UserCanEditAlert.
func UserCanEditDashboard(dashboard *models.Dashboard, user *models.User) bool {
	if dashboard == nil || user == nil {
		return false
	}
	if user.Role == models.UserRoleAdmin {
		return true
	}
	return dashboard.CreatedBy != nil && *dashboard.CreatedBy == user.ID
}
