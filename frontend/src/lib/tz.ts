import { localDateTimeToInstant } from './api'

// Module-level holder for the signed-in user's configured timezone (IANA).
// The backend computes week windows / day indexes in this zone; client-side
// helpers that bucket or compare *instants* should use the same zone so a
// browser in another timezone does not disagree with the server's calendar.
//
// Defaults to the browser's own zone (''), which matches the legacy behavior
// when the preference has not been loaded yet.

let userZone = ''

/** IANA zone of the signed-in user; '' means "use the browser's zone". */
export function getUserZone(): string {
  return userZone
}

export function setUserZone(zone: string | null | undefined) {
  // IANA names are Region/City (Europe/Dublin); fixed-offset zones (UTC, GMT)
  // are bare names without a slash and must be honored too, or a
  // UTC-preferenced user silently falls back to the browser zone.
  const ok =
    zone != null &&
    (/^[A-Za-z_]+(?:\/[A-Za-z_+-]+)+$/.test(zone) || zone === 'UTC' || zone === 'GMT')
  userZone = ok ? zone : ''
}

/** The effective Intl timezone token for formatting ('' → browser default). */
export function effectiveZone(): string | undefined {
  return userZone || undefined
}

/**
 * Turn a `datetime-local` value (a NAIVE wall-clock string with no zone) into
 * an ISO instant, interpreting it in the USER's configured zone rather than the
 * browser's: a Dublin browser + Shanghai user typing 14:30 must store 14:30
 * Shanghai, not Dublin 14:30 = Shanghai 21:30 — otherwise reminder days and
 * calendar buckets shift.
 *
 * Returns the instant plus the zone label it was parsed in, so callers that
 * persist a timezone tag (interview rounds) stamp the same zone they parsed in.
 * `iso` is null for an empty input and for an unparseable one — callers that
 * must distinguish the two should check the raw value first.
 */
export function toInstantInUserZone(value: string): { iso: string | null; zone: string } {
  const zone = effectiveZone() ?? Intl.DateTimeFormat().resolvedOptions().timeZone
  if (!value) return { iso: null, zone }
  const ms = localDateTimeToInstant(value, zone)
  return { iso: ms == null ? null : new Date(ms).toISOString(), zone }
}
