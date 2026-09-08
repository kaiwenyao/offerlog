// Timezone normalization regression (review round 4, P1): Go's
// time.LoadLocation accepts "" (→ UTC) and "Local" (→ server zone), so a
// naive "LoadLocation(tz)==nil" validation let empty/Local strings be stored
// verbatim on users.timezone — and SQL `AT TIME ZONE ”` / `'Local'` then made
// that user's /home/summary and /calendar permanently 500. Every profile write
// (PATCH /auth/me and PUT /preferences) must go through NormalizeTimezone:
// empty → the app default, "Local" rejected, valid IANA preserved.
package integration

import (
	"context"
	"testing"
	"time"

	"offerlog/backend/internal/calendar"
	"offerlog/backend/internal/home"
	idrepo "offerlog/backend/internal/identity/repository"
	idservice "offerlog/backend/internal/identity/service"
	"offerlog/backend/internal/reminders"
)

func TestNormalizeTimezoneSemantics(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "Europe/Dublin", false},                // empty → app default
		{"   ", "Europe/Dublin", false},             // whitespace → default
		{" Europe/Dublin ", "Europe/Dublin", false}, // trimmed + valid
		{"Local", "", true},                         // pseudo-zone that would break SQL
		{"local", "", true},                         // case-insensitive rejection
		{"UTC", "UTC", false},                       // real fixed-offset zone stays
		{"Mars/Olympus", "", true},                  // non-IANA
		{"Asia/Shanghai", "Asia/Shanghai", false},
	}
	for _, tc := range cases {
		got, err := idservice.NormalizeTimezone(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("NormalizeTimezone(%q) = %q, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("NormalizeTimezone(%q) unexpected error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("NormalizeTimezone(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A stored blank/Local zone (written before NormalizeTimezone was enforced)
// must not permanently break the user's /home/summary and /calendar — the
// read-side SQL now sanitizes the zone before AT TIME ZONE.
func TestPoisonedStoredZoneDoesNotBreakReads(t *testing.T) {
	db, svc, _, owner := setup(t)
	ctx := context.Background()
	// Poison the row the way the old validation allowed.
	if _, err := db.Pool().Exec(ctx, `UPDATE users SET timezone='' WHERE id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	app := mustCreate(t, svc, owner, "TZSafe", "Role")
	if _, err := db.Pool().Exec(ctx, `INSERT INTO actions(application_id, owner_id, title, due_date, priority, source)
		VALUES($1,$2,'跟进', CURRENT_DATE+1, 'medium','manual')`, app.ID, owner); err != nil {
		t.Fatal(err)
	}

	// home summary must succeed (and return a usable timezone).
	if _, err := home.New(db).Get(ctx, owner, "", time.Monday, time.Now(), 5); err != nil {
		t.Fatalf("home.Get with blank stored zone: %v", err)
	}
	// calendar range must succeed.
	if _, err := calendar.New(db).Range(ctx, owner, "", time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("calendar.Range with blank stored zone: %v", err)
	}
	// reminders pass must succeed.
	if _, err := reminders.New(db).Run(ctx, time.Now()); err != nil {
		t.Fatalf("reminders.Run with blank stored zone: %v", err)
	}
}

// The profile path (through the identity service used by PATCH /auth/me and
// prefs PUT) must never leave a blank or "Local" value on the users row.
func TestProfileUpdateNeverPersistsBlankOrLocalZone(t *testing.T) {
	db, _, _, owner := setup(t)
	ctx := context.Background()

	svc := idservice.New(db, idrepo.NewSQLUsers(db), 24, "test")
	for _, bad := range []string{"", "   ", "Local"} {
		norm, err := idservice.NormalizeTimezone(bad)
		if err != nil {
			// "Local" is rejected before persistence — exactly the point.
			continue
		}
		if _, err := svc.UpdateProfile(ctx, owner, "x", norm); err != nil {
			t.Fatalf("UpdateProfile with normalized %q: %v", bad, err)
		}
		var stored string
		if err := db.Pool().QueryRow(ctx, `SELECT timezone FROM users WHERE id=$1`, owner).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if stored == "" || stored == "Local" {
			t.Fatalf("blank/Local zone was persisted verbatim: %q", stored)
		}
		if _, err := time.LoadLocation(stored); err != nil {
			t.Fatalf("stored zone %q does not load: %v", stored, err)
		}
	}
}
