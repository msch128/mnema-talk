// The role list an event editor works on before it is sent: rows with a stable
// key, a capacity that may be switched off, and the roles removed meanwhile.
import type { CommunityeventsEventRole as EventRole, CommunityeventsRoleInput as RoleInput } from '../types/rest'

export const MAX_EVENT_ROLES = 12
export const MAX_ROLE_NAME = 40
export const MAX_ROLE_CAPACITY = 1000

export interface RoleDraft {
  /** Identifies the row in the editor. */
  key: string
  /** The server's id of a role that exists already. */
  id?: string
  name: string
  emoji: string
  /** Unlimited when null. */
  capacity: number | null
  /** Members who hold or await the role (of an existing one). */
  members: number
}

/** A role taken out of the list; its members go to moveTo when it had any. */
export interface RoleRemoval {
  id: string
  name: string
  moveTo: string | null
}

let counter = 0
const nextKey = () => `draft-${++counter}`

export function draftsOf(roles: readonly EventRole[]): RoleDraft[] {
  return roles.map(r => ({ key: nextKey(), id: r.id, name: r.name, emoji: r.emoji ?? '', capacity: r.capacity ?? null, members: r.confirmed + r.waitlist }))
}

export function blankDraft(): RoleDraft {
  return { key: nextKey(), name: '', emoji: '', capacity: null, members: 0 }
}

/** Template roles appended to the list, as far as the limit and the names allow. */
export function withTemplate(drafts: readonly RoleDraft[], roles: readonly RoleInput[]): RoleDraft[] {
  const out = [...drafts]
  const names = new Set(out.map(d => d.name.trim().toLowerCase()))
  for (const r of roles) {
    if (out.length >= MAX_EVENT_ROLES) break
    if (names.has(r.name.trim().toLowerCase())) continue
    names.add(r.name.trim().toLowerCase())
    out.push({ key: nextKey(), name: r.name, emoji: r.emoji ?? '', capacity: r.capacity ?? null, members: 0 })
  }
  return out
}

/** The rows the server may take: named, with a capacity within bounds. Null when a row is not valid yet. */
export function inputsOf(drafts: readonly RoleDraft[], keepIds = true): RoleInput[] | null {
  const out: RoleInput[] = []
  const seen = new Set<string>()
  for (const d of drafts) {
    const name = d.name.trim()
    const key = name.toLowerCase()
    if (!name || [...name].length > MAX_ROLE_NAME || seen.has(key)) return null
    if (d.capacity != null && (!Number.isInteger(d.capacity) || d.capacity < 1 || d.capacity > MAX_ROLE_CAPACITY)) return null
    seen.add(key)
    out.push({ ...(keepIds && d.id ? { id: d.id } : {}), name, ...(d.emoji.trim() ? { emoji: d.emoji.trim() } : {}), ...(d.capacity != null ? { capacity: d.capacity } : {}) })
  }
  return out
}
