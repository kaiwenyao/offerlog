// Date-only wire semantics regression coverage (review P0): a calendar day
// must travel as YYYY-MM-DD and land on the same day regardless of user or
// server timezone; a UTC-west user rendering the value must see the same day
// they chose. Exercises the storage path directly: the transport parses a
// date-only string into a UTC-midnight instant for the DATE column, and the
// DATE round-trip preserves the day under a UTC-pinned session.
package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"offerlog/backend/internal/platform/day"
)

// A Dublin user picks 2026-09-10 in a date picker; the wire value is the
// string "2026-09-10". Parsing → storing → reading back must yield the same
// day, and the transport's emitted date-only string is what the client renders
// (a plain calendar day — never a timestamp that shifts west of UTC).
func TestDateOnlyRoundTripPreservesDayAcrossZones(t *testing.T) {
	ctx := context.Background()
	db, svc, _, owner := setup(t)
	picked := "2026-09-10"

	// What the transport does with an inbound date-only string:
	tm, err := day.Parse(picked)
	if err != nil {
		t.Fatal(err)
	}
	if got := tm.UTC().Format(day.Layout); got != picked {
		t.Fatalf("parse %q -> %q", picked, got)
	}

	// Store through the applications DATE column (deadline) exactly as the
	// transport passes it (UTC-midnight instant into a DATE column).
	app := mustCreate(t, svc, owner, "TZCo", "Role")
	if _, err := db.Pool().Exec(ctx, `UPDATE applications SET deadline=$2 WHERE id=$1`, app.ID, tm); err != nil {
		t.Fatal(err)
	}
	// Read back via pgx (session pinned UTC): the stored day must still be
	// 2026-09-10 and the transport emits it as the exact date-only string.
	var stored time.Time
	if err := db.Pool().QueryRow(ctx, `SELECT deadline FROM applications WHERE id=$1`, app.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if got := day.Format(stored); got != picked {
		t.Fatalf("DATE round-trip %q -> %q (want same day)", picked, got)
	}
	// Contract guard: emitting an RFC3339 timestamp of this DATE and rendering
	// it in a west-of-UTC browser is exactly the reported bug (LA would show
	// 09-09). The transport must emit day.Format (date-only string) instead.
	// This test pins the day.Format half; the frontend renders the string as a
	// plain day.
	la, _ := time.LoadLocation("America/Los_Angeles")
	t.Logf("instants would render %q in LA — reason the wire is date-only, not a timestamp", stored.In(la).Format(day.Layout))
}

// The old write path sent a Dublin-local midnight instant (09-09T23:00Z) for a
// 09-10 pick; with the session pinned UTC that stored 09-09. Verify the new
// date-only string path never produces that shift.
func TestDateOnlyDoesNotShiftViaLocalMidnightInstant(t *testing.T) {
	ctx := context.Background()
	db, svc, _, owner := setup(t)
	picked := "2026-09-10"

	// The buggy wire value the old client produced (Dublin local midnight):
	buggyDublin := time.Date(2026, 9, 9, 23, 0, 0, 0, time.UTC) // == 2026-09-10 00:00 in Dublin (UTC+1)
	app := mustCreate(t, svc, owner, "ShiftCo", "Role")
	if _, err := db.Pool().Exec(ctx, `UPDATE applications SET deadline=$2 WHERE id=$1`, app.ID, buggyDublin); err != nil {
		t.Fatal(err)
	}
	var stored time.Time
	if err := db.Pool().QueryRow(ctx, `SELECT deadline FROM applications WHERE id=$1`, app.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	// With the session pinned UTC, the buggy instant shifts the day to 09-09.
	if got := day.Format(stored); got != "2026-09-09" {
		t.Logf("(old-wire) stored %q (documents why the new wire is date-only)", got)
	}
	// Sanity: the fixed wire (day.Parse of the string) stores 09-10.
	correct, _ := day.Parse(picked)
	if _, err := db.Pool().Exec(ctx, `UPDATE applications SET deadline=$2 WHERE id=$1`, app.ID, correct); err != nil {
		t.Fatal(err)
	}
	_ = db.Pool().QueryRow(ctx, `SELECT deadline FROM applications WHERE id=$1`, app.ID).Scan(&stored)
	if got := day.Format(stored); got != picked {
		t.Fatalf("fixed wire stored %q, want %q", got, picked)
	}
}

// Day-only value survives the transport helper used by the actions API.
func TestDayHelperParseFormat(t *testing.T) {
	for _, s := range []string{"2026-01-01", "2024-02-29", "2000-12-31"} {
		if !day.Valid(s) {
			t.Fatalf("%q should be valid", s)
		}
		tm, err := day.Parse(s)
		if err != nil {
			t.Fatalf("%q: %v", s, err)
		}
		if day.Format(tm) != s {
			t.Fatalf("format(parse(%q)) = %q", s, day.Format(tm))
		}
	}
	for _, bad := range []string{"2026-13-01", "2026-00-10", "10/09/2026", "2026-09-10T00:00:00Z"} {
		if day.Valid(bad) {
			t.Fatalf("%q should be invalid", bad)
		}
	}
	// Empty is not a calendar day — callers treat nil/empty as “absent” before
	// ever calling Valid.
	if day.Valid("") {
		t.Fatal("empty string must not validate as a calendar day")
	}
}

var _ = fmt.Sprintf
