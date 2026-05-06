package core

import (
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestUserCanEditAlert(t *testing.T) {
	t.Parallel()

	creator := models.UserID(42)
	other := models.UserID(99)

	cases := []struct {
		name  string
		alert *models.Alert
		user  *models.User
		want  bool
	}{
		{
			name:  "nil alert",
			alert: nil,
			user:  &models.User{ID: creator},
			want:  false,
		},
		{
			name:  "nil user",
			alert: &models.Alert{CreatedBy: &creator},
			user:  nil,
			want:  false,
		},
		{
			name:  "global admin always allowed",
			alert: &models.Alert{CreatedBy: &other},
			user:  &models.User{ID: creator, Role: models.UserRoleAdmin},
			want:  true,
		},
		{
			name:  "global admin allowed on legacy NULL-creator alert",
			alert: &models.Alert{CreatedBy: nil},
			user:  &models.User{ID: creator, Role: models.UserRoleAdmin},
			want:  true,
		},
		{
			name:  "creator allowed",
			alert: &models.Alert{CreatedBy: &creator},
			user:  &models.User{ID: creator, Role: models.UserRoleMember},
			want:  true,
		},
		{
			name:  "non-creator member denied",
			alert: &models.Alert{CreatedBy: &creator},
			user:  &models.User{ID: other, Role: models.UserRoleMember},
			want:  false,
		},
		{
			name:  "legacy NULL-creator alert denied for non-admin",
			alert: &models.Alert{CreatedBy: nil},
			user:  &models.User{ID: creator, Role: models.UserRoleMember},
			want:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := UserCanEditAlert(tc.alert, tc.user); got != tc.want {
				t.Errorf("UserCanEditAlert(%+v, %+v) = %v, want %v", tc.alert, tc.user, got, tc.want)
			}
		})
	}
}
