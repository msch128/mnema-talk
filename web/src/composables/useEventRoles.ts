// Saving the role list an editor produced: the roles that are removed with
// members go first (their members move over), then the list is set in order.
import { useEventsStore } from '../stores/events'
import { inputsOf } from '../lib/eventRoleDrafts'
import type { RoleDraft, RoleRemoval } from '../lib/eventRoleDrafts'
import type { CommunityEvent } from '../types/domain'
import { t } from '../i18n'

export function useEventRoles() {
  const events = useEventsStore()

  /** Stores the roles of an event; throws an Error with a sentence when a row is not valid yet. */
  async function save(eventId: string, rows: readonly RoleDraft[], removals: readonly RoleRemoval[]): Promise<CommunityEvent> {
    const inputs = inputsOf(rows)
    if (!inputs) throw new Error(t('events.roles.invalid'))
    for (const r of removals) await events.deleteRole(eventId, r.id, r.moveTo ?? undefined)
    return events.setRoles(eventId, inputs)
  }

  return { save }
}
