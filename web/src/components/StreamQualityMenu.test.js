import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import { setLocale } from '../i18n'
import { useVoiceStore } from '../stores/voice'
import StreamQualityMenu from './StreamQualityMenu.vue'

const rtc = vi.hoisted(() => ({ getScreenSendStats: vi.fn(), startScreenShare: vi.fn() }))
vi.mock('../composables/useWebRTC', () => ({ useWebRTC: () => rtc }))

let w
beforeEach(() => {
  setLocale('de')
  setActivePinia(createPinia())
  rtc.getScreenSendStats.mockReset()
  rtc.startScreenShare.mockReset()
})
afterEach(() => {
  w?.unmount()
  vi.useRealTimers()
  document.body.innerHTML = ''
})

const q = sel => document.querySelector(sel)
const item = id => q(`[data-menu-item="${id}"]`)
const key = (k) => document.activeElement.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true }))

async function open() {
  w = mount(StreamQualityMenu, { attachTo: document.body })
  const voice = useVoiceStore()
  voice.hasScreenAudio = true
  await w.find('[data-testid="stream-quality-button"]').trigger('click')
  await flushPromises()
  await nextTick()
  return voice
}

async function openSub(id) {
  q(`[data-submenu-trigger="${id}"]`).click()
  await flushPromises()
  await nextTick()
}

describe('stream quality menu', () => {
  it('shows mode, resolution, frame rate, sound and advanced, without premium marks', async () => {
    await open()
    const menu = q('[role="menu"]')
    expect(menu.getAttribute('aria-label')).toBe('Stream-Qualität')
    expect(menu.textContent).toContain('Stream-Modus')
    expect(item('mode-gaming').textContent).toContain('Flüssigeres Video (1440p, 60 fps)')
    expect(item('mode-screen').textContent).toContain('Klarere Schrift (Quelle, 15 fps)')
    // A new stream: 1080p at 30 fps, which is custom.
    expect(item('mode-custom').getAttribute('aria-checked')).toBe('true')
    expect(q('[data-submenu-trigger="resolution"]').textContent).toContain('1080p')
    expect(q('[data-submenu-trigger="fps"]').textContent).toContain('30 fps')
    expect(item('mute-sound').getAttribute('role')).toBe('menuitemcheckbox')
    expect(q('[data-submenu-trigger="advanced"]').textContent).toContain('Erweitert')
    expect(menu.textContent).not.toMatch(/nitro|premium/i)
  })

  it('a preset sets both values; another value makes it custom', async () => {
    const voice = await open()
    item('mode-gaming').click()
    await nextTick()
    expect(voice.screenQuality).toEqual({ resolution: 1440, fps: 60 })
    // The menu stays open and shows the choice.
    expect(item('mode-gaming').getAttribute('aria-checked')).toBe('true')

    await openSub('resolution')
    expect([...document.querySelectorAll('[data-submenu="resolution"] [role="menuitemradio"]')].map(e => e.textContent.trim()))
      .toEqual(['720p', '1080p', '1440p', 'Quelle'])
    item('resolution-720').click()
    await nextTick()
    expect(voice.screenQuality).toEqual({ resolution: 720, fps: 60 })
    expect(item('mode-custom').getAttribute('aria-checked')).toBe('true')

    await openSub('fps')
    expect([...document.querySelectorAll('[data-submenu="fps"] [role="menuitemradio"]')].map(e => e.textContent.trim()))
      .toEqual(['15 fps', '30 fps', '60 fps'])
    item('fps-15').click()
    await nextTick()
    expect(voice.screenQuality).toEqual({ resolution: 720, fps: 15 })

    // Picking the Screen preset's values by hand shows Screen.
    await openSub('resolution')
    item('resolution-source').click()
    await nextTick()
    expect(item('mode-screen').getAttribute('aria-checked')).toBe('true')
    // Custom on purpose keeps the values.
    item('mode-custom').click()
    await nextTick()
    expect(voice.screenQuality).toEqual({ resolution: 'source', fps: 15, custom: true })
    expect(item('mode-custom').getAttribute('aria-checked')).toBe('true')
  })

  it('mutes the stream sound', async () => {
    const voice = await open()
    item('mute-sound').click()
    await nextTick()
    expect(voice.isScreenAudioMuted).toBe(true)
    expect(item('mute-sound').getAttribute('aria-checked')).toBe('true')
  })

  it('advanced shows what is sent, refreshed every second while open', async () => {
    vi.useFakeTimers()
    let bytes = 1_000_000
    let ts = 1000
    rtc.getScreenSendStats.mockImplementation(async () => [
      { id: 'c', type: 'codec', mimeType: 'video/VP8' },
      { id: 'o', type: 'outbound-rtp', kind: 'video', codecId: 'c', bytesSent: bytes, timestamp: ts, frameWidth: 1280, frameHeight: 720, framesPerSecond: 15 }
    ])
    await open()
    await openSub('advanced')
    const info = id => q(`[data-menu-info="${id}"]`)?.textContent
    await vi.waitFor(() => expect(info('codec')).toContain('VP8'))
    expect(info('size')).toContain('1280×720')
    expect(info('fps')).toContain('15 fps')
    expect(info('bitrate')).toContain('–')

    bytes += 250_000
    ts += 1000
    await vi.advanceTimersByTimeAsync(1000)
    await nextTick()
    expect(info('bitrate')).toContain('2,0 Mbit/s')
    const calls = rtc.getScreenSendStats.mock.calls.length

    // Closed: no more sampling.
    q('[role="menu"]').dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await vi.advanceTimersByTimeAsync(3000)
    expect(rtc.getScreenSendStats.mock.calls.length).toBe(calls)
  })

  it('works with the keyboard: into a submenu and back', async () => {
    const voice = await open()
    q('[role="menu"]').focus()
    key('ArrowDown')
    expect(document.activeElement).toBe(item('mode-gaming'))
    key('ArrowDown')
    key('ArrowDown')
    key('ArrowDown')
    expect(document.activeElement.getAttribute('data-submenu-trigger')).toBe('resolution')
    key('ArrowRight')
    await nextTick()
    await nextTick()
    expect(document.activeElement).toBe(item('resolution-720'))
    expect(q('[data-submenu-trigger="resolution"]').getAttribute('aria-expanded')).toBe('true')
    key('ArrowDown')
    expect(document.activeElement).toBe(item('resolution-1080'))
    key('ArrowLeft')
    await nextTick()
    await nextTick()
    expect(document.activeElement.getAttribute('data-submenu-trigger')).toBe('resolution')
    expect(q('[data-submenu="resolution"]')).toBeNull()
    // Escape in a submenu goes back one level, then closes the menu.
    key('ArrowRight')
    await nextTick()
    await nextTick()
    key('Escape')
    await nextTick()
    await nextTick()
    expect(q('[role="menu"]')).not.toBeNull()
    expect(document.activeElement.getAttribute('data-submenu-trigger')).toBe('resolution')
    key('Escape')
    await nextTick()
    expect(w.emitted()).toBeTruthy()
    expect(voice.screenQuality).toEqual({ resolution: 1080, fps: 30 })
  })

  it('offers to change the shared screen', async () => {
    await open()
    item('change-source').click()
    expect(rtc.startScreenShare).toHaveBeenCalled()
  })
})
