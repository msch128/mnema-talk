import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { setLocale } from '../i18n'
import ParticipantTile from './ParticipantTile.vue'

const user = { id: 'a', username: 'alice', display_name: 'Alice with a rather long display name', role: 'admin', joined_at: '2026-01-01T10:00:00Z' }

function tile(props = {}) {
  return mount(ParticipantTile, { props: { user, ...props } })
}

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
})

describe('ParticipantTile name', () => {
  it('is centered under the avatar, with the status line', () => {
    for (const compact of [false, true]) {
      const w = tile({ compact, speaking: true })
      const row = w.find('[data-tile-name]')
      expect(row.classes()).toEqual(expect.arrayContaining(['w-full', 'justify-center']))
      expect(row.classes()).not.toContain('absolute')
      // Long names are cut with an ellipsis, still centered.
      const inner = row.find('div')
      expect(inner.classes()).toEqual(expect.arrayContaining(['justify-center', 'min-w-0']))
      expect(inner.find('span').classes()).toContain('truncate')
      expect(inner.text()).toContain('Admin')
      const status = w.find('[data-tile-status]')
      expect(status.classes()).toEqual(expect.arrayContaining(['w-full', 'text-center']))
    }
  })

  it('on camera is a pill centered at the bottom, with the time next to it', () => {
    const w = tile({ stream: new MediaStream(), localMuted: true })
    const row = w.find('[data-tile-name]')
    expect(row.classes()).toEqual(expect.arrayContaining(['absolute', 'inset-x-2', 'bottom-2', 'justify-center']))
    expect(row.classes()).not.toContain('left-2')
    const pill = row.find('div')
    expect(pill.classes()).toEqual(expect.arrayContaining(['bg-black/70', 'min-w-0']))
    expect(pill.find('span').classes()).toContain('truncate')
    expect(row.find('[data-voice-timer]').exists()).toBe(true)
    expect(w.find('[data-tile-status]').exists()).toBe(false)
  })
})

describe('ParticipantTile click', () => {
  it('a camera that can go on the stage is focused, otherwise the profile opens', async () => {
    const cam = tile({ stream: new MediaStream(), cameraFocusable: true })
    expect(cam.attributes('aria-label')).toBe('Enlarge camera of Alice with a rather long display name')
    expect(cam.attributes('aria-pressed')).toBe('false')
    await cam.trigger('click')
    expect(cam.emitted('focus-camera')).toEqual([['a']])
    expect(cam.emitted('open-profile')).toBeUndefined()

    const focused = tile({ stream: new MediaStream(), cameraFocused: true })
    expect(focused.attributes('aria-pressed')).toBe('true')
    expect(focused.attributes('aria-label')).toBe('Back to everyone')

    const avatar = tile()
    expect(avatar.attributes('aria-pressed')).toBeUndefined()
    await avatar.trigger('click')
    expect(avatar.emitted('open-profile')).toHaveLength(1)
    expect(avatar.emitted('focus-camera')).toBeUndefined()
  })
})

describe('ParticipantTile status line', () => {
  it('shows the time in the Talk while speaking, without a "Speaking" label', () => {
    const status = tile({ speaking: true }).find('[data-tile-status]')
    expect(status.exists()).toBe(true)
    expect(status.text()).not.toMatch(/speaking/i)
    expect(status.text()).toMatch(/\d/)
  })

  it('shows the same status line when silent', () => {
    expect(tile({ speaking: false }).find('[data-tile-status]').text()).toMatch(/\d/)
  })
})
