import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import MentionSuggestions from './MentionSuggestions.vue'
import type { MentionSuggestion } from '../lib/mentionQuery'
import { userFixture } from '../test-fixtures.fixture'

enableAutoUnmount(afterEach)
beforeEach(() => setActivePinia(createPinia()))

const role = { id: 'r1', name: 'Dev Team', color: 0x2da771, position: 1, permissions: 0, hoist: false, mentionable: true, is_everyone: false, max_upload_mb: null }
const items: MentionSuggestion[] = [
  { username: 'max', display_name: 'Max', user: userFixture({ id: 'u1', username: 'max', display_name: 'Max' }) },
  { username: 'Dev Team', display_name: 'Dev Team', role },
  { username: 'Plain', display_name: 'Plain', role: { ...role, id: 'r2', name: 'Plain', color: 0 } },
  { username: 'all', display_name: 'all', group: true }
]

describe('MentionSuggestions', () => {
  it('draws members, roles (with their colour) and groups, and picks on mouse down', async () => {
    const w = mount(MentionSuggestions, { props: { items, active: 1 } })
    const options = w.findAll('[role="option"]')
    expect(options).toHaveLength(4)
    expect(options[1]?.attributes('aria-selected')).toBe('true')
    expect(options[1]?.text()).toContain('Dev Team')
    expect(options[1]?.html()).toContain('style=')
    expect(options[2]?.html()).not.toContain('style=')
    expect(options[3]?.text()).toContain('@all')
    await options[1]?.trigger('mousedown')
    expect(w.emitted('pick')?.[0]).toEqual([items[1]])
    await options[2]?.trigger('mousemove')
    expect(w.emitted('hover')?.[0]).toEqual([2])
  })
})
