import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { categoryFixture, channelFixture } from '../test-fixtures.fixture'
import VoiceStatusPanel from './VoiceStatusPanel.vue'
const rtc = vi.hoisted(() => ({ leaveVoiceChannel: vi.fn(), startScreenShare: vi.fn(), stopScreenShare: vi.fn() }))
vi.mock('../composables/useWebRTC', () => ({ useWebRTC: () => rtc }))
let wrapper: ReturnType<typeof mount<typeof VoiceStatusPanel>> | undefined
beforeEach(() => { setActivePinia(createPinia()); vi.clearAllMocks() })
afterEach(() => wrapper?.unmount())
describe('VoiceStatusPanel', () => {
  it('shows absent channel/unknown latency and opens voice/stats', async () => {
    const voice = useVoiceStore(); voice.ping = null; voice.pingHistory = []
    wrapper = mount(VoiceStatusPanel)
    expect(wrapper.get('[data-testid="voice-panel-ping"]').text()).toContain('– ms')
    await wrapper.get('[data-testid="voice-panel-open"]').trigger('click'); expect(voice.activeView).toBe('voice')
    await wrapper.get('[data-testid="voice-panel-ping"]').trigger('click'); expect(voice.showStatsModal).toBe(true)
  })
  it('prefers current latency then average and dispatches sharing/audio/leave controls', async () => {
    const voice = useVoiceStore(); voice.setChannel('v')
    useChatStore().categories = [categoryFixture({ channels: [channelFixture({ id: 'v', name: 'Lounge', type: 'voice' })] })]
    voice.ping = 40; voice.pingHistory = [50]; wrapper = mount(VoiceStatusPanel)
    expect(wrapper.text()).toContain('Lounge'); expect(wrapper.get('[data-testid="voice-panel-ping"]').text()).toContain('40 ms')
    await wrapper.get('[data-testid="voice-panel-share"]').trigger('click'); expect(rtc.startScreenShare).toHaveBeenCalledOnce()
    voice.isScreenSharing = true; voice.ping = null; await wrapper.vm.$nextTick()
    expect(wrapper.get('[data-testid="voice-panel-ping"]').text()).toContain('50 ms')
    await wrapper.get('[data-testid="voice-panel-share"]').trigger('click'); expect(rtc.stopScreenShare).toHaveBeenCalledOnce()
    const audio = wrapper.get('[data-testid="voice-panel-stream-audio"]')
    await audio.trigger('click'); expect(audio.attributes('aria-pressed')).toBe('true')
    await audio.trigger('click'); expect(audio.attributes('aria-pressed')).toBe('false')
    await wrapper.get('[data-testid="voice-panel-leave"]').trigger('click'); expect(rtc.leaveVoiceChannel).toHaveBeenCalledOnce()
  })
})
