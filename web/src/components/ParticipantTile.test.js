import { describe, it, expect, beforeEach, vi } from 'vitest'
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

describe('ParticipantTile layout', () => {
  it('centers the avatar in the tile in every variant', () => {
    for (const props of [{}, { compact: true }, { fill: true }, { fill: true, compact: true }, { isScreensharing: true }]) {
      const w = tile(props)
      const stage = w.find('.tile-stage')
      expect(stage.classes()).toEqual(expect.arrayContaining(['absolute', 'inset-0', 'flex', 'items-center', 'justify-center']))
      // The avatar is the only thing in the centered layer: no badge, name
      // or timer shifts it.
      expect(stage.element.children).toHaveLength(1)
      expect(stage.find('[data-tile-avatar]').classes()).toEqual(expect.arrayContaining(['tile-avatar', 'rounded-full']))
      expect(w.classes()).toContain('aspect-video')
    }
  })

  it('scales the avatar with the tile and marks the compact variant', () => {
    expect(tile({ compact: true }).classes()).toContain('tile-compact')
    expect(tile().classes()).not.toContain('tile-compact')
    // The initial when there is no picture.
    expect(tile().find('[data-tile-avatar]').text()).toBe('A')
    const pic = tile({ user: { ...user, avatar_url: '/a.png' } })
    expect(pic.find('[data-tile-avatar] img').attributes('src')).toBe('/a.png')
  })

  it('speaking: the green ring around the avatar and a glowing frame', () => {
    const w = tile({ speaking: true })
    expect(w.find('[data-tile-avatar]').classes()).toEqual(expect.arrayContaining(['ring-mnema-accent']))
    expect(w.find('[data-tile-frame]').classes()).toEqual(expect.arrayContaining(['ring-2', 'ring-inset', 'ring-mnema-accent']))
    const quiet = tile()
    expect(quiet.find('[data-tile-avatar]').classes()).not.toContain('ring-mnema-accent')
    expect(quiet.find('[data-tile-frame]').classes()).not.toContain('ring-mnema-accent')
  })

  it('LIVE sits in the top-right corner, outside the avatar layer', () => {
    for (const compact of [false, true]) {
      const w = tile({ isScreensharing: true, compact })
      const badge = w.find('[data-testid="tile-live-badge"]')
      expect(badge.exists()).toBe(true)
      const corner = w.find('[data-tile-corner]')
      expect(corner.element.contains(badge.element)).toBe(true)
      expect(corner.classes()).toEqual(expect.arrayContaining(['absolute', compact ? 'top-1.5' : 'top-2', compact ? 'right-1.5' : 'right-2']))
      expect(corner.classes()).not.toContain('left-2')
      expect(w.find('.tile-stage').element.contains(badge.element)).toBe(false)
      expect(badge.find('svg').exists()).toBe(true)
    }
    expect(tile().find('[data-testid="tile-live-badge"]').exists()).toBe(false)
  })
})

describe('ParticipantTile name', () => {
  it('is centered at the bottom with Admin and the mute marks', () => {
    for (const compact of [false, true]) {
      const w = tile({ compact, muted: true, localMuted: true })
      const row = w.find('[data-tile-name]')
      expect(row.classes()).toEqual(expect.arrayContaining(['absolute', 'inset-x-0', 'justify-center']))
      expect(row.classes()).toContain(compact ? 'bottom-1.5' : 'bottom-2')
      // Long names are cut with an ellipsis, still centered.
      const inner = row.find('div')
      expect(inner.classes()).toEqual(expect.arrayContaining(['justify-center', 'min-w-0', 'max-w-full']))
      expect(inner.find('span').classes()).toContain('truncate')
      expect(inner.text()).toContain('Admin')
      expect(inner.find('[data-mute-marks]').exists()).toBe(true)
      expect(inner.findAll('svg').length).toBeGreaterThanOrEqual(2)
      // No time next to the name.
      expect(row.find('[data-voice-timer]').exists()).toBe(false)
    }
  })

  it('on camera is a pill centered at the bottom, over the video', () => {
    const w = tile({ stream: new MediaStream(), localMuted: true })
    expect(w.find('video').classes()).toEqual(expect.arrayContaining(['absolute', 'inset-0', 'object-cover']))
    expect(w.find('.tile-stage').exists()).toBe(false)
    const row = w.find('[data-tile-name]')
    expect(row.classes()).toEqual(expect.arrayContaining(['absolute', 'inset-x-0', 'bottom-2', 'justify-center']))
    expect(row.classes()).not.toContain('left-2')
    const pill = row.find('div')
    expect(pill.classes()).toEqual(expect.arrayContaining(['bg-black/70', 'min-w-0']))
    expect(pill.find('span').classes()).toContain('truncate')
    expect(row.find('[data-voice-timer]').exists()).toBe(false)
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

describe('ParticipantTile time in the Talk', () => {
  const REVEALED_BY = ['opacity-0', 'group-hover:opacity-100', 'group-focus-visible:opacity-100', 'group-has-[:focus-visible]:opacity-100']

  it('is hidden until hover or keyboard focus, on avatars and cameras', () => {
    for (const props of [{}, { speaking: true }, { compact: true }, { stream: new MediaStream() }]) {
      const status = tile(props).find('[data-tile-status]')
      expect(status.exists()).toBe(true)
      expect(status.text()).toMatch(/\d+:\d\d/)
      expect(status.text()).not.toMatch(/speaking/i)
      expect(status.classes()).toEqual(expect.arrayContaining([...REVEALED_BY, 'absolute', 'pointer-events-none']))
    }
  })

  it('sits in the top-left corner, away from LIVE', () => {
    const w = tile({ isScreensharing: true })
    expect(w.find('[data-tile-status]').classes()).toEqual(expect.arrayContaining(['top-2', 'left-2']))
    expect(w.find('[data-tile-corner]').element.contains(w.find('[data-tile-status]').element)).toBe(false)
  })

  it('is not shown in the preview', () => {
    expect(tile({ showStatus: false }).find('[data-tile-status]').exists()).toBe(false)
  })
})

describe('ParticipantTile streaming', () => {
  it('offers to watch on hover or focus; a click on the tile watches too', async () => {
    const w = tile({ isScreensharing: true })
    const overlay = w.find('[data-tile-watch-overlay]')
    expect(overlay.classes()).toEqual(expect.arrayContaining(['opacity-0', 'group-hover:opacity-100', 'group-has-[:focus-visible]:opacity-100']))
    await w.get('[data-testid="tile-watch-button"]').trigger('click')
    expect(w.emitted('watch-stream')).toEqual([['a']])
    expect(w.emitted('open-profile')).toBeUndefined()
    await w.trigger('click')
    expect(w.emitted('watch-stream')).toHaveLength(2)

    const watching = tile({ isScreensharing: true, isWatching: true })
    await watching.get('[data-testid="tile-stop-watching-button"]').trigger('click')
    expect(watching.emitted('stop-watching')).toEqual([['a']])
  })

  it('my own share: LIVE, but nothing to watch', () => {
    const w = tile({ isScreensharing: true, isSelf: true })
    expect(w.find('[data-testid="tile-live-badge"]').exists()).toBe(true)
    expect(w.find('[data-tile-watch-overlay]').exists()).toBe(false)
  })
})

describe('ParticipantTile double-click', () => {
  it('a camera: the first click focuses at once, the second leaves it to the dblclick (full screen)', async () => {
    const cam = tile({ stream: new MediaStream(), cameraFocusable: true })
    await cam.trigger('click', { detail: 1 })
    await cam.trigger('click', { detail: 2 })
    await cam.trigger('dblclick')
    expect(cam.emitted('focus-camera')).toEqual([['a']])
    expect(cam.emitted('fullscreen-camera')).toEqual([['a']])
  })

  it('keyboard activation (no click count) still focuses', async () => {
    const cam = tile({ stream: new MediaStream(), cameraFocusable: true })
    await cam.trigger('keydown', { key: 'Enter' })
    expect(cam.emitted('focus-camera')).toEqual([['a']])
  })

  it('stops the dblclick so the Talk area does not handle it again', async () => {
    const outer = vi.fn()
    const w = mount({
      components: { ParticipantTile },
      template: '<div @dblclick="outer"><ParticipantTile :user="user" :stream="stream" camera-focusable /></div>',
      data: () => ({ user, stream: new MediaStream() }),
      methods: { outer }
    })
    await w.get('[data-participant-tile]').trigger('dblclick')
    expect(outer).not.toHaveBeenCalled()
  })

  it('an avatar or a button on the tile does not go full screen', async () => {
    const avatar = tile({ cameraFocusable: true })
    await avatar.trigger('dblclick')
    expect(avatar.emitted('fullscreen-camera')).toBeUndefined()

    const cam = tile({ stream: new MediaStream(), cameraFocusable: true, cameraAvailable: true })
    await cam.get('button').trigger('dblclick')
    expect(cam.emitted('fullscreen-camera')).toBeUndefined()
  })

  it('a fill tile is 16:9 without a fixed size, the others 16:9 with their own width', () => {
    const w = tile({ fill: true })
    expect(w.classes()).toContain('aspect-video')
    expect(w.classes().filter(c => /^[wh]-/.test(c))).toEqual([])
    expect(w.attributes('data-user-id')).toBe('a')
    expect(tile({ compact: true }).classes()).toEqual(expect.arrayContaining(['aspect-video', 'w-44']))
    expect(tile({ stream: new MediaStream() }).classes()).toEqual(expect.arrayContaining(['aspect-video', 'w-72']))
  })
})
