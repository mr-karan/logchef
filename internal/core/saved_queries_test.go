package core

import (
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

// TestUserCanDeleteSavedQuery covers the base creator-or-admin authority shared
// by delete (and the non-delegated part of edit). Delegated collection-editor
// edit access requires a DB and is exercised by the integration/browser smoke test.
func TestUserCanDeleteSavedQuery(t *testing.T) {
	t.Parallel()

	creator := models.UserID(42)
	other := models.UserID(99)

	cases := []struct {
		name  string
		query *models.SavedQuery
		user  *models.User
		want  bool
	}{
		{
			name:  "nil query",
			query: nil,
			user:  &models.User{ID: creator},
			want:  false,
		},
		{
			name:  "nil user",
			query: &models.SavedQuery{CreatedBy: &creator},
			user:  nil,
			want:  false,
		},
		{
			name:  "global admin always allowed",
			query: &models.SavedQuery{CreatedBy: &other},
			user:  &models.User{ID: creator, Role: models.UserRoleAdmin},
			want:  true,
		},
		{
			name:  "global admin allowed on legacy NULL-creator query",
			query: &models.SavedQuery{CreatedBy: nil},
			user:  &models.User{ID: creator, Role: models.UserRoleAdmin},
			want:  true,
		},
		{
			name:  "creator allowed",
			query: &models.SavedQuery{CreatedBy: &creator},
			user:  &models.User{ID: creator, Role: models.UserRoleMember},
			want:  true,
		},
		{
			name:  "non-creator member denied",
			query: &models.SavedQuery{CreatedBy: &creator},
			user:  &models.User{ID: other, Role: models.UserRoleMember},
			want:  false,
		},
		{
			name:  "legacy NULL-creator query denied for non-admin",
			query: &models.SavedQuery{CreatedBy: nil},
			user:  &models.User{ID: creator, Role: models.UserRoleMember},
			want:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := UserCanDeleteSavedQuery(tc.query, tc.user); got != tc.want {
				t.Errorf("UserCanDeleteSavedQuery(%+v, %+v) = %v, want %v", tc.query, tc.user, got, tc.want)
			}
		})
	}
}
