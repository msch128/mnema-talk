import type { DiscordimportPlan } from '../../types/rest'

import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import DiscordImportPreview from './DiscordImportPreview.vue'
import { setLocale, t } from '../../i18n'
import { IMPORT_NOTE_CODES, IMPORT_SKIP_CODES } from '../lib/discordImportHints'

const base: DiscordimportPlan = {
  code: 'AbCdEf123456', template_name: '', source_name: 'Source', server_name: '',
  categories: [{ source_id: '1', name: 'Empty', overwrites: [] }],
  channels: [],
  roles: [{ source_id: '5', name: 'Plain', source_name: 'Plain', color: 0, hoist: false, mentionable: false, permissions: 0, permission_keys: [], dropped: [], administrator_dropped: false, suggested_role_id: null }],
  mapped_roles: [],
  mappable_roles: [],
  everyone: { permission_keys: [], dropped: [], applied: false },
  skipped: [], skipped_hints: [], dropped_bits: [], administrator_dropped: false,
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
        { source_id: '2', category_source_id: '1', category: 'Empty', name: 'chat', type: 'text', source_type: 'text', topic: '', slow_mode_seconds: 0, user_limit: 0, private: false, overwrites: [], notes: [], note_hints: [] },
        { source_id: '3', category_source_id: '', category: '', name: 'Hangout', type: 'voice', source_type: 'voice', topic: '', slow_mode_seconds: 0, user_limit: 4, private: true, overwrites: [], notes: ['a note'], note_hints: [] }
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

  it('has a translation in both locales for every hint code the server can send', () => {
    const keys = [
      ...IMPORT_NOTE_CODES.map(c => `discordImport.note.${c}`),
      ...IMPORT_SKIP_CODES.map(c => `discordImport.skipped.${c}`)
    ]
    for (const l of ['en', 'de'] as const) {
      setLocale(l)
      for (const key of keys) expect(t(key, { seconds: 1, name: 'x', type: 3 }), `${l} ${key}`).not.toBe(key)
    }
  })

  const withHints = (): DiscordimportPlan => ({
    ...base,
    channels: [{ source_id: '2', category_source_id: '', category: '', name: 'Stage', type: 'voice', source_type: 'stage', topic: '', slow_mode_seconds: 0, user_limit: 0, private: false, overwrites: [], notes: ['a stage channel becomes a Talk', 'slow mode is capped at 21600 seconds'], note_hints: [{ code: 'stage_to_talk', params: {} }, { code: 'slow_mode_capped', params: { seconds: 21600 } }] }],
    skipped: ['a thread (Discord channel type 11 has no counterpart)'],
    skipped_hints: [{ code: 'channel_type_unsupported', params: { name: 'a thread', type: 11 } }]
  })

  it('shows notes and skipped channels in the German admin\'s language', () => {
    setLocale('de')
    const w = mount(DiscordImportPreview, { props: { plan: withHints() } })
    expect(w.text()).toContain(t('discordImport.note.stage_to_talk'))
    expect(w.text()).toContain(t('discordImport.note.slow_mode_capped', { seconds: 21600 }))
    expect(w.get('[data-testid="import-skipped"]').text()).toContain(t('discordImport.skipped.channel_type_unsupported', { name: 'a thread', type: 11 }))
    expect(w.text()).not.toContain('becomes a Talk')
  })

  it('shows the server text for a code this client does not know', () => {
    setLocale('de')
    const plan = withHints()
    plan.channels[0]!.note_hints = [{ code: 'from_a_newer_server', params: {} }, { code: 'slow_mode_capped', params: { seconds: 21600 } }]
    plan.skipped_hints = []
    const w = mount(DiscordImportPreview, { props: { plan } })
    expect(w.text()).toContain('a stage channel becomes a Talk')
    expect(w.get('[data-testid="import-skipped"]').text()).toContain('a thread (Discord channel type 11 has no counterpart)')
  })
})
