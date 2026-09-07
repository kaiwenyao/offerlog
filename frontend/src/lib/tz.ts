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
  userZone = zone && /^[A-Za-z_]+(?:\/[A-Za-z_+-]+)+$/.test(zone) ? zone : ''
}

/** The effective Intl timezone token for formatting ('' → browser default). */
export function effectiveZone(): string | undefined {
  return userZone || undefined
}
