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
export const responseGaps: ReadonlyArray<{ schema: string, versions: readonly string[], reason: string }> = [
  { schema: 'chat.NotificationSettings', versions: ['v1.0.0'], reason: 'the `server` level was added in 1.1; before it the notification settings view fails to load' },
  { schema: 'onboarding.Settings', versions: ['v1.0.0'], reason: 'direct_messages_enabled and session_inactivity_days were added in 1.1; the onboarding admin form fails to load' },
  { schema: 'server.Legal', versions: ['v1.0.0', 'v2.0.0'], reason: 'address_detection, clock_check (1.x) and builtin_turn (2.1) were added later; the legal notice view fails to load' },
  { schema: 'server.OpsSample', versions: ['v1.0.0', 'v2.0.0'], reason: 'egress_pps and ingress_pps were added in 2.x; the ops charts fail to load' },
  { schema: 'server.OpsStatus', versions: ['v1.0.0', 'v2.0.0'], reason: 'carries server.OpsSample' },
  { schema: 'server.VoiceHealth', versions: ['v1.0.0'], reason: 'builtin_turn was added in 2.1; the system status view fails to load' },
  { schema: 'server.SystemHealth', versions: ['v1.0.0'], reason: 'carries server.VoiceHealth' },
  { schema: 'server.SystemStatus', versions: ['v1.0.0'], reason: 'carries server.SystemHealth' },
  { schema: 'chat.ForumTag', versions: ['v2.0.0'], reason: 'moderated and position were added in 2.9; a forum post of a tagged thread fails to decode on such a server' },
  { schema: 'chat.Message', versions: ['v2.0.0'], reason: 'carries chat.ForumTag (only a message with tags is affected)' },
  { schema: 'chat.MentionsPage', versions: ['v2.0.0'], reason: 'carries chat.Message' },
  { schema: 'chat.SavedMessage', versions: ['v2.0.0'], reason: 'carries chat.Message' },
  { schema: 'chat.SearchResult', versions: ['v2.0.0'], reason: 'carries chat.Message' },
  { schema: 'chat.StartTalkResponse', versions: ['v2.0.0'], reason: 'carries chat.Message' },
  { schema: 'chat.Thread', versions: ['v2.0.0'], reason: 'carries chat.Message' },
  { schema: 'discordimport.Plan', versions: ['v2.0.0'], reason: 'carries discordimport.PlanRole' },
  { schema: 'discordimport.PlanRole', versions: ['v2.0.0'], reason: 'suggested_role_id was added in 2.9; the Discord import preview fails to load' }
]

/** Endpoints newer than some releases. `expectOldStatus` is what such a server answers; the app must cope with it (hide the feature or fall back). */
export const newEndpoints: ReadonlyArray<{ method: string, path: string, expectOldStatus: number, reason: string }> = [
  { method: 'GET', path: '/api/threads/following', expectOldStatus: 404, reason: 'asked on every connect; a 404 leaves the followed list unknown and the notification rules as they were (stores/chat/threadFollow.ts)' },
  { method: 'GET', path: '/api/admin/vanity-link', expectOldStatus: 404, reason: 'added in 2.x; the Administrator tab then shows its load-failed state with a retry (components/AdminVanityLink.vue)' }
]

/** WebSocket events added since the oldest baseline. A client ignores unknown names, so an entry says the app copes without it. */
export const newEvents: readonly string[] = ['reports_open']
