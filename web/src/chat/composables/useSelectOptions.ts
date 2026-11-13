// The choices of a select menu: a string select's own options, or the members,
// roles and channels this member can see for the user, role and channel
// selects. The server checks every value again (and that the bot may know it).
import { computed } from 'vue'
import { useChatStore } from '../../stores/chat'
import { usePermissionsStore } from '../../stores/permissions'
import { shownName } from '../../shared/lib/deletedUser'
import { roleColorCss } from '../../shared/lib/permissions'
import { CHANNEL_SELECT, ROLE_SELECT, STRING_SELECT, stringOptions, USER_SELECT, type Component } from '../lib/components'

export interface SelectChoice { value: string; label: string; description?: string; emoji?: string; color?: string; prefix?: string }

/** Choices shown at most at once; a search narrows the rest. */
export const MAX_SHOWN = 50

export function useSelectOptions(component: () => Component) {
  const chat = useChatStore()
  const permissions = usePermissionsStore()

  const choices = computed<SelectChoice[]>(() => {
    const c = component()
    if (c.type === STRING_SELECT) {
      return stringOptions(c).map(o => ({ value: o.value, label: o.label, ...(typeof o.description === 'string' && o.description ? { description: o.description } : {}), ...(typeof o.emoji?.name === 'string' && o.emoji.name ? { emoji: o.emoji.name } : {}) }))
    }
    if (c.type === USER_SELECT) {
      return chat.members.map(m => ({ value: m.id, label: shownName(m), description: `@${m.username}` }))
    }
    if (c.type === ROLE_SELECT) {
      return permissions.roles.filter(r => !r.is_everyone && !r.managed).map(r => ({ value: r.id, label: r.name, color: roleColorCss(r.color) }))
    }
    if (c.type === CHANNEL_SELECT) {
      return chat.allChannels.filter(ch => ch.type !== 'voice').map(ch => ({ value: ch.id, label: ch.name, prefix: '#' }))
    }
    return []
  })

  /** Only the string select has a short list of its own; the others are searched. */
  const searchable = computed(() => component().type !== STRING_SELECT)
  const defaults = computed(() => stringOptions(component()).filter(o => o.default === true).map(o => o.value))

  function filtered(query: string): SelectChoice[] {
    const q = query.trim().toLowerCase()
    const hits = q ? choices.value.filter(c => c.label.toLowerCase().includes(q) || c.description?.toLowerCase().includes(q)) : choices.value
    return hits.slice(0, MAX_SHOWN)
  }

  return { choices, searchable, defaults, filtered }
}
