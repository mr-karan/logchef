package sqlite

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestDeleteExpiredSessionsTimeZones(t *testing.T) {
	db := newTxTestDB(t)
	ctx := context.Background()
	user := makeUser("sessions@example.com")
	if err := db.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	now, cases := expiryTimeZoneCases()
	cases = append(cases, expiryTimeZoneCase{"exact expiry", now, now, false})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			session := &models.Session{ID: models.SessionID(tc.name), UserID: user.ID, ExpiresAt: tc.expiresAt}
			if err := db.CreateSession(ctx, session); err != nil {
				t.Fatal(err)
			}
			if err := db.DeleteExpiredSessions(ctx, tc.before); err != nil {
				t.Fatal(err)
			}
			_, err := db.GetSession(ctx, session.ID)
			if tc.wantAlive {
				if err != nil {
					t.Fatalf("live session deleted: %v", err)
				}
			} else if !errors.Is(err, models.ErrNotFound) {
				t.Fatalf("expired session survived: got %v, want ErrNotFound", err)
			}
		})
	}
}

type expiryTimeZoneCase struct {
	name      string
	expiresAt time.Time
	before    time.Time
	wantAlive bool
}

func expiryTimeZoneCases() (time.Time, []expiryTimeZoneCase) {
	now := time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)
	west := time.FixedZone("CDT", -5*3600)
	east := time.FixedZone("IST", 5*3600+1800)
	return now, []expiryTimeZoneCase{
		{"live west expiry", now.Add(2 * time.Hour).In(west), now, true},
		{"expired east expiry", now.Add(-2 * time.Hour).In(east), now, false},
		{"live east cutoff", now.Add(2 * time.Hour), now.In(east), true},
		{"expired west cutoff", now.Add(-2 * time.Hour), now.In(west), false},
	}
}

func TestCountUserSessionsTimeZones(t *testing.T) {
	now := time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		id        models.SessionID
		expiresAt time.Time
		wantCount int
	}{
		{"live-west", now.Add(2 * time.Hour).In(time.FixedZone("CDT", -5*3600)), 1},
		{"expired-east", now.Add(-2 * time.Hour).In(time.FixedZone("IST", 5*3600+1800)), 0},
		{"live-utc", now.Add(2 * time.Hour), 1},
		{"expired-utc", now.Add(-2 * time.Hour), 0},
	}
	for _, tc := range cases {
		t.Run(string(tc.id), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				db := newTxTestDB(t)
				ctx := context.Background()
				user := makeUser(string(tc.id) + "@example.com")
				if err := db.CreateUser(ctx, user); err != nil {
					t.Fatal(err)
				}
				session := &models.Session{ID: tc.id, UserID: user.ID, ExpiresAt: tc.expiresAt}
				if err := db.CreateSession(ctx, session); err != nil {
					t.Fatal(err)
				}
				count, err := db.CountUserSessions(ctx, user.ID)
				if err != nil {
					t.Fatal(err)
				}
				if count != tc.wantCount {
					t.Fatalf("active sessions = %d, want %d", count, tc.wantCount)
				}
			})
		})
	}
}
