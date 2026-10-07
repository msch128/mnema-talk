import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import { setLocale } from '../i18n'
import { useVoiceStore } from '../stores/voice'
import ScreenShareModal from './ScreenShareModal.vue'

const rtc = vi.hoisted(() => ({ startScreenShare: vi.fn() }))
vi.mock('../composables/useWebRTC', () => ({ useWebRTC: () => rtc }))

let w!: ReturnType<typeof mount<typeof ScreenShareModal>>
beforeEach(() => {
  localStorage.clear()
  setLocale('de')
  setActivePinia(createPinia())
  rtc.startScreenShare.mockReset()
})
afterEach(() => {
  w?.unmount()
  document.body.innerHTML = ''
})

describe('ScreenShareModal', () => {
  it('renders presets, resolutions, and frame rates', () => {
    w = mount(ScreenShareModal)
    expect(w.find('[data-testid="preset-gaming"]').exists()).toBe(true)
    expect(w.find('[data-testid="preset-screen"]').exists()).toBe(true)
    expect(w.find('[data-testid="preset-custom"]').exists()).toBe(true)

    expect(w.find('[data-testid="resolution-btn-720"]').exists()).toBe(true)
    expect(w.find('[data-testid="resolution-btn-1080"]').exists()).toBe(true)
    expect(w.find('[data-testid="resolution-btn-1440"]').exists()).toBe(true)
    expect(w.find('[data-testid="resolution-btn-source"]').exists()).toBe(true)

    expect(w.find('[data-testid="fps-btn-15"]').exists()).toBe(true)
    expect(w.find('[data-testid="fps-btn-30"]').exists()).toBe(true)
    expect(w.find('[data-testid="fps-btn-60"]').exists()).toBe(true)
    // Source follows the chosen capture, and the negotiated codec is unknown
    // before capture/connection: neither is a guaranteed 4K/H.264 mode.
    expect(w.find('[data-testid="resolution-btn-source"]').text()).not.toContain('4K')
    expect(w.text()).not.toContain('H.264')
  })

  it('switches to gaming preset when clicked', async () => {
    w = mount(ScreenShareModal)
    await w.find('[data-testid="preset-gaming"]').trigger('click')
    await nextTick()

    // 1440p and 60fps should be selected
    const res1440 = w.find('[data-testid="resolution-btn-1440"]')
    expect(res1440.classes()).toContain('border-mnema-accent')

    const fps60 = w.find('[data-testid="fps-btn-60"]')
    expect(fps60.classes()).toContain('border-mnema-accent')
  })

  it('switches to screen preset when clicked', async () => {
    w = mount(ScreenShareModal)
    await w.find('[data-testid="preset-screen"]').trigger('click')
    await nextTick()

    const resSource = w.find('[data-testid="resolution-btn-source"]')
    expect(resSource.classes()).toContain('border-mnema-accent')

    const fps15 = w.find('[data-testid="fps-btn-15"]')
    expect(fps15.classes()).toContain('border-mnema-accent')
  })

  it('allows picking custom combinations', async () => {
    w = mount(ScreenShareModal)
    await w.find('[data-testid="resolution-btn-720"]').trigger('click')
    await w.find('[data-testid="fps-btn-60"]').trigger('click')
    await nextTick()

    const customBtn = w.find('[data-testid="preset-custom"]')
    expect(customBtn.classes()).toContain('border-mnema-accent')
  })

  it('applies quality and starts screen share on submit', async () => {
    w = mount(ScreenShareModal)
    const voice = useVoiceStore()
    voice.isScreenSharing = false

    await w.find('[data-testid="preset-gaming"]').trigger('click')
    await w.find('[data-testid="stream-modal-submit"]').trigger('click')
    await flushPromises()

    expect(voice.screenQuality.resolution).toBe(1440)
    expect(voice.screenQuality.fps).toBe(60)
    expect(rtc.startScreenShare).toHaveBeenCalled()
    expect(w.emitted('close')).toBeTruthy()
  })

  it('only saves quality without restarting if already sharing', async () => {
    w = mount(ScreenShareModal)
    const voice = useVoiceStore()
    voice.isScreenSharing = true

    await w.find('[data-testid="resolution-btn-source"]').trigger('click')
    await w.find('[data-testid="fps-btn-60"]').trigger('click')
    await w.find('[data-testid="stream-modal-submit"]').trigger('click')
    await flushPromises()

    expect(voice.screenQuality.resolution).toBe('source')
    expect(voice.screenQuality.fps).toBe(60)
    expect(rtc.startScreenShare).not.toHaveBeenCalled()
    expect(w.emitted('close')).toBeTruthy()
  })

  it('emits close when clicking cancel', async () => {
    w = mount(ScreenShareModal)
    await w.find('[data-testid="stream-modal-cancel"]').trigger('click')
    expect(w.emitted('close')).toBeTruthy()
  })

  it('initializes with quality from voiceStore', () => {
    const voice = useVoiceStore()
    voice.setScreenQuality({ resolution: 720, fps: 15, custom: true })
    w = mount(ScreenShareModal)
    const res720 = w.find('[data-testid="resolution-btn-720"]')
    expect(res720.classes()).toContain('border-mnema-accent')
    const fps15 = w.find('[data-testid="fps-btn-15"]')
    expect(fps15.classes()).toContain('border-mnema-accent')
  })
})

describe('ScreenShareModal custom selection and dismissal', () => {
  it('selects custom preset, returns to screen preset through matching values, dismisses dialog', async () => {
    w = mount(ScreenShareModal)
    await w.get('[data-testid="preset-custom"]').trigger('click')
    expect(w.get('[data-testid="preset-custom"]').classes()).toContain('border-mnema-accent')
    await w.get('[data-testid="fps-btn-15"]').trigger('click')
    await w.get('[data-testid="resolution-btn-source"]').trigger('click')
    expect(w.get('[data-testid="preset-screen"]').classes()).toContain('border-mnema-accent')
    w.findComponent({ name: 'BaseDialog' }).vm.$emit('close')
    expect(w.emitted('close')).toEqual([[]])
  })
})
