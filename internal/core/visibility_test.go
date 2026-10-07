package core

import (
	"context"
	"errors"
	"testing"

	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/pkg/models"
)

// TestVisibilityForPrincipal pins the read paths that MCP will call: a saved
// query or alert is visible only through a team that has its source, and a
// global admin gets no bypass.
func TestVisibilityForPrincipal(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()

	owner := newTestUser(t, db, "owner@example.com", "Owner")
	outsider := newTestUser(t, db, "outsider@example.com", "Outsider")
	admin := &models.User{Email: "admin@example.com", FullName: "Admin", Role: models.UserRoleAdmin, Status: models.UserStatusActive}
	if err := db.CreateUser(ctx, admin); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	src := newTestSource(t, db, "visibility_src")
	team, err := CreateTeam(ctx, db, log, "visibility", "")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if err := AddTeamMember(ctx, db, log, team.ID, owner.ID, models.TeamRoleMember); err != nil {
		t.Fatalf("AddTeamMember: %v", err)
	}
	if err := db.AddTeamSource(ctx, team.ID, src.ID); err != nil {
		t.Fatalf("AddTeamSource: %v", err)
	}
	query := seedSavedQueryOnSource(t, db, src, owner)
	alert, err := CreateAlert(ctx, db, newFakeDatasourceService(db, log, nil), log, src.ID, owner.ID, newTestCreateAlertRequest())
	if err != nil {
		t.Fatalf("CreateAlert: %v", err)
	}

	for _, tc := range []struct {
		name    string
		user    *models.User
		visible bool
	}{
		{"member of a team with the source", owner, true},
		{"no team", outsider, false},
		{"global admin, no team", admin, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := access.SessionPrincipal(tc.user)

			gotQuery, err := GetSavedQueryForPrincipal(ctx, db, log, p, query.ID)
			if tc.visible && (err != nil || gotQuery.ID != query.ID) {
				t.Fatalf("GetSavedQueryForPrincipal = %v, %v", gotQuery, err)
			}
			if !tc.visible && !errors.Is(err, ErrQueryNotFound) {
				t.Fatalf("GetSavedQueryForPrincipal error = %v, want ErrQueryNotFound", err)
			}

			gotAlert, err := GetAlertForPrincipal(ctx, db, log, p, alert.ID)
			if tc.visible && (err != nil || gotAlert.ID != alert.ID) {
				t.Fatalf("GetAlertForPrincipal = %v, %v", gotAlert, err)
			}
			if !tc.visible && !errors.Is(err, ErrAlertNotFound) {
				t.Fatalf("GetAlertForPrincipal error = %v, want ErrAlertNotFound", err)
			}

			history, err := ListAlertHistoryForPrincipal(ctx, db, log, p, alert.ID, 10)
			if tc.visible && (err != nil || history == nil) {
				t.Fatalf("ListAlertHistoryForPrincipal = %v, %v", history, err)
			}
			if !tc.visible && !errors.Is(err, ErrAlertNotFound) {
				t.Fatalf("ListAlertHistoryForPrincipal error = %v, want ErrAlertNotFound", err)
			}

			alerts, err := ListAlertsBySourceForPrincipal(ctx, db, p, src.ID)
			if tc.visible && (err != nil || len(alerts) != 1) {
				t.Fatalf("ListAlertsBySourceForPrincipal = %v, %v", alerts, err)
			}
			if !tc.visible && !errors.Is(err, ErrSourceAccessDenied) {
				t.Fatalf("ListAlertsBySourceForPrincipal error = %v, want ErrSourceAccessDenied", err)
			}
		})
	}

	p := access.SessionPrincipal(owner)
	if _, err := GetSavedQueryForPrincipal(ctx, db, log, p, query.ID+1000); !errors.Is(err, ErrQueryNotFound) {
		t.Fatalf("missing saved query: %v, want ErrQueryNotFound", err)
	}
	if _, err := GetAlertForPrincipal(ctx, db, log, p, alert.ID+1000); !errors.Is(err, ErrAlertNotFound) {
		t.Fatalf("missing alert: %v, want ErrAlertNotFound", err)
	}
}
