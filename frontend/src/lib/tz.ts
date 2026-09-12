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

/**
 * Inverse of toInstantInUserZone: render an instant (or "now") as the naive
 * `YYYY-MM-DDTHH:mm` string a datetime-local input expects, in the USER's zone.
 *
 * Needed so a form can PREFILL a time instead of leaving the field blank and
 * silently stamping the server clock — the user must see the value that is
 * about to be written. Built from Intl parts rather than toISOString(), which
 * would render UTC and shift the day for anyone east or west of it.
 */
export function toLocalDateTimeInput(iso?: string | null, now: Date = new Date()): string {
  const d = iso ? new Date(iso) : now
  if (isNaN(d.getTime())) return ''
  const parts = new Intl.DateTimeFormat('en-CA', {
    timeZone: effectiveZone(),
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).formatToParts(d)
  const get = (type: string) => parts.find((p) => p.type === type)?.value ?? ''
  // en-CA renders midnight as 24 in some engines; datetime-local needs 00.
  const hour = get('hour') === '24' ? '00' : get('hour')
  return `${get('year')}-${get('month')}-${get('day')}T${hour}:${get('minute')}`
}

/**
 * 「实际投递时间」留空时的兜底：现在。
 *
 * But never later than the change's own 发生时间. A submission necessarily
 * happened BEFORE the stage change being recorded, and the timeline is rendered
 * in `occurred_at` order (application_events is read with `ORDER BY occurred_at
 * ASC, sequence ASC`) — so a blank field plus a backdated 发生时间 would
 * otherwise draw 「昨天 OA → 今天 已投递」 and silently inflate 等待天数.
 */
export function defaultSubmittedIso(occurredIso: string | null, now: Date = new Date()): string {
  if (!occurredIso) return now.toISOString()
  const occ = new Date(occurredIso)
  if (isNaN(occ.getTime())) return now.toISOString()
  return new Date(Math.min(occ.getTime(), now.getTime())).toISOString()
}
