import { requireValue } from '../test-fixtures.fixture'
import { fixtureId, userFixture } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import ReactionBar from './ReactionBar.vue'
import ReactionPalette from './ReactionPalette.vue'
import { useAuthStore } from '../stores/auth'

const reactions = [
  { emoji: '👍', count: 2, users: [fixtureId(1), fixtureId(2)] },
  { emoji: '🔥', count: 1, users: [fixtureId(2)] }
]

beforeEach(() => {
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
