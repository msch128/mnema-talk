// Saving the role list an editor produced: the roles that are removed with
// members go first (their members move over), then the list is set in order.
import { useEventsStore } from '../stores/events'
import { inputsOf, draftsOf } from '../lib/eventRoleDrafts'
import type { RoleDraft, RoleRemoval } from '../lib/eventRoleDrafts'
import { isApiError } from '../lib/api'
import type { CommunityEvent } from '../types/domain'
import { t } from '../i18n'

/** What is left to do after a save that stopped half way: the removals not yet sent and the rows as the server knows them. */
export interface RolesState {
  rows: RoleDraft[]
  removals: RoleRemoval[]
}

/** The rows with the member counts of the event as it stands; a row whose role is gone becomes a new one. */
function refreshed(rows: readonly RoleDraft[], event: CommunityEvent): RoleDraft[] {
  const known = new Map(draftsOf(event.roles ?? []).map(d => [d.id, d]))
  return rows.map(row => {
    if (!row.id) return row
    const now = known.get(row.id)
    if (now) return { ...row, members: now.members }
    const fresh: RoleDraft = { ...row, members: 0 }
    delete fresh.id
    return fresh
  })
}

export function useEventRoles() {
  const events = useEventsStore()

  /** A removal that finds the role gone is done. */
  async function remove(eventId: string, r: RoleRemoval) {
    try {
      await events.deleteRole(eventId, r.id, r.moveTo ?? undefined)
    } catch (err) {
      if (!isApiError(err) || err.status !== 404) throw err
    }
  }

  /**
   * Stores the roles of an event; throws an Error with a sentence when a row is not valid yet.
   * Each removal leaves the pending list when it went through, and when anything fails
   * `sync` gets what is left (and the member counts as the server has them), so a retry
   * does not send the same removal twice.
   */
  async function save(eventId: string, rows: readonly RoleDraft[], removals: readonly RoleRemoval[], sync?: (state: RolesState) => void): Promise<CommunityEvent> {
    const inputs = inputsOf(rows)
    if (!inputs) throw new Error(t('events.roles.invalid'))
    let pending = [...removals]
    try {
      for (const r of removals) {
        await remove(eventId, r)
        pending = pending.filter(p => p.id !== r.id)
      }
      return await events.setRoles(eventId, inputs)
    } catch (err) {
      let current = [...rows]
      try {
        current = refreshed(rows, await events.fetchDetail(eventId))
      } catch { /* the editor keeps what it has */ }
      sync?.({ rows: current, removals: pending })
      throw err
    }
  }

  return { save }
}
