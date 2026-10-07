import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import type { ApiOptions } from '../lib/api'
import { ApiError } from '../lib/api'
import type { AdminUser, ChannelHierarchy } from '../types/domain'
import AdminDashboard from './AdminDashboard.vue'
import AdminUsersTab from './AdminUsersTab.vue'
import AdminLayoutTab from './AdminLayoutTab.vue'
import AdminSystemTab from './AdminSystemTab.vue'
import SelfUpdateDialog from './SelfUpdateDialog.vue'
import ActivityStats from './ActivityStats.vue'
import ConnectionStatsModal from './ConnectionStatsModal.vue'
import BaseDialog from './BaseDialog.vue'
import { systemStatus } from './systemStatus.fixture'
import {
  categoryFixture,
  channelFixture,
  fixtureId,
  userFixture,
  voiceUserFixture,
} from '../test-fixtures.fixture'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'
import { setLocale } from '../i18n'
const apiMock =
  vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
const confirmMock = vi.fn<() => Promise<boolean>>()
vi.mock('../lib/api', async (original) => ({
  ...(await original<typeof import('../lib/api')>()),
  api: (path: string, options?: ApiOptions) => apiMock(path, options),
}))
vi.mock('../lib/confirm', () => ({ confirm: () => confirmMock() }))
let wrappers: VueWrapper[] = []
function keep<T extends VueWrapper>(w: T): T {
  wrappers.push(w)
  return w
}
function tree(): ChannelHierarchy {
  return {
    uncategorized: [
      channelFixture({ id: fixtureId(10) }),
      channelFixture({ id: fixtureId(11), type: 'voice' }),
    ],
    categories: [
      categoryFixture({
        id: fixtureId(20),
        channels: [
          channelFixture({
            id: fixtureId(21),
            category_id: fixtureId(20),
            topic: 'topic',
          }),
          channelFixture({
            id: fixtureId(22),
            category_id: fixtureId(20),
            type: 'voice',
          }),
        ],
      }),
      categoryFixture({ id: fixtureId(30), sort_order: 1 }),
    ],
  }
}
function member(disabled = false): AdminUser {
  return {
    ...userFixture({
      id: fixtureId(disabled ? 42 : 41),
      username: disabled ? 'disabled' : 'member',
      display_name: '',
    }),
    disabled,
    last_seen_at: disabled ? null : '2026-01-01T00:00:00Z',
  }
}
function errors() {
  return useToastStore()
    .toasts.filter((t) => t.type === 'error')
    .map((t) => t.text)
}
function button(w: VueWrapper, text: string) {
  const b = w.findAll('button').find((b) => b.text() === text)
  if (!b) throw new Error(`Missing button ${text}`)
  return b
}
beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  confirmMock.mockReset().mockResolvedValue(true)
})
afterEach(() => {
  for (const w of wrappers) w.unmount()
  wrappers = []
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.useRealTimers()
  document.body.innerHTML = ''
})
describe('admin account recovery and cancellation', () => {
  async function openUsers() {
    apiMock.mockResolvedValue([member(), member(true)])
    const w = keep(mount(AdminUsersTab))
    await flushPromises()
    return w
  }
  it.each(['action-disable', 'action-revoke-sessions', 'action-kick'])(
    'cancels %s without a mutation',
    async (action) => {
      useVoiceStore().channelUsers = {
        [fixtureId(2)]: {
          [fixtureId(41)]: voiceUserFixture({ id: fixtureId(41) }),
        },
      }
      const w = await openUsers()
      confirmMock.mockResolvedValue(false)
      apiMock.mockClear()
      await w.find(`[data-testid="${action}"]`).trigger('click')
      await flushPromises()
      expect(apiMock).not.toHaveBeenCalled()
    },
  )
  it.each(['action-disable', 'action-revoke-sessions', 'action-kick'])(
    'reports failed %s and preserves the table',
    async (action) => {
      useVoiceStore().channelUsers = {
        [fixtureId(2)]: {
          [fixtureId(41)]: voiceUserFixture({ id: fixtureId(41) }),
        },
      }
      useChatStore().uncategorized = [channelFixture({ name: 'Lounge' })]
      const w = await openUsers()
      expect(w.text()).toContain('Lounge')
      apiMock.mockRejectedValueOnce(new Error('mutation denied'))
      await w.find(`[data-testid="${action}"]`).trigger('click')
      await flushPromises()
      expect(errors()).toContain('mutation denied')
      expect(w.findAll('[data-testid="user-row"]')).toHaveLength(2)
    },
  )
  it('reports enable and password failures, preserves input for retry and permits cancelling', async () => {
    const w = await openUsers()
    apiMock.mockRejectedValueOnce(new Error('enable failed'))
    await w.findAll('[data-testid="action-disable"]')[1]!.trigger('click')
    await flushPromises()
    expect(errors()).toContain('enable failed')
    await w.find('[data-testid="action-reset-password"]').trigger('click')
    await w.find('[data-testid="new-password-input"]').setValue('long-password')
    apiMock.mockRejectedValueOnce(new Error('reset failed'))
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(errors()).toContain('reset failed')
    expect(w.find('[data-testid="new-password-input"]').element).toHaveProperty(
      'value',
      'long-password',
    )
    await button(w, 'Cancel').trigger('click')
    expect(w.find('form').exists()).toBe(false)
    await w.find('[data-testid="action-reset-password"]').trigger('click')
    w.findComponent(BaseDialog).vm.$emit('close')
    await nextTick()
    expect(w.find('form').exists()).toBe(false)
  })
  it('filters by username, handles no matches, and keeps members after a failed refresh', async () => {
    const w = await openUsers()
    await w.find('input[type="search"]').setValue(' MEMBER ')
    expect(w.findAll('[data-testid="user-row"]')).toHaveLength(1)
    await w.find('input[type="search"]').setValue('absent')
    expect(w.findAll('[data-testid="user-row"]')).toHaveLength(0)
    await w.find('input[type="search"]').setValue('')
    apiMock.mockRejectedValueOnce(new Error('reload failed'))
    await w.find('button[aria-label="Refresh"]').trigger('click')
    await flushPromises()
    expect(w.findAll('[data-testid="user-row"]')).toHaveLength(2)
    expect(errors()).toContain('reload failed')
  })
  it('falls back to original last-seen text if locale formatting throws', async () => {
    vi.spyOn(Date.prototype, 'toLocaleString').mockImplementation(() => {
      throw new RangeError('locale')
    })
    const w = await openUsers()
    expect(w.text()).toContain('2026-01-01T00:00:00Z')
  })
})
describe('dashboard route synchronization', () => {
  it('accepts valid prop changes, ignores invalid props, and emits close', async () => {
    apiMock.mockResolvedValue([])
    useAuthStore().user = userFixture({ username: 'admin', display_name: '' })
    const w = keep(
      mount(AdminDashboard, {
        props: { initialTab: 'invalid' },
        global: {
          stubs: {
            AdminUsersTab: true,
            AdminLayoutTab: true,
            AdminMediaTab: true,
            AdminInvitesTab: true,
            AdminSystemTab: true,
          },
        },
      }),
    )
    expect(w.find('[data-testid="tab-users"]').attributes('aria-pressed')).toBe(
      'true',
    )
    await w.setProps({ initialTab: 'system' })
    expect(
      w.find('[data-testid="tab-system"]').attributes('aria-pressed'),
    ).toBe('true')
    await w.setProps({ initialTab: '' })
    await w.setProps({ initialTab: 'invalid' })
    expect(
      w.find('[data-testid="tab-system"]').attributes('aria-pressed'),
    ).toBe('true')
    window.history.replaceState(null, '', '/admin/media')
    await w.find('[data-testid="tab-media"]').trigger('click')
    w.findComponent(BaseDialog).vm.$emit('close')
    expect(w.emitted('close')).toHaveLength(1)
  })
  it('renders an empty signed-in name safely', () => {
    apiMock.mockResolvedValue([])
    const w = keep(
      mount(AdminDashboard, { global: { stubs: { AdminUsersTab: true } } }),
    )
    expect(w.text()).toContain('Signed in as')
  })
})
describe('self update confirmation', () => {
  it('guards empty and concurrent submits, clears credentials, and emits result', async () => {
    let finish: ((value: unknown) => void) | undefined
    apiMock.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve
        }),
    )
    const w = keep(
      mount(SelfUpdateDialog, {
        props: { currentVersion: '0.4.0', targetVersion: '0.5.0' },
      }),
    )
    await w.find('form').trigger('submit')
    expect(apiMock).not.toHaveBeenCalled()
    await w.find('input').setValue('synthetic password')
    await w.find('form').trigger('submit')
    await w.find('form').trigger('submit')
    expect(apiMock).toHaveBeenCalledTimes(1)
    expect(
      w.find('[data-testid="self-update-confirm"]').attributes('disabled'),
    ).toBeDefined()
    if (!finish) throw new Error('Missing request resolver')
    finish({
      status: 'started',
      from_version: '0.4.0',
      target_version: '0.5.0',
    })
    await flushPromises()
    expect(w.emitted('started')).toEqual([
      [{ status: 'started', from_version: '0.4.0', target_version: '0.5.0' }],
    ])
    expect(w.find('input').element).toHaveProperty('value', '')
    await button(w, 'Cancel').trigger('click')
    w.findComponent(BaseDialog).vm.$emit('close')
    expect(w.emitted('close')).toHaveLength(2)
  })
  it.each([
    new ApiError(429, 'RATE_LIMITED', 'limited'),
    new Error('denied'),
    null,
  ])('reports a rejected update and clears credentials', async (reason) => {
    apiMock.mockRejectedValue(reason)
    const w = keep(
      mount(SelfUpdateDialog, {
        props: {
          currentVersion: '0.4.0',
          targetVersion: '0.5.0',
          releaseUrl: 'https://example.com/releases',
        },
      }),
    )
    await w.find('input').setValue('synthetic password')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(w.find('[role="alert"]').text()).not.toBe('')
    expect(w.find('input').element).toHaveProperty('value', '')
    expect(w.find('a').attributes('rel')).toContain('noopener')
  })
})
describe('admin system operational failures', () => {
  async function openSystem(st = systemStatus()) {
    apiMock.mockResolvedValueOnce(st)
    const w = keep(mount(AdminSystemTab))
    await flushPromises()
    return w
  }
  it.each([
    new ApiError(429, 'RATE_LIMITED', 'limited'),
    new Error('check failed'),
    null,
  ])(
    'reports failed immediate checks and re-enables the action',
    async (reason) => {
      const w = await openSystem()
      apiMock.mockRejectedValueOnce(reason)
      await w.find('[data-testid="update-check-now"]').trigger('click')
      await flushPromises()
      expect(errors()).toHaveLength(1)
      expect(
        w.find('[data-testid="update-check-now"]').attributes('disabled'),
      ).toBeUndefined()
    },
  )
  it('reports release service errors then up-to-date status', async () => {
    const w = await openSystem()
    apiMock.mockResolvedValueOnce({
      ...systemStatus().update,
      check_error: 'upstream offline',
    })
    await w.find('[data-testid="update-check-now"]').trigger('click')
    await flushPromises()
    expect(errors()[0]).toContain('upstream offline')
    apiMock.mockResolvedValueOnce(systemStatus().update)
    await w.find('[data-testid="update-check-now"]').trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.some((t) => t.type === 'success')).toBe(true)
  })
  it('copies a manual update command, reports clipboard denial, and closes confirmation', async () => {
    const st = systemStatus()
    st.update.update_available = true
    st.self_update = { ...st.self_update, configured: true, available: true }
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    const w = await openSystem(st)
    await button(w, 'Copy').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith(
      'docker compose pull app && docker compose up -d app',
    )
    writeText.mockRejectedValueOnce(new Error('denied'))
    await button(w, 'Copy').trigger('click')
    await flushPromises()
    expect(errors()).toHaveLength(1)
    await w.find('[data-testid="self-update-open"]').trigger('click')
    w.findComponent(SelfUpdateDialog).vm.$emit('close')
    await nextTick()
    expect(w.findComponent(SelfUpdateDialog).exists()).toBe(false)
  })
  it.each([0, 3700])(
    'formats short uptime, missing metadata, and absent configuration (%s seconds)',
    async (seconds) => {
      const st = systemStatus()
      st.version.revision = ''
      st.update.checked_at = 'invalid date'
      st.update.latest_version = ''
      st.health.database.latest_migration = ''
      st.health.database.pending_migrations = 2
      st.health.storage.configured = false
      st.health.storage.total_bytes = 0
      st.health.runtime.uptime_seconds = seconds
      st.health.voice.enabled = false
      st.health.voice.turn_configured = false
      const w = await openSystem(st)
      expect(w.find('[data-testid="system-health"]').text()).toContain('0 B')
      expect(w.find('[data-testid="system-uptime"]').text()).toContain(
        seconds ? '1 h' : '0 min',
      )
      expect(w.text()).toContain('Not configured')
    },
  )
  it('shows a placeholder for a missing check time and guards a check while status is absent', async () => {
    const w = await openSystem()
    apiMock.mockResolvedValueOnce({
      ...systemStatus().update,
      checked_at: null,
    })
    await w.find('[data-testid="update-check-now"]').trigger('click')
    await flushPromises()
    expect(w.text()).toContain('–')
  })
  it('polls old version, malformed health, and network failure until stalled, then stops on unmount', async () => {
    vi.useFakeTimers()
    const st = systemStatus()
    st.update.update_available = true
    st.self_update = { ...st.self_update, configured: true, available: true }
    const w = await openSystem(st)
    await w.find('[data-testid="self-update-open"]').trigger('click')
    const fetchMock = vi.fn().mockRejectedValue(new Error('restart'))
    vi.stubGlobal('fetch', fetchMock)
    w.findComponent(SelfUpdateDialog).vm.$emit('started', {
      status: 'started',
      from_version: '',
      target_version: '0.5.0',
    })
    await nextTick()
    fetchMock
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ status: 'ok', version: '0.4.0' }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ status: 'ok', version: '' }),
      })
    await vi.advanceTimersByTimeAsync(310000)
    expect(w.find('[data-testid="self-update-stalled"]').exists()).toBe(true)
    const count = fetchMock.mock.calls.length
    await vi.advanceTimersByTimeAsync(15000)
    expect(fetchMock).toHaveBeenCalledTimes(count)
  })
  it('clears an active health poll when the component unmounts', async () => {
    vi.useFakeTimers()
    const st = systemStatus()
    st.update.update_available = true
    st.self_update = { ...st.self_update, configured: true, available: true }
    const w = await openSystem(st)
    await w.find('[data-testid="self-update-open"]').trigger('click')
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    w.findComponent(SelfUpdateDialog).vm.$emit('started', {
      status: 'started',
      from_version: '0.4.0',
      target_version: '0.5.0',
    })
    await nextTick()
    w.unmount()
    wrappers = wrappers.filter((x) => x !== w)
    await vi.advanceTimersByTimeAsync(10000)
    expect(fetchMock).not.toHaveBeenCalled()
  })
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
describe('layout editing recovery and unsaved order', () => {
  async function openLayout() {
    vi.spyOn(useChatStore(), 'fetchChannels').mockResolvedValue(undefined)
    apiMock.mockImplementation(async (path, options) => {
      if (path === '/api/channels') return tree()
      if (path === '/api/admin/categories' && options?.method === 'POST')
        return categoryFixture({ id: fixtureId(99), name: 'New category' })
      if (options?.method === 'PATCH')
        return channelFixture({ name: 'server name', topic: 'server topic' })
      return { ok: true }
    })
    const w = keep(mount(AdminLayoutTab))
    await flushPromises()
    return w
  }
  const cat = (w: VueWrapper, id = 20) =>
    w.find(`[data-testid="category-item-${fixtureId(id)}"]`)
  const ch = (w: VueWrapper, id = 21) =>
    w.find(`[data-testid="channel-item-${fixtureId(id)}"]`)
  async function dirty(w: VueWrapper) {
    await cat(w).find('[data-testid="move-down-channel"]').trigger('click')
  }
  it('reorders categories and uncategorized channels in both directions and exercises native drag events', async () => {
    const w = await openLayout()
    await cat(w).find('[data-testid="move-down-category"]').trigger('click')
    await cat(w).find('[data-testid="move-up-category"]').trigger('click')
    await ch(w, 10).find('[data-testid="move-down-channel"]').trigger('click')
    await ch(w, 10).find('[data-testid="move-up-channel"]').trigger('click')
    await ch(w).find('[data-testid="move-down-channel"]').trigger('click')
    await ch(w).find('[data-testid="move-up-channel"]').trigger('click')
    const transfer = { effectAllowed: '' }
    await cat(w).trigger('dragstart', { dataTransfer: transfer })
    expect(transfer.effectAllowed).toBe('move')
    await cat(w, 30).trigger('drop')
    await cat(w, 30).trigger('dragend')
    await cat(w).trigger('dragover')
    await ch(w, 10).trigger('dragstart', { dataTransfer: transfer })
    await ch(w, 11).trigger('dragover')
    await ch(w, 11).trigger('drop')
    await ch(w, 11).trigger('dragend')
    await ch(w).trigger('dragstart', { dataTransfer: transfer })
    await ch(w, 22).trigger('dragover')
    await ch(w).trigger('dragend')
    await cat(w, 30).find('div.border-l-2').trigger('dragover')
    await cat(w, 30).find('div.border-l-2').trigger('drop')
    await ch(w, 11).trigger('dragover')
    await ch(w, 11).trigger('dragstart')
    await ch(w, 11).trigger('dragend')
    await w
      .findAll('button')
      .find((b) => b.text() === 'Save layout')!
      .trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(
      '/api/admin/layout',
      expect.objectContaining({ method: 'PUT' }),
    )
  })
  it('renames categories with unsaved order, validates empty submissions, and permits both cancellation controls', async () => {
    const w = await openLayout()
    await dirty(w)
    await cat(w).find('[data-testid="rename-category"]').trigger('click')
    await w.find('form input').setValue('  ')
    await w.find('form').trigger('submit')
    expect(
      apiMock.mock.calls.some(([, opts]) => opts?.method === 'PATCH'),
    ).toBe(false)
    await w.find('form input').setValue(' Renamed ')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(cat(w).text()).toContain('Renamed')
    expect(w.text()).toContain('Unsaved changes')
    await cat(w).find('[data-testid="rename-category"]').trigger('click')
    await button(w, 'Cancel').trigger('click')
    expect(w.find('form').exists()).toBe(false)
    await cat(w).find('[data-testid="rename-category"]').trigger('click')
    w.findComponent(BaseDialog).vm.$emit('close')
    await nextTick()
    expect(w.find('form').exists()).toBe(false)
  })
  it('creates categories through enter and click, preserves unsaved state, and reports failure without clearing input', async () => {
    const w = await openLayout()
    const input = w.find('input')
    await input.setValue(' ')
    await input.trigger('keydown.enter')
    expect(apiMock.mock.calls.some(([, opts]) => opts?.method === 'POST')).toBe(
      false,
    )
    await dirty(w)
    await input.setValue('New category')
    await input.trigger('keydown.enter')
    await flushPromises()
    expect(cat(w, 99).text()).toContain('New category')
    expect(input.element).toHaveProperty('value', '')
    await input.setValue('Retry')
    apiMock.mockRejectedValueOnce(new Error('create denied'))
    w.find('input').element.parentElement!.querySelector('button')!.click()
    await flushPromises()
    expect(errors()).toContain('create denied')
    expect(input.element).toHaveProperty('value', 'Retry')
  })
  it.each(['category', 'channel'] as const)(
    'keeps a failed %s edit open and supports cancellation',
    async (kind) => {
      const w = await openLayout()
      const item = kind === 'category' ? cat(w) : ch(w, 10)
      await item.find(`[data-testid="rename-${kind}"]`).trigger('click')
      if (kind === 'channel') {
        await w.find('form input').setValue(' ')
        await w.find('form').trigger('submit')
        expect(
          apiMock.mock.calls.some(([, opts]) => opts?.method === 'PATCH'),
        ).toBe(false)
      }
      await w.find('form input').setValue('renamed')
      apiMock.mockRejectedValueOnce(new Error('edit denied'))
      await w.find('form').trigger('submit')
      await flushPromises()
      expect(errors()).toContain('edit denied')
      expect(w.find('form').exists()).toBe(true)
      await button(w, 'Cancel').trigger('click')
      expect(w.find('form').exists()).toBe(false)
      await item.find(`[data-testid="rename-${kind}"]`).trigger('click')
      w.findComponent(BaseDialog).vm.$emit('close')
      await nextTick()
      expect(w.find('form').exists()).toBe(false)
    },
  )
  it.each(['category', 'channel'] as const)(
    'cancels %s deletion and reports mutation failure',
    async (kind) => {
      const w = await openLayout()
      const item = kind === 'category' ? cat(w) : ch(w, 10)
      confirmMock.mockResolvedValueOnce(false)
      apiMock.mockClear()
      await item.find(`[data-testid="delete-${kind}"]`).trigger('click')
      await flushPromises()
      expect(apiMock).not.toHaveBeenCalled()
      apiMock.mockRejectedValueOnce(new Error('delete denied'))
      await item.find(`[data-testid="delete-${kind}"]`).trigger('click')
      await flushPromises()
      expect(errors()).toContain('delete denied')
      expect(item.exists()).toBe(true)
    },
  )
  it('deletes an uncategorized channel while preserving unsaved order', async () => {
    const w = await openLayout()
    await dirty(w)
    await ch(w, 10).find('[data-testid="delete-channel"]').trigger('click')
    await flushPromises()
    expect(ch(w, 10).exists()).toBe(false)
    expect(w.text()).toContain('Unsaved changes')
  })
  it('reports a save failure and preserves the unsaved layout', async () => {
    const w = await openLayout()
    await dirty(w)
    apiMock.mockRejectedValueOnce(new Error('save denied'))
    await w.find('[data-testid="save-layout-button"]').trigger('click')
    await flushPromises()
    expect(errors()).toContain('save denied')
    expect(w.text()).toContain('Unsaved changes')
    expect(
      w.find('[data-testid="save-layout-button"]').attributes('disabled'),
    ).toBeUndefined()
  })
  it('retains a local edit when its follow-up channel reload fails', async () => {
    const w = await openLayout()
    await ch(w).find('[data-testid="rename-channel"]').trigger('click')
    await w.find('form input').setValue('local name')
    apiMock
      .mockResolvedValueOnce(channelFixture({ name: 'local name' }))
      .mockRejectedValueOnce(new Error('reload denied'))
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(ch(w).text()).toContain('local name')
    expect(errors()).toContain('reload denied')
  })
  it('preserves a reorder begun while a post-edit reload is in flight', async () => {
    const w = await openLayout()
    await cat(w).find('[data-testid="rename-category"]').trigger('click')
    await w.find('form input').setValue('new name')
    let resolveReload: ((value: unknown) => void) | undefined
    apiMock.mockResolvedValueOnce({ ok: true }).mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveReload = resolve
        }),
    )
    await w.find('form').trigger('submit')
    await flushPromises()
    await dirty(w)
    if (!resolveReload) throw new Error('Missing reload resolver')
    resolveReload(tree())
    await flushPromises()
    expect(cat(w).text()).toContain('new name')
    expect(w.text()).toContain('Unsaved changes')
  })
  it('handles initial loading failures', async () => {
    apiMock.mockRejectedValue(new Error('layout offline'))
    const w = keep(mount(AdminLayoutTab))
    await flushPromises()
    expect(errors()).toContain('layout offline')
    expect(w.findAll('[data-testid^="category-item-"]')).toHaveLength(0)
  })
})
describe('layout defensive stale action handling', () => {
  it('opens empty names for editing and invokes the channel topic update handler', async () => {
    const data = tree()
    data.categories[0]!.name = ''
    data.categories[0]!.channels[0]!.name = ''
    data.categories[0]!.channels[0]!.topic = ''
    apiMock.mockResolvedValue(data)
    const w = keep(mount(AdminLayoutTab))
    await flushPromises()
    await w.find('[data-testid="rename-category"]').trigger('click')
    expect(w.find('form input').element).toHaveProperty('value', '')
    await button(w, 'Cancel').trigger('click')
    await w
      .find(
        `[data-testid="channel-item-${fixtureId(21)}"] [data-testid="rename-channel"]`,
      )
      .trigger('click')
    expect(w.find('form input').element).toHaveProperty('value', '')
    await w.findAll('form input')[1]!.setValue('new topic')
    expect(w.findAll('form input')[1]!.element).toHaveProperty(
      'value',
      'new topic',
    )
    await button(w, 'Cancel').trigger('click')
  })
  it.each(['category', 'channel'] as const)(
    'safely ignores a successful %s rename after concurrent removal',
    async (kind) => {
      vi.spyOn(useChatStore(), 'fetchChannels').mockResolvedValue(undefined)
      apiMock.mockResolvedValue(tree())
      const w = keep(mount(AdminLayoutTab))
      await flushPromises()
      await w
        .find(
          `[data-testid="category-item-${fixtureId(20)}"] [data-testid="move-down-channel"]`,
        )
        .trigger('click')
      const item = w.find(
        `[data-testid="${kind}-item-${fixtureId(kind === 'category' ? 20 : 21)}"]`,
      )
      await item.find(`[data-testid="rename-${kind}"]`).trigger('click')
      await w.find('form input').setValue('Renamed')
      let complete: ((value: unknown) => void) | undefined
      apiMock.mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            complete = resolve
          }),
      )
      await w.find('form').trigger('submit')
      await item.find(`[data-testid="delete-${kind}"]`).trigger('click')
      await flushPromises()
      if (!complete) throw new Error('Missing rename resolver')
      complete(channelFixture({ name: 'Renamed' }))
      await flushPromises()
      expect(
        w
          .find(
            `[data-testid="${kind}-item-${fixtureId(kind === 'category' ? 20 : 21)}"]`,
          )
          .exists(),
      ).toBe(false)
      expect(errors()).toEqual([])
    },
  )
})

describe('layout stale arrow handler', () => {
  it('ignores an arrow from a channel removed by another action', async () => {
    vi.spyOn(useChatStore(), 'fetchChannels').mockResolvedValue(undefined)
    apiMock.mockResolvedValue(tree())
    const w = keep(mount(AdminLayoutTab))
    await flushPromises()
    const row = w.find(`[data-testid="channel-item-${fixtureId(22)}"]`)
    const arrow = row.find('[data-testid="move-up-channel"]')
    await arrow.trigger('click')
    const downArrow = row.find('[data-testid="move-down-channel"]')
    await row.find('[data-testid="delete-channel"]').trigger('click')
    await flushPromises()
    await downArrow.trigger('click')
    expect(w.find(`[data-testid="channel-item-${fixtureId(22)}"]`).exists()).toBe(false)
    expect(errors()).toEqual([])
  })
})
