// The role list an event form edits: the rows and the pending removals, and a key that
// says whether they differ from the roles the event has (never a flag set by hand).
import { computed, ref, watch } from 'vue'
import { draftsOf, rosterKey } from '../lib/eventRoleDrafts'
import type { RoleDraft, RoleRemoval } from '../lib/eventRoleDrafts'
import type { CommunityeventsEventRole as EventRole } from '../../types/rest'
import type { RolesState } from './useEventRoles'

export function useRosterDraft(roles: () => readonly EventRole[] | undefined) {
  const rows = ref<RoleDraft[]>(draftsOf(roles() ?? []))
  const removals = ref<RoleRemoval[]>([])
  const key = computed(() => rosterKey(rows.value, removals.value))
  const savedKey = computed(() => rosterKey(draftsOf(roles() ?? [])))

  function reset() {
    rows.value = draftsOf(roles() ?? [])
    removals.value = []
  }

  /** The roster follows the event's roles while nobody has edited it. */
  watch(savedKey, (_, previous) => { if (key.value === previous) reset() })

  /** What is left to do after a save that stopped half way. */
  function sync(state: RolesState) {
    rows.value = state.rows
    removals.value = state.removals
  }

  return { rows, removals, key, savedKey, reset, sync }
}
