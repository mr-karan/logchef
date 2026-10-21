package sqlite

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestTimestampRoundTrips(t *testing.T) {
	db := newTxTestDB(t)
	ctx := context.Background()
	if _, err := db.writeDB.ExecContext(ctx, `CREATE TABLE timestamp_round_trips (id TEXT PRIMARY KEY, value DATETIME NOT NULL)`); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	bound := now.In(time.FixedZone("IST", 5*3600+1800))
	if _, err := db.writeDB.ExecContext(ctx, `INSERT INTO timestamp_round_trips VALUES ('bound', ?)`, bound); err != nil {
		t.Fatal(err)
	}
	if _, err := db.writeDB.ExecContext(ctx, `INSERT INTO timestamp_round_trips VALUES
		('datetime', datetime('now')),
		('strftime', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		id   string
		want time.Time
	}{
		{"bound", now},
		{"datetime", now.Truncate(time.Second)},
		{"strftime", now.Truncate(time.Millisecond)},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			var raw string
			var got time.Time
			if err := db.readDB.QueryRowContext(ctx, `SELECT CAST(value AS TEXT), value FROM timestamp_round_trips WHERE id = ?`, tc.id).Scan(&raw, &got); err != nil {
				t.Fatal(err)
			}
			if got.Location() != time.UTC {
				t.Errorf("read location = %v, want UTC", got.Location())
			}
			if tc.id == "bound" {
				if want := bound.UTC().String(); raw != want {
					t.Errorf("stored timestamp = %q, want %q", raw, want)
				}
				if !got.Equal(tc.want) {
					t.Errorf("read timestamp = %v, want %v", got, tc.want)
				}
				return
			}
			layout := "2006-01-02 15:04:05"
			if tc.id == "strftime" {
				layout = time.RFC3339Nano
			}
			want, err := time.Parse(layout, raw)
			if err != nil {
				t.Fatal(err)
			}
			if !got.Equal(want) {
				t.Errorf("read timestamp = %v, want %v from SQLite text %q", got, want, raw)
			}
			if got.Before(tc.want) || got.After(time.Now()) {
				t.Errorf("SQLite-generated timestamp %v is outside the insertion interval", got)
			}
		})
	}
}

func TestLegacyTimestampFormat(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE timestamps (value DATETIME)`); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err := db.Exec(`INSERT INTO timestamps VALUES (?)`, now); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := db.QueryRow(`SELECT CAST(value AS TEXT) FROM timestamps`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if want := now.String(); raw != want {
		t.Fatalf("legacy timestamp = %q, want time.String() %q", raw, want)
	}
}
