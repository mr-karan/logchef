package provisioning

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// validateSourceViews checks a source's declared views with the rules the API
// applies, plus two of its own: names are unique and at most one view is the
// default. The descriptor of the declared source_type decides whether a
// filter is allowed, so no connection is needed.
func validateSourceViews(prefix string, src config.ProvisionSource, ds *datasource.Service) []string {
	if len(src.Views) == 0 {
		return nil
	}
	if len(src.Views) > models.MaxSavedViewsPerScope {
		return []string{fmt.Sprintf("%s: declares %d views, the limit is %d", prefix, len(src.Views), models.MaxSavedViewsPerScope)}
	}
	provider, err := ds.ProviderForSourceType(src.NormalizedSourceType())
	if err != nil {
		return []string{fmt.Sprintf("%s: %v", prefix, err)}
	}
	descriptor := provider.Describe()

	var errs []string
	seen := make(map[string]bool, len(src.Views))
	var defaultView string
	for i := range src.Views {
		view := &src.Views[i]
		viewPrefix := fmt.Sprintf("%s.views[%d] (%q)", prefix, i, view.Name)
		if _, _, err := core.SavedViewText(view.Name, view.Description); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", viewPrefix, err))
			continue
		}
		if seen[view.Name] {
			errs = append(errs, fmt.Sprintf("%s: duplicate view name", viewPrefix))
		}
		seen[view.Name] = true
		if view.Default {
			if defaultView != "" {
				errs = append(errs, fmt.Sprintf("%s: default is also set on view %q; a source has at most one default view", viewPrefix, defaultView))
			} else {
				defaultView = view.Name
			}
		}
		if err := core.ValidateSavedViewDefinition(view.Definition(), descriptor); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", viewPrefix, err))
		}
	}
	return errs
}

// listSourceScopeViews returns the source views of one source, ordered by
// name. Zero user and team ids match no personal or team view.
func listSourceScopeViews(ctx context.Context, db store.StoreOps, sourceID models.SourceID) ([]*models.SavedView, error) {
	views, err := db.ListSavedViews(ctx, sourceID, 0, 0)
	if err != nil {
		return nil, err
	}
	out := views[:0]
	for _, v := range views {
		if v.Scope == models.SavedViewScopeSource {
			out = append(out, v)
		}
	}
	return out, nil
}

// reconcileSourceViews makes the source views of every declared source match
// the file. It runs after reconcileSources, so every declared source exists
// and a pruned source has taken its views with it.
func reconcileSourceViews(ctx context.Context, tx store.StoreOps, cfg *config.ProvisioningConfig, log *slog.Logger, events *auditLog) error {
	for i := range cfg.Sources {
		cfgSrc := &cfg.Sources[i]
		src, err := tx.GetSourceByNameForProvisioning(ctx, cfgSrc.Name)
		if err != nil {
			return fmt.Errorf("failed to look up source %q: %w", cfgSrc.Name, err)
		}
		if err := reconcileViewsOfSource(ctx, tx, cfgSrc, src.ID, cfg.Prune, log, events); err != nil {
			return fmt.Errorf("source %q: %w", cfgSrc.Name, err)
		}
	}
	return nil
}

func reconcileViewsOfSource(ctx context.Context, tx store.StoreOps, cfgSrc *config.ProvisionSource, sourceID models.SourceID, prune bool, log *slog.Logger, events *auditLog) error {
	existing, err := listSourceScopeViews(ctx, tx, sourceID)
	if err != nil {
		return fmt.Errorf("failed to list views: %w", err)
	}
	byName := make(map[string]*models.SavedView, len(existing))
	for _, v := range existing {
		byName[v.Name] = v
	}

	declared := make(map[string]bool, len(cfgSrc.Views))
	var wantDefault *models.SavedView
	for i := range cfgSrc.Views {
		view, err := upsertManagedView(ctx, tx, sourceID, &cfgSrc.Views[i], byName[cfgSrc.Views[i].Name], log, events)
		if err != nil {
			return err
		}
		declared[view.Name] = true
		if cfgSrc.Views[i].Default {
			wantDefault = view
		}
	}

	var currentDefault *models.SavedView
	for _, v := range existing {
		if !v.Managed || declared[v.Name] {
			if v.IsDefault {
				currentDefault = v
			}
			continue
		}
		if !prune {
			log.Warn("managed view not in config (prune=false, keeping)", "source", cfgSrc.Name, "view", v.Name)
			if v.IsDefault {
				currentDefault = v
			}
			continue
		}
		log.Warn("pruning managed view not in config", "source", cfgSrc.Name, "view", v.Name, "id", v.ID)
		if err := tx.DeleteSavedView(ctx, v.ID); err != nil {
			return fmt.Errorf("failed to prune view %q: %w", v.Name, err)
		}
		events.add(audit.SavedViewDelete, audit.SavedViewTarget(v), audit.SavedViewFields(v), nil)
	}

	return reconcileDefaultView(ctx, tx, cfgSrc.Name, currentDefault, wantDefault, log, events)
}

// upsertManagedView creates, adopts or updates the stored view for one
// declaration and returns the stored view. An unchanged view is not written,
// so its revision stays put. An adoption and an update in one run are one
// saved_view.update event.
func upsertManagedView(ctx context.Context, tx store.StoreOps, sourceID models.SourceID, decl *config.ProvisionView, current *models.SavedView, log *slog.Logger, events *auditLog) (*models.SavedView, error) {
	def := decl.Definition()
	if current == nil {
		log.Info("creating managed view", "source_id", sourceID, "view", decl.Name)
		view := &models.SavedView{
			SourceID:    sourceID,
			Scope:       models.SavedViewScopeSource,
			Name:        decl.Name,
			Description: decl.Description,
			Definition:  def,
			Managed:     true,
		}
		if err := tx.CreateSavedView(ctx, view); err != nil {
			return nil, fmt.Errorf("failed to create view %q: %w", decl.Name, err)
		}
		events.add(audit.SavedViewCreate, audit.SavedViewTarget(view), nil, audit.SavedViewFields(view))
		return view, nil
	}

	before := audit.SavedViewFields(current)
	adopted := !current.Managed
	if adopted {
		log.Info("adopting existing view as managed", "source_id", sourceID, "view", decl.Name, "id", current.ID)
		if err := tx.MarkSavedViewManaged(ctx, current.ID); err != nil {
			return nil, fmt.Errorf("failed to adopt view %q: %w", decl.Name, err)
		}
		current.Managed = true
	}

	same, err := sameViewContent(current, decl.Description, def)
	if err != nil {
		return nil, fmt.Errorf("failed to compare view %q: %w", decl.Name, err)
	}
	if same {
		if adopted {
			events.add(audit.SavedViewUpdate, audit.SavedViewTarget(current), before, audit.SavedViewFields(current))
		}
		return current, nil
	}
	log.Info("updating managed view", "source_id", sourceID, "view", decl.Name, "revision", current.Revision)
	next := *current
	next.Description, next.Definition = decl.Description, def
	if err := tx.UpdateSavedView(ctx, &next); err != nil {
		return nil, fmt.Errorf("failed to update view %q: %w", decl.Name, err)
	}
	events.add(audit.SavedViewUpdate, audit.SavedViewTarget(&next), before, audit.SavedViewFields(&next))
	return &next, nil
}

func sameViewContent(v *models.SavedView, description string, def models.SavedViewDefinition) (bool, error) {
	if v.Description != description {
		return false, nil
	}
	stored, err := json.Marshal(v.Definition)
	if err != nil {
		return false, err
	}
	desired, err := json.Marshal(def)
	if err != nil {
		return false, err
	}
	return bytes.Equal(stored, desired), nil
}

// reconcileDefaultView gives the source default to the declared default view.
// With no declared default it clears a managed default and leaves an
// unmanaged one, so a file without a default leaves it to global admins.
func reconcileDefaultView(ctx context.Context, tx store.StoreOps, source string, current, want *models.SavedView, log *slog.Logger, events *auditLog) error {
	switch {
	case want != nil && (current == nil || current.ID != want.ID):
		log.Info("setting managed default view", "source", source, "view", want.Name)
		cleared, err := tx.SetSavedViewDefault(ctx, want.ID)
		if err != nil {
			return fmt.Errorf("failed to make view %q the default: %w", want.Name, err)
		}
		var clearedID *models.SavedViewID
		if cleared != nil {
			clearedID = &cleared.ID
		}
		after := *want
		after.IsDefault = true
		events.addWithDetail(audit.SavedViewSetDefault, audit.SavedViewTarget(&after), audit.SavedViewFields(want), audit.SavedViewFields(&after),
			audit.NewFields().Set("scope", after.Scope).Set("cleared_view_id", clearedID))
	case want == nil && current != nil && current.Managed:
		log.Info("clearing managed default view", "source", source, "view", current.Name)
		if err := tx.ClearSavedViewDefault(ctx, current.ID); err != nil {
			return fmt.Errorf("failed to clear the default of view %q: %w", current.Name, err)
		}
		after := *current
		after.IsDefault = false
		events.addWithDetail(audit.SavedViewClearDefault, audit.SavedViewTarget(&after), audit.SavedViewFields(current), audit.SavedViewFields(&after),
			audit.NewFields().Set("scope", after.Scope))
	}
	return nil
}

// exportSourceViews returns the source views of a source as declarations.
func exportSourceViews(ctx context.Context, db store.StoreOps, src *models.Source) ([]config.ProvisionView, error) {
	views, err := listSourceScopeViews(ctx, db, src.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to list views for source %q: %w", src.Name, err)
	}
	var out []config.ProvisionView
	for _, v := range views {
		out = append(out, config.ProvisionViewFromSaved(v))
	}
	return out, nil
}
