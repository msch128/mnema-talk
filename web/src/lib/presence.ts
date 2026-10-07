// Presence: what a user chose (online, away, dnd, focus) and what others see
// live (the same, or "offline" without a connection; "online" turns "away"
// while idle).

/** Presences a user can choose, in menu order. Offline is never a choice. */
import type { ChosenPresence, Presence } from '../types/domain'
import { isRecord } from '../types/validation'
export const CHOOSABLE: readonly ChosenPresence[] = ['online', 'away', 'dnd', 'focus']

/** Every live status, in member-list order. */
export const STATUSES: readonly Presence[] = [...CHOOSABLE, 'offline']

/** Idle time after which an "online" user shows as away. */
export const IDLE_AFTER_MS = 10 * 60 * 1000

export function normalizeStatus(s: unknown): Presence {
  return STATUSES.find(status => status === s) || 'offline'
}

/** Do not disturb and focus silence every notification. */
export function isQuiet(presence: unknown): boolean {
  return presence === 'dnd' || presence === 'focus'
}

/**
 * The presence_snapshot payload as a map user_id → status. Older servers sent
 * a plain list of online ids.
 */
export function snapshotToMap(payload: unknown): Record<string, Presence> {
  if (Array.isArray(payload)) return Object.fromEntries(payload.filter((id: unknown): id is string => typeof id === 'string').map(id => [id, 'online']))
  const out: Record<string, Presence> = {}
  for (const [id, raw] of Object.entries(isRecord(payload) ? payload : {})) {
    const s = normalizeStatus(raw)
    if (s !== 'offline') out[id] = s
  }
  return out
}

/** Applies one presence_update to a status map, returning a new map. */
export function applyPresenceUpdate(map: Record<string, Presence>, update: { user_id: string; status?: unknown } | null | undefined): Record<string, Presence> {
  if (!update?.user_id) return map
  const next = { ...map }
  const s = normalizeStatus(update.status)
  if (s === 'offline') delete next[update.user_id]
  else next[update.user_id] = s
  return next
}
