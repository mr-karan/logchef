package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/logchefql"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

var (
	// ErrInvalidSavedView is a request rule failure outside the definition.
	// Definition failures wrap models.ErrInvalidSavedViewDefinition.
	ErrInvalidSavedView = errors.New("invalid saved view")
	// ErrSavedViewNotFound covers a missing view and a view the caller cannot
	// read, so the route does not confirm that a private id exists.
	ErrSavedViewNotFound = errors.New("saved view not found")
	// ErrSavedViewForbidden means the caller can read the view but may not
	// make this change.
	ErrSavedViewForbidden = errors.New("you may not change this view")
	// ErrSavedViewManaged means provisioning owns the view.
	ErrSavedViewManaged = errors.New("this view is managed by provisioning")
	// ErrSavedViewManagedDefault means the change would clear the default
	// flag of a view that provisioning owns.
	ErrSavedViewManagedDefault = errors.New("provisioning manages the default view of this source")
	// ErrSavedViewNameTaken means another view of the scope has the name.
	ErrSavedViewNameTaken = errors.New("a view with this name already exists here")
	// ErrSavedViewStale means another save moved the view past the revision
	// the caller loaded.
	ErrSavedViewStale = errors.New("this view changed")
	// ErrSavedViewDefaultConflict means a concurrent request changed the
	// default of the scope first.
	ErrSavedViewDefaultConflict = errors.New("the default view changed at the same time")
	// ErrSavedViewLimit means the scope is at its cap. SavedViewLimitError
	// carries the scope.
	ErrSavedViewLimit = errors.New("too many saved views")
)

// SavedViewLimitError refuses a create in a scope at its cap.
type SavedViewLimitError struct {
	Scope models.SavedViewScope
}

func (e *SavedViewLimitError) Error() string {
	switch e.Scope {
	case models.SavedViewScopePersonal:
		return fmt.Sprintf("You have %d views for this source. Delete one first.", models.MaxSavedViewsPerScope)
	case models.SavedViewScopeTeam:
		return fmt.Sprintf("This team has %d views for this source. Delete one first.", models.MaxSavedViewsPerScope)
	default:
		return fmt.Sprintf("This source has %d source views. Delete one first.", models.MaxSavedViewsPerScope)
	}
}

func (e *SavedViewLimitError) Is(target error) bool { return target == ErrSavedViewLimit }

// SavedViewCaller is who asks, for one authorized team and source.
type SavedViewCaller struct {
	principal access.Principal
	source    access.AuthorizedSource
	// role is the caller's role in the team, or empty for a global admin
	// with no membership row.
	role models.TeamRole
}

// NewSavedViewCaller reads the caller's role in the authorized team.
func NewSavedViewCaller(ctx context.Context, db store.StoreOps, p access.Principal, src access.AuthorizedSource) (SavedViewCaller, error) {
	caller := SavedViewCaller{principal: p, source: src}
	member, err := db.GetTeamMember(ctx, src.TeamID(), src.UserID())
	switch {
	case err == nil:
		if member != nil {
			caller.role = member.Role
		}
	case errors.Is(err, models.ErrNotFound), errors.Is(err, sql.ErrNoRows):
	default:
		return SavedViewCaller{}, fmt.Errorf("reading team role: %w", err)
	}
	return caller, nil
}

// SavedViewAccess is what one caller may do with one view.
type SavedViewAccess struct {
	Read, Edit, SetDefault bool
}

// savedViewAccess decides every saved view permission (RFC 0007, "Access and
// security"): the source, then the scope's owner, then the role rules.
func savedViewAccess(v *models.SavedView, c SavedViewCaller) SavedViewAccess {
	if v.SourceID != c.source.SourceID() {
		return SavedViewAccess{}
	}
	globalAdmin := c.principal.RequireGlobalAdmin() == nil
	var a SavedViewAccess
	switch v.Scope {
	case models.SavedViewScopePersonal:
		if v.UserID == nil || *v.UserID != c.source.UserID() {
			return SavedViewAccess{}
		}
		a = SavedViewAccess{Read: true, Edit: true, SetDefault: true}
	case models.SavedViewScopeTeam:
		if v.TeamID == nil || *v.TeamID != c.source.TeamID() {
			return SavedViewAccess{}
		}
		teamAdmin := globalAdmin || c.role == models.TeamRoleAdmin
		a = SavedViewAccess{Read: true, Edit: teamAdmin || c.role == models.TeamRoleEditor, SetDefault: teamAdmin}
	case models.SavedViewScopeSource:
		a = SavedViewAccess{Read: true, Edit: globalAdmin, SetDefault: globalAdmin}
	default:
		return SavedViewAccess{}
	}
	if v.Managed {
		a.Edit, a.SetDefault = false, false
	}
	return a
}

func withAccess(v *models.SavedView, c SavedViewCaller) *models.SavedView {
	a := savedViewAccess(v, c)
	v.CanEdit, v.CanSetDefault = a.Edit, a.SetDefault
	return v
}

// ValidateSavedViewDefinition runs the definition rules, then checks the
// filter as LogchefQL against the provider of the source.
func ValidateSavedViewDefinition(def models.SavedViewDefinition, descriptor datasource.Descriptor) error {
	if err := def.Validate(); err != nil {
		return err
	}
	if def.Filter == nil {
		return nil
	}
	if !slices.Contains(descriptor.QueryLanguages, models.QueryLanguageLogchefQL) {
		return fmt.Errorf("%w: this source does not support LogchefQL, so the view cannot have a filter", models.ErrInvalidSavedViewDefinition)
	}
	if res := logchefql.Validate(*def.Filter); !res.Valid {
		msg := "the query does not parse"
		if res.Error != nil {
			msg = res.Error.Message
		}
		return fmt.Errorf("%w: filter is not valid LogchefQL: %s", models.ErrInvalidSavedViewDefinition, msg)
	}
	return nil
}

func parseSavedViewDefinition(ctx context.Context, db store.StoreOps, ds *datasource.Service, sourceID models.SourceID, raw []byte) (models.SavedViewDefinition, error) {
	def, err := models.ParseSavedViewDefinition(raw)
	if err != nil {
		return models.SavedViewDefinition{}, err
	}
	source, err := db.GetSource(ctx, sourceID)
	if err != nil {
		return models.SavedViewDefinition{}, fmt.Errorf("reading source %d: %w", sourceID, err)
	}
	provider, err := ds.ProviderForSource(source)
	if err != nil {
		return models.SavedViewDefinition{}, err
	}
	if err := ValidateSavedViewDefinition(def, provider.Describe()); err != nil {
		return models.SavedViewDefinition{}, err
	}
	return def, nil
}

func savedViewText(rawName, rawDescription string) (name, description string, err error) {
	name = strings.TrimSpace(rawName)
	description = strings.TrimSpace(rawDescription)
	if name == "" {
		return "", "", fmt.Errorf("%w: name is required", ErrInvalidSavedView)
	}
	if n := utf8.RuneCountInString(name); n > models.MaxSavedViewNameRunes {
		return "", "", fmt.Errorf("%w: name is %d characters, the limit is %d", ErrInvalidSavedView, n, models.MaxSavedViewNameRunes)
	}
	if n := utf8.RuneCountInString(description); n > models.MaxSavedViewDescriptionRunes {
		return "", "", fmt.Errorf("%w: description is %d characters, the limit is %d", ErrInvalidSavedView, n, models.MaxSavedViewDescriptionRunes)
	}
	return name, description, nil
}

// ListSavedViews returns the views the caller can see for the authorized
// team and source, and the default the rules pick: the personal default,
// else the team default, else the source default.
func ListSavedViews(ctx context.Context, db store.StoreOps, c SavedViewCaller) (*models.SavedViewList, error) {
	views, err := db.ListSavedViews(ctx, c.source.SourceID(), c.source.UserID(), c.source.TeamID())
	if err != nil {
		return nil, fmt.Errorf("listing saved views: %w", err)
	}
	list := &models.SavedViewList{Views: make([]*models.SavedView, 0, len(views))}
	// The store orders the views personal, team, source, so the first
	// default is the one the rules pick.
	for _, v := range views {
		list.Views = append(list.Views, withAccess(v, c))
		if v.IsDefault && list.DefaultViewID == nil {
			id := v.ID
			list.DefaultViewID = &id
		}
	}
	return list, nil
}

// readableSavedView loads a view and answers ErrSavedViewNotFound unless the
// caller can read it.
func readableSavedView(ctx context.Context, db store.StoreOps, c SavedViewCaller, id models.SavedViewID) (*models.SavedView, SavedViewAccess, error) {
	v, err := db.GetSavedView(ctx, id)
	if err != nil {
		if models.IsNotFound(err) {
			return nil, SavedViewAccess{}, ErrSavedViewNotFound
		}
		return nil, SavedViewAccess{}, fmt.Errorf("reading saved view %d: %w", id, err)
	}
	a := savedViewAccess(v, c)
	if !a.Read {
		return nil, SavedViewAccess{}, ErrSavedViewNotFound
	}
	return withAccess(v, c), a, nil
}

// GetSavedView returns one view the caller can read.
func GetSavedView(ctx context.Context, db store.StoreOps, c SavedViewCaller, id models.SavedViewID) (*models.SavedView, error) {
	v, _, err := readableSavedView(ctx, db, c, id)
	return v, err
}

// editableSavedView loads a view the caller may change.
func editableSavedView(ctx context.Context, db store.StoreOps, c SavedViewCaller, id models.SavedViewID, needDefault bool) (*models.SavedView, error) {
	v, a, err := readableSavedView(ctx, db, c, id)
	if err != nil {
		return nil, err
	}
	if v.Managed {
		return nil, ErrSavedViewManaged
	}
	if !a.Edit || (needDefault && !a.SetDefault) {
		return nil, ErrSavedViewForbidden
	}
	return v, nil
}

// CreateSavedView creates a view owned by the caller's user (personal) or the
// authorized team (team). It counts the scope and inserts in one
// transaction; with IsDefault it also takes the scope's default there.
func CreateSavedView(ctx context.Context, db store.Store, ds *datasource.Service, c SavedViewCaller, req *models.CreateSavedViewRequest) (*models.SavedView, error) {
	name, description, err := savedViewText(req.Name, req.Description)
	if err != nil {
		return nil, err
	}
	userID := c.source.UserID()
	view := &models.SavedView{
		SourceID:    c.source.SourceID(),
		Scope:       req.Scope,
		Name:        name,
		Description: description,
		CreatedBy:   &userID,
	}
	switch req.Scope {
	case models.SavedViewScopePersonal:
		view.UserID = &userID
	case models.SavedViewScopeTeam:
		teamID := c.source.TeamID()
		view.TeamID = &teamID
	case models.SavedViewScopeSource:
	default:
		return nil, fmt.Errorf("%w: scope must be %q, %q or %q", ErrInvalidSavedView, models.SavedViewScopePersonal, models.SavedViewScopeTeam, models.SavedViewScopeSource)
	}
	a := savedViewAccess(view, c)
	if !a.Edit || (req.IsDefault && !a.SetDefault) {
		return nil, ErrSavedViewForbidden
	}
	view.Definition, err = parseSavedViewDefinition(ctx, db, ds, view.SourceID, req.Definition)
	if err != nil {
		return nil, err
	}

	err = db.WithTx(ctx, func(tx store.StoreOps) error {
		n, err := tx.CountSavedViewsInScope(ctx, view.ScopeKey())
		if err != nil {
			return fmt.Errorf("counting saved views: %w", err)
		}
		if n >= models.MaxSavedViewsPerScope {
			return &SavedViewLimitError{Scope: view.Scope}
		}
		if err := tx.CreateSavedView(ctx, view); err != nil {
			if models.IsConflict(err) {
				return ErrSavedViewNameTaken
			}
			return fmt.Errorf("creating saved view: %w", err)
		}
		if !req.IsDefault {
			return nil
		}
		if _, err := takeSavedViewDefault(ctx, tx, view.ID); err != nil {
			return err
		}
		view.IsDefault = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	return withAccess(view, c), nil
}

// takeSavedViewDefault moves the scope's default to the view inside a
// transaction and returns the id of the view that lost the flag. It refuses,
// and so rolls the transaction back, when that view is managed.
func takeSavedViewDefault(ctx context.Context, tx store.StoreOps, id models.SavedViewID) (*models.SavedViewID, error) {
	cleared, err := tx.SetSavedViewDefault(ctx, id)
	switch {
	case err == nil:
	case models.IsNotFound(err):
		return nil, ErrSavedViewNotFound
	case models.IsConflict(err):
		return nil, ErrSavedViewDefaultConflict
	default:
		return nil, fmt.Errorf("setting saved view default: %w", err)
	}
	if cleared == nil {
		return nil, nil
	}
	if cleared.Managed {
		return nil, ErrSavedViewManagedDefault
	}
	return &cleared.ID, nil
}

// UpdateSavedView writes name, description and definition when the view is
// still at the revision the caller loaded. It returns the view before and
// after the save.
func UpdateSavedView(ctx context.Context, db store.StoreOps, ds *datasource.Service, c SavedViewCaller, id models.SavedViewID, req *models.UpdateSavedViewRequest) (before, after *models.SavedView, err error) {
	if req.Revision == nil {
		return nil, nil, fmt.Errorf("%w: revision is required", ErrInvalidSavedView)
	}
	name, description, err := savedViewText(req.Name, req.Description)
	if err != nil {
		return nil, nil, err
	}
	before, err = editableSavedView(ctx, db, c, id, false)
	if err != nil {
		return nil, nil, err
	}
	def, err := parseSavedViewDefinition(ctx, db, ds, before.SourceID, req.Definition)
	if err != nil {
		return nil, nil, err
	}
	if before.Revision != *req.Revision {
		return nil, nil, ErrSavedViewStale
	}

	next := *before
	next.Name, next.Description, next.Definition, next.Revision = name, description, def, *req.Revision
	if err := db.UpdateSavedView(ctx, &next); err != nil {
		switch {
		case models.IsNotFound(err):
			return nil, nil, ErrSavedViewNotFound
		case models.IsConflict(err):
			return nil, nil, updateConflict(ctx, db, id, *req.Revision)
		default:
			return nil, nil, fmt.Errorf("updating saved view %d: %w", id, err)
		}
	}
	return before, withAccess(&next, c), nil
}

// updateConflict tells a stale revision from a taken name: the store reports
// both as a conflict.
func updateConflict(ctx context.Context, db store.StoreOps, id models.SavedViewID, revision int) error {
	current, err := db.GetSavedView(ctx, id)
	if err != nil {
		if models.IsNotFound(err) {
			return ErrSavedViewNotFound
		}
		return fmt.Errorf("reading saved view %d: %w", id, err)
	}
	if current.Revision != revision {
		return ErrSavedViewStale
	}
	return ErrSavedViewNameTaken
}

// DeleteSavedView deletes a view the caller may edit and returns it.
func DeleteSavedView(ctx context.Context, db store.StoreOps, c SavedViewCaller, id models.SavedViewID) (*models.SavedView, error) {
	v, err := editableSavedView(ctx, db, c, id, false)
	if err != nil {
		return nil, err
	}
	if err := db.DeleteSavedView(ctx, id); err != nil {
		if models.IsNotFound(err) {
			return nil, ErrSavedViewNotFound
		}
		return nil, fmt.Errorf("deleting saved view %d: %w", id, err)
	}
	return v, nil
}

// SetSavedViewDefault makes the view the default of its scope. It returns the
// view before and after, and the id of the view that lost the flag.
func SetSavedViewDefault(ctx context.Context, db store.Store, c SavedViewCaller, id models.SavedViewID) (before, after *models.SavedView, clearedID *models.SavedViewID, err error) {
	before, err = editableSavedView(ctx, db, c, id, true)
	if err != nil {
		return nil, nil, nil, err
	}
	err = db.WithTx(ctx, func(tx store.StoreOps) error {
		clearedID, err = takeSavedViewDefault(ctx, tx, id)
		return err
	})
	if err != nil {
		return nil, nil, nil, err
	}
	next := *before
	next.IsDefault = true
	return before, &next, clearedID, nil
}

// ClearSavedViewDefault clears the view's default flag. It returns the view
// before and after.
func ClearSavedViewDefault(ctx context.Context, db store.StoreOps, c SavedViewCaller, id models.SavedViewID) (before, after *models.SavedView, err error) {
	before, err = editableSavedView(ctx, db, c, id, true)
	if err != nil {
		return nil, nil, err
	}
	if err := db.ClearSavedViewDefault(ctx, id); err != nil {
		if models.IsNotFound(err) {
			return nil, nil, ErrSavedViewNotFound
		}
		return nil, nil, fmt.Errorf("clearing saved view %d default: %w", id, err)
	}
	next := *before
	next.IsDefault = false
	return before, &next, nil
}
