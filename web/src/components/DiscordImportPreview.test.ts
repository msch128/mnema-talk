import type { DiscordimportPlan } from '../types/rest'

import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import DiscordImportPreview from './DiscordImportPreview.vue'
import { setLocale } from '../i18n'

const base: DiscordimportPlan = {
  code: 'AbCdEf123456', template_name: '', source_name: 'Source', server_name: '',
  categories: [{ source_id: '1', name: 'Empty', overwrites: [] }],
  channels: [],
  roles: [{ source_id: '5', name: 'Plain', source_name: 'Plain', color: 0, hoist: false, mentionable: false, permissions: 0, permission_keys: [], dropped: [], administrator_dropped: false, suggested_role_id: null }],
  mapped_roles: [],
  mappable_roles: [],
  everyone: { permission_keys: [], dropped: [], applied: false },
  skipped: [], dropped_bits: [], administrator_dropped: false,
  counts: { categories: 1, channels: 0, roles: 1 }, hash: 'h'
}

beforeEach(() => setLocale('en'))

describe('DiscordImportPreview', () => {
  it('shows an empty category, a role without permissions and no warnings when there are none', () => {
    const w = mount(DiscordImportPreview, { props: { plan: base } })
    expect(w.text()).toContain('Source')
    expect(w.findAll('[data-testid="import-group"]')).toHaveLength(1)
    expect(w.findAll('[data-testid="import-channel"]')).toHaveLength(0)
    expect(w.get('[data-testid="import-role"]').text()).toContain('No permissions')
    expect(w.text()).not.toContain('Not imported')
    expect(w.find('[data-testid="import-admin-dropped"]').exists()).toBe(false)
    expect(w.find('[data-testid="import-skipped"]').exists()).toBe(false)
    expect(w.text()).toContain('Discord’s @everyone')
  })

  it('shows channels with their kind, limit and notes, and the category-less ones last', () => {
    const plan: DiscordimportPlan = {
      ...base,
      channels: [
        { source_id: '2', category_source_id: '1', category: 'Empty', name: 'chat', type: 'text', source_type: 'text', topic: '', slow_mode_seconds: 0, user_limit: 0, private: false, overwrites: [], notes: [] },
        { source_id: '3', category_source_id: '', category: '', name: 'Hangout', type: 'voice', source_type: 'voice', topic: '', slow_mode_seconds: 0, user_limit: 4, private: true, overwrites: [], notes: ['a note'] }
      ],
      counts: { categories: 1, channels: 2, roles: 1 }
    }
    const w = mount(DiscordImportPreview, { props: { plan } })
    const groups = w.findAll('[data-testid="import-group"]').map(g => g.text())
    expect(groups).toHaveLength(2)
    expect(groups[0]).toContain('chat')
    expect(groups[1]).toContain('Without a category')
    expect(groups[1]).toContain('Hangout')
    expect(groups[1]).toContain('up to 4 members')
    expect(groups[1]).toContain('a note')
  })
})
