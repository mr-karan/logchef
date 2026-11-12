package postgres

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mr-karan/logchef/pkg/models"
)

// A serialization failure or a deadlock maps to models.ErrDatabaseBusy; a
// constraint violation does not.
func TestTranslateTransientConflict(t *testing.T) {
	for code, want := range map[string]bool{"40001": true, "40P01": true, "23505": false} {
		err := translateTransientConflict(&pgconn.PgError{Code: code})
		if models.IsDatabaseBusy(err) != want {
			t.Errorf("code %s: IsDatabaseBusy = %v, want %v", code, !want, want)
		}
	}
	plain := errors.New("plain")
	if got := translateTransientConflict(plain); !errors.Is(got, plain) || models.IsDatabaseBusy(got) {
		t.Errorf("plain error was wrapped: %v", got)
	}
}
