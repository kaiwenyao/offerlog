// Package day defines the wire representation of DATE-backed columns.
//
// A "date-only" value is a calendar day (YYYY-MM-DD) — never an instant. It is
// the user's day, independent of any timezone, and must not be serialized as a
// timestamp (a UTC-midnight timestamp renders as the previous day in
// west-of-UTC browsers, and sending a browser-local midnight instant shifts
// the stored day for east-of-UTC users).
//
// Every DATE column in the schema (actions.due_date, actions via due_ts null,
// applications.deadline, applications.next_action_due_at) travels on the wire
// as a date-only string. Internally the repository keeps a time.Time with the
// session timezone pinned to UTC so the day survives the round-trip; the
// transport converts at the boundary.
package day

import (
	"fmt"
	"strings"
	"time"
)

// Layout is the canonical wire/display format.
const Layout = "2006-01-02"

// Valid reports whether s is a strict YYYY-MM-DD calendar date.
func Valid(s string) bool {
	_, err := time.Parse(Layout, s)
	return err == nil
}

// Parse interprets s as a calendar day and returns a UTC-midnight instant that
// represents that day for storage into a DATE column.
func Parse(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(Layout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("日期格式需为 YYYY-MM-DD：%q", s)
	}
	return t, nil
}

// Format renders a DATE-derived time.Time (which the driver returns at UTC
// midnight) as a date-only string. Any time-of-day/zone component is ignored —
// only the calendar day matters.
func Format(t time.Time) string {
	return t.UTC().Format(Layout)
}

// Normalize trims whitespace; empty stays empty.
func Normalize(s string) string { return strings.TrimSpace(s) }
