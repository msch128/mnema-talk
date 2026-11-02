import { requireValue } from '../test-fixtures.fixture'
import { fixtureId, userFixture } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import ReactionBar from './ReactionBar.vue'
import ReactionPalette from './ReactionPalette.vue'
import { useAuthStore } from '../stores/auth'
import { useEmojiStore } from '../stores/emoji'
import { usePermissionsStore } from '../stores/permissions'
import { setLocale } from '../i18n'

const reactions = [
  { emoji: '👍', count: 2, users: [fixtureId(1), fixtureId(2)] },
  { emoji: '🔥', count: 1, users: [fixtureId(2)] }
]

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  useAuthStore().user = userFixture({ id: fixtureId(1), username: 'me', role: 'user' })
})

describe('ReactionBar', () => {
  it('renders nothing without reactions', () => {
    const w = mount(ReactionBar, { props: { reactions: [] } })
    expect(w.find('button').exists()).toBe(false)
  })

  it('marks my own reactions as pressed', () => {
    const w = mount(ReactionBar, { props: { reactions } })
    const [mine, other] = w.findAll('button[aria-pressed]')
    expect(requireValue(mine).attributes('aria-pressed')).toBe('true')
    expect(requireValue(mine).classes()).toContain('text-mnema-accent')
    expect(requireValue(other).attributes('aria-pressed')).toBe('false')
  })

  it('emits toggle for a badge and toggle-picker for the add button', async () => {
    const w = mount(ReactionBar, { props: { reactions } })
    await requireValue(w.findAll('button[aria-pressed]')[1]).trigger('click')
    expect(w.emitted('toggle')).toEqual([['🔥']])
    await w.find('[data-testid="reaction-add"]').trigger('click')
    expect(w.emitted('toggle-picker')).toHaveLength(1)
  })

  it('shows the palette while open and forwards picks', async () => {
    const w = mount(ReactionBar, { props: { reactions, pickerOpen: true } })
    const palette = w.find('[data-testid="reaction-palette"]')
    expect(palette.exists()).toBe(true)
    await palette.find('button').trigger('click')
    expect(requireValue(requireValue(w.emitted('toggle'))[0])).toEqual(['👍'])
  })

  it('forwards palette dismissal and supports a smaller add icon', async () => {
    const w = mount(ReactionBar, { props: { reactions, pickerOpen: true, smallIcon: true } })
    w.findComponent(ReactionPalette).vm.$emit('close')
    expect(w.emitted('close-picker')).toHaveLength(1)
    expect(w.find('[data-testid="reaction-add"] svg').classes()).toContain('w-3.5')
  })

  it('shows no selected reaction for an anonymous reader', () => {
    useAuthStore().user = null
    const w = mount(ReactionBar, { props: { reactions } })
    expect(w.findAll('[aria-pressed]').every(button => button.attributes('aria-pressed') === 'false')).toBe(true)
  })

  it('uses smaller badges when compact', () => {
    const w = mount(ReactionBar, { props: { reactions, compact: true } })
    expect(w.find('button[aria-pressed]').classes()).toContain('text-xs')
  })
})

describe('ReactionBar with a deleted custom emoji', () => {
  const ID = '11111111-2222-4333-8444-555555555555'
  const token = `<:blob:${ID}>`
  const custom = [{ emoji: token, count: 1, users: [fixtureId(2)] }]

  it('shows :name: and cannot be added any more once the emoji list knows it is gone', () => {
    useEmojiStore().loaded = true
    const w = mount(ReactionBar, { props: { reactions: custom } })
    const badge = requireValue(w.find('button[aria-pressed]'))
    expect(badge.text()).toContain(':blob:')
    expect(badge.attributes('disabled')).toBeDefined()
  })

  it('still lets the member take their own reaction back', () => {
    useEmojiStore().loaded = true
    const w = mount(ReactionBar, { props: { reactions: [{ ...requireValue(custom[0]), users: [fixtureId(1)] }] } })
    expect(requireValue(w.find('button[aria-pressed]')).attributes('disabled')).toBeUndefined()
  })

  it('does not treat an emoji as gone before the list has loaded', () => {
    const w = mount(ReactionBar, { props: { reactions: custom } })
    expect(requireValue(w.find('button[aria-pressed]')).attributes('disabled')).toBeUndefined()
  })

  it('shows a held-back add button with the wait to a new member who may not react yet', () => {
    useAuthStore().user = userFixture({ id: fixtureId(1), username: 'me', role: 'user' })
    usePermissionsStore().info = { owner: false, permissions: 0, role_ids: [], top_position: 0, max_upload_mb: 50, catalog: [], channels: [], new_member: { react_at: new Date(Date.now() + 12 * 60_000).toISOString() } }
    const w = mount(ReactionBar, { props: { reactions, canAdd: false }, global: { directives: { tooltip: () => undefined } } })
    expect(w.find('[data-testid="reaction-add"]').exists()).toBe(false)
    expect(w.get('[data-testid="reaction-wait"] [role="img"]').attributes('aria-label')).toBe('You can react in 12 min.')
  })
})
