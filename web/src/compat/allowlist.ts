// Where this web app knowingly asks more of an older server than it promised,
// and why that is safe. The rule is in doc/api-stability.md: a new request
// field, a new endpoint or a new required answer field needs a health feature
// or an entry here with a fallback. Entries are not a way to silence a test:
// each one names what the app does instead on the older server.

/**
 * Answers whose required keys grew after a release, so the strict guard of this app refuses what that release
 * sends. Each entry holds only for the named releases (a newer baseline failing is a regression), and the test
 * checks the entry is still needed. The effect on such a server is the admin or settings view that reads the
 * answer showing its load error; chat itself is unaffected unless the reason says so.
 */
export const responseGaps: ReadonlyArray<{ schema: string, versions: readonly string[], reason: string }> = []

/** Endpoints newer than some releases. `expectOldStatus` is what such a server answers; the app must cope with it (hide the feature or fall back). */
export const newEndpoints: ReadonlyArray<{ method: string, path: string, expectOldStatus: number, reason: string }> = [
  { method: 'GET', path: '/api/threads/following', expectOldStatus: 404, reason: 'asked on every connect; a 404 leaves the followed list unknown and the notification rules as they were (stores/chat/threadFollow.ts)' },
  { method: 'GET', path: '/api/admin/vanity-link', expectOldStatus: 404, reason: 'added in 2.x; the Administrator tab then shows its load-failed state with a retry (components/AdminVanityLink.vue)' }
]

/** WebSocket events added since the oldest baseline. A client ignores unknown names, so an entry says the app copes without it. */
export const newEvents: readonly string[] = ['reports_open']
