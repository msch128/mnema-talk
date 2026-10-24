// Quick access presets for a Talk. They only rewrite the Talk's permission
// overwrites (the same list the permissions dialog edits) and its member limit;
// there is no separate permission model behind them.
import type { Overwrite } from '../types/domain'
import { PERMISSION_BITS } from './permissions'

/** What "may join" means for a Talk: seeing it and connecting to it. */
const JOIN = PERMISSION_BITS.view_channel | PERMISSION_BITS.connect

export type AccessPreset = 'everyone' | 'roles'

function isEveryone(o: Overwrite, everyoneId: string): boolean {
  return o.type === 'role' && o.id === everyoneId
}

/** Entries that carry something, in a stable order. */
function tidy(entries: Overwrite[]): Overwrite[] {
  return entries.filter(o => o.allow !== 0 || o.deny !== 0)
}

/** "Everyone": @everyone may see and join again; roles that were let in one by one need no entry any more. */
export function presetEveryone(entries: Overwrite[], everyoneId: string): Overwrite[] {
  return tidy(entries.map(o => {
    if (isEveryone(o, everyoneId)) return { ...o, deny: o.deny & ~JOIN }
    if (o.type === 'role') return { ...o, allow: o.allow & ~JOIN }
    return o
  }))
}

/**
 * "Only these roles": @everyone may neither see nor join, the chosen roles may.
 * Roles that were let in before and are not chosen now lose that. Entries for
 * single members are left as they are.
 */
export function presetOnlyRoles(entries: Overwrite[], everyoneId: string, roleIds: string[]): Overwrite[] {
  const chosen = new Set(roleIds)
  const next = entries.map(o => {
    if (isEveryone(o, everyoneId)) return { ...o, allow: o.allow & ~JOIN, deny: o.deny | JOIN }
    if (o.type !== 'role') return o
    if (chosen.has(o.id)) return { ...o, allow: o.allow | JOIN, deny: o.deny & ~JOIN }
    return { ...o, allow: o.allow & ~JOIN }
  })
  if (!next.some(o => isEveryone(o, everyoneId))) next.push({ type: 'role', id: everyoneId, allow: 0, deny: JOIN })
  for (const id of chosen) {
    if (!next.some(o => o.type === 'role' && o.id === id)) next.push({ type: 'role', id, allow: JOIN, deny: 0 })
  }
  return tidy(next)
}

/** Which preset the overwrites amount to: @everyone is shut out of the Talk, or not. */
export function currentPreset(entries: Overwrite[], everyoneId: string): AccessPreset {
  const everyone = entries.find(o => isEveryone(o, everyoneId))
  return everyone && (everyone.deny & JOIN) !== 0 ? 'roles' : 'everyone'
}

/** The roles (other than @everyone) that are let into the Talk. */
export function rolesLetIn(entries: Overwrite[], everyoneId: string): string[] {
  return entries.filter(o => o.type === 'role' && o.id !== everyoneId && (o.allow & JOIN) === JOIN).map(o => o.id)
}
