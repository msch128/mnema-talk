// Presence: what a user chose (online, away, dnd, focus) and what others see
// live (the same, or "offline" without a connection; "online" turns "away"
// while idle).

/** Presences a user can choose, in menu order. Offline is never a choice. */
export const CHOOSABLE = ['online', 'away', 'dnd', 'focus']

/** Every live status, in member-list order. */
export const STATUSES = [...CHOOSABLE, 'offline']

/** Idle time after which an "online" user shows as away. */
export const IDLE_AFTER_MS = 10 * 60 * 1000

export function isChoosable(p) {
  return CHOOSABLE.includes(p)
}

export function normalizeStatus(s) {
  return STATUSES.includes(s) ? s : 'offline'
}

/** Do not disturb and focus silence every notification. */
export function isQuiet(presence) {
  return presence === 'dnd' || presence === 'focus'
}

/**
 * The presence_snapshot payload as a map user_id → status. Older servers sent
 * a plain list of online ids.
 */
export function snapshotToMap(payload) {
  if (Array.isArray(payload)) return Object.fromEntries(payload.map(id => [id, 'online']))
  const out = {}
  for (const [id, raw] of Object.entries(payload || {})) {
    const s = normalizeStatus(raw)
    if (s !== 'offline') out[id] = s
  }
  return out
}

/** Applies one presence_update to a status map, returning a new map. */
export function applyPresenceUpdate(map, update) {
  if (!update?.user_id) return map
  const next = { ...map }
  const s = normalizeStatus(update.status)
  if (s === 'offline') delete next[update.user_id]
  else next[update.user_id] = s
  return next
}
