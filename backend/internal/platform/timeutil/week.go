// Package timeutil computes user-timezone calendar windows used by the
// dashboard, reminders and weekly reports. Weeks run [weekStart, weekStart+7d)
// in the user's zone (IANA, e.g. Europe/Dublin — the app's default); all
// bounds are half-open and DST-safe because arithmetic happens in zone-local
// wall clock before converting to UTC instants.
package timeutil

import (
	"time"
)

// StartOfDay returns midnight (00:00) of d's calendar day in loc.
func StartOfDay(d time.Time, loc *time.Location) time.Time {
	y, m, day := d.In(loc).Date()
	return time.Date(y, m, day, 0, 0, 0, 0, loc)
}

// WeekBounds returns the half-open [start, end) window of the week containing
// d, where weeks begin on weekStart (time.Sunday=0 … time.Saturday=6).
func WeekBounds(d time.Time, loc *time.Location, weekStart time.Weekday) (time.Time, time.Time) {
	dayStart := StartOfDay(d, loc)
	wd := int(dayStart.Weekday())
	// distance back to the most recent weekStart (Monday=1 when weekStart=1)
	delta := (wd - int(weekStart) + 7) % 7
	start := dayStart.AddDate(0, 0, -delta)
	end := start.AddDate(0, 0, 7)
	return start, end
}

// TodayBounds returns [today 00:00, tomorrow 00:00) in loc.
func TodayBounds(d time.Time, loc *time.Location) (time.Time, time.Time) {
	s := StartOfDay(d, loc)
	return s, s.AddDate(0, 0, 1)
}

// In returns whether t falls inside the half-open window [start, end).
func In(t, start, end time.Time) bool {
	return (t.Equal(start) || t.After(start)) && t.Before(end)
}

// NextDayBounds returns the bounds of the user-local day that follows today
// (used by "面试前一天" reminders: an interview scheduled on the next local
// day gets its reminder now).
func NextDayBounds(d time.Time, loc *time.Location) (time.Time, time.Time) {
	s := StartOfDay(d, loc)
	s = s.AddDate(0, 0, 1)
	return s, s.AddDate(0, 0, 1)
}

// DateOnly renders a zone-local date key (YYYY-MM-DD) for the instant.
func DateOnly(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("2006-01-02")
}

// DaysSince returns whole calendar days between t and now in loc, counting
// local-midnight boundaries — robust across DST transitions where a real day
// is 23 or 25 hours (dividing an elapsed duration by 24h would be off by one
// across a spring-forward).
func DaysSince(t, now time.Time, loc *time.Location) int {
	ts := StartOfDay(t, loc)
	ns := StartOfDay(now, loc)
	// Walk whole local days (AddDate keeps the local wall clock, so DST shifts
	// never skew the count) instead of dividing elapsed hours by 24.
	days := 0
	cursor := ts
	if ns.After(ts) || ns.Equal(ts) {
		for cursor.Before(ns) {
			cursor = cursor.AddDate(0, 0, 1)
			days++
			if days > 40000 {
				break // safety valve (~110 years)
			}
		}
		return days
	}
	for cursor.After(ns) {
		cursor = cursor.AddDate(0, 0, -1)
		days--
		if days < -40000 {
			break
		}
	}
	return days
}
