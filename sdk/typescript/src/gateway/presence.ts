// The `d` of PRESENCE_UPDATE (op 3), in Discord's format, checked early: the
// gateway closes with 4002 on a bad status or activity type.
import { ValidationError } from '../builders/validate.ts'
import type { PresenceData } from '../types/gateway.ts'

const STATUSES = new Set(['online', 'idle', 'dnd', 'invisible'])
const ACTIVITY_TYPES = new Set([0, 2, 3, 4, 5])

export interface PresenceFrame {
  since: null
  afk: false
  status: string
  activities: { name: string; type: number; state?: string }[]
}

export function presenceFrame(p: PresenceData): PresenceFrame {
  if (!STATUSES.has(p?.status)) {
    throw new ValidationError(`presence status must be online, idle, dnd or invisible, got "${String(p?.status)}"`)
  }
  const activities: PresenceFrame['activities'] = []
  if (p.activity) {
    if (!ACTIVITY_TYPES.has(p.activity.type)) {
      throw new ValidationError('activity type must be 0 playing, 2 listening, 3 watching, 4 custom or 5 competing')
    }
    const a: PresenceFrame['activities'][number] = { name: p.activity.name, type: p.activity.type }
    if (p.activity.state !== undefined) a.state = p.activity.state
    activities.push(a)
  }
  return { since: null, afk: false, status: p.status, activities }
}
