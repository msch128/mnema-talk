import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ConnectionStatsModal from './ConnectionStatsModal.vue'
import { useVoiceStore } from '../stores/voice'
import { summarizeStats } from '../lib/rtcStats'

enableAutoUnmount(afterEach)
beforeEach(() => setActivePinia(createPinia()))

describe('ConnectionStatsModal stable ping histories', () => {
  it.each([
    { history: [0, 0], points: '0.0,46.0 280.0,46.0' },
    { history: [10, 10], points: '0.0,29.2 280.0,29.2' },
  ])('draws a finite horizontal line for $history', ({ history, points }) => {
    useVoiceStore().pingHistory = history
    const wrapper = mount(ConnectionStatsModal)
    expect(wrapper.get('polyline').attributes('points')).toBe(points)
  })
})

describe('ConnectionStatsModal received video', () => {
  it('shows the size and frame rate of what arrives, or a dash', async () => {
    const voice = useVoiceStore()
    const wrapper = mount(ConnectionStatsModal)
    const row = () => wrapper.findAll('.divide-y > div').find(r => r.text().startsWith('Video received') || r.text().startsWith('Empfangenes Video'))
    expect(row()?.text()).toContain('–')
    voice.rtcStats = summarizeStats([
      { id: 'a', type: 'inbound-rtp', kind: 'video', frameWidth: 1280, frameHeight: 720, framesPerSecond: 30 },
      { id: 'b', type: 'inbound-rtp', kind: 'video', frameWidth: 640, frameHeight: 360, framesPerSecond: 15 },
    ])
    await wrapper.vm.$nextTick()
    expect(row()?.text()).toContain('1280×720 · 30 fps, 640×360 · 15 fps')
    voice.rtcStats = summarizeStats([])
    await wrapper.vm.$nextTick()
    expect(row()?.text()).toContain('–')
  })
})
