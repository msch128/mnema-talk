import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import ReactionBar from './ReactionBar.vue'
import { useAuthStore } from '../stores/auth'

const reactions = [
  { emoji: '👍', count: 2, users: ['me', 'u2'] },
  { emoji: '🔥', count: 1, users: ['u2'] }
]

beforeEach(() => {
  setActivePinia(createPinia())
  useAuthStore().user = { id: 'me', username: 'me', role: 'user' }
})

describe('ReactionBar', () => {
  it('renders nothing without reactions', () => {
    const w = mount(ReactionBar, { props: { reactions: [] } })
    expect(w.find('button').exists()).toBe(false)
  })

  it('marks my own reactions as pressed', () => {
    const w = mount(ReactionBar, { props: { reactions } })
    const [mine, other] = w.findAll('button[aria-pressed]')
    expect(mine.attributes('aria-pressed')).toBe('true')
    expect(mine.classes()).toContain('text-mnema-accent')
    expect(other.attributes('aria-pressed')).toBe('false')
  })

  it('emits toggle for a badge and toggle-picker for the add button', async () => {
    const w = mount(ReactionBar, { props: { reactions } })
    await w.findAll('button[aria-pressed]')[1].trigger('click')
    expect(w.emitted('toggle')).toEqual([['🔥']])
    await w.find('[data-testid="reaction-add"]').trigger('click')
    expect(w.emitted('toggle-picker')).toHaveLength(1)
  })

  it('shows the palette while open and forwards picks', async () => {
    const w = mount(ReactionBar, { props: { reactions, pickerOpen: true } })
    const palette = w.find('[data-testid="reaction-palette"]')
    expect(palette.exists()).toBe(true)
    await palette.find('button').trigger('click')
    expect(w.emitted('toggle')[0]).toEqual(['👍'])
  })

  it('uses smaller badges when compact', () => {
    const w = mount(ReactionBar, { props: { reactions, compact: true } })
    expect(w.find('button[aria-pressed]').classes()).toContain('text-xs')
  })
})
