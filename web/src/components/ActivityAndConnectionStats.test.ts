import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import ActivityStats from './ActivityStats.vue'
import ConnectionStatsModal from './ConnectionStatsModal.vue'
import BaseDialog from './BaseDialog.vue'
import { useVoiceStore } from '../stores/voice'
import { fixtureId, userFixture, voiceUserFixture } from '../test-fixtures.fixture'
import { button } from './adminTestkit.fixture'
import { useAuthStore } from '../stores/auth'
import { setLocale } from '../i18n'

// The live operations panel has its own tests and endpoint.
vi.mock('./AdminOpsPanel.vue', () => ({ default: { template: '<div />' } }))
vi.mock('./AdminUploadLimit.vue', () => ({ default: { template: '<div />' } }))

let wrappers: VueWrapper[] = []
function keep<T extends VueWrapper>(w: T): T {
  wrappers.push(w)
  return w
}
beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  useAuthStore().user = userFixture({ role: 'admin', username: 'owner' }) // the Owner may do everything
})
afterEach(() => {
  for (const w of wrappers) w.unmount()
  wrappers = []
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.useRealTimers()
  document.body.innerHTML = ''
})
describe('member activity and connection summaries', () => {
  it('adds an ongoing voice stay to completed time', () => {
    useVoiceStore().channelUsers = {
      [fixtureId(2)]: {
        [fixtureId(1)]: voiceUserFixture({
          joined_at: new Date(Date.now() - 7260000).toISOString(),
        }),
      },
    }
    const w = keep(
      mount(ActivityStats, {
        props: {
          user: { id: fixtureId(1), voice_seconds: 3600, message_count: 3 },
        },
      }),
    )
    expect(w.text()).toContain('3 h')
  })
  it('defaults absent member counters to zero in compact and full views', async () => {
    const w = keep(mount(ActivityStats, { props: { user: {} } }))
    expect(w.text()).toContain('0')
    await w.setProps({
      variant: 'full',
      user: { message_count: 1234, voice_seconds: 3600 },
    })
    expect(w.text()).toContain('1,234')
  })
  it('leaves talk time out of the compact view until there is some', async () => {
    const w = keep(mount(ActivityStats, { props: { user: { id: fixtureId(1), voice_seconds: 0, message_count: 5 } } }))
    expect(w.find('[data-testid="activity-voice"]').exists()).toBe(false)
    expect(w.text()).toContain('5')
    await w.setProps({ user: { id: fixtureId(1), voice_seconds: 90, message_count: 5 } })
    expect(w.find('[data-testid="activity-voice"]').exists()).toBe(true)
  })
  it('shows empty connection data and emits close through both controls', async () => {
    const w = keep(mount(ConnectionStatsModal))
    expect(w.text()).toContain('–')
    expect(w.find('polyline').exists()).toBe(false)
    await button(w, 'Close').trigger('click')
    w.findComponent(BaseDialog).vm.$emit('close')
    expect(w.emitted('close')).toHaveLength(2)
    useVoiceStore().pingHistory = [4]
    await nextTick()
    expect(w.find('polyline').exists()).toBe(false)
  })
  it('draws a measurable round-trip history and preserves zero-valued metrics', async () => {
    const store = useVoiceStore()
    store.pingHistory = [0, 2, 8]
    store.ping = 0
    store.rtcStats = {
      connected: true,
      codec: '',
      sendKbps: 0,
      recvKbps: 10,
      packetsSent: 0,
      packetsReceived: 2,
      localCandidate: 'host',
      remoteCandidate: 'relay',
      srtpCipher: 'AES',
      dtlsCipher: null,
      videoIn: [],
      sample: { timestamp: 1, bytesSent: 0, bytesReceived: 0 },
      rttMs: 0,
      jitterMs: 0,
      lossPercent: 0,
      packetsLost: 0,
    }
    const w = keep(mount(ConnectionStatsModal))
    expect(w.find('polyline').attributes('points')).toBe(
      '0.0,46.0 140.0,35.5 280.0,4.0',
    )
    expect(w.text()).toContain('0 / 10 kbit/s')
    expect(w.text()).toContain('DTLS-SRTP (AES)')
    expect(w.text()).toContain('0 ms')
  })
})
