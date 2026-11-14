import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { ApiOptions } from '../../shared/lib/api'
import BotAccessPanel from './BotAccessPanel.vue'
import { useToastStore } from '../../stores/toast'
import { PERMISSION_BITS } from '../../shared/lib/permissions'
import { fixtureId } from '../../test-fixtures.fixture'
import { setLocale } from '../../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../../shared/lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../shared/lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn()
}))

const botId = fixtureId(5)
const chan = (n: number, permissions: number) => ({ channel_id: fixtureId(60 + n), name: `room${n}`, permissions })

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
})

describe('BotAccessPanel', () => {
  it('shows server-wide and per-channel permissions', async () => {
    apiMock.mockResolvedValueOnce({ permissions: PERMISSION_BITS.view_channel, elevated: false, mode: 'channels', granted: 0, mixed: false, selected_channel_ids: [], channels: [chan(1, PERMISSION_BITS.view_channel | PERMISSION_BITS.send_messages), chan(2, 0)] })
    const w = mount(BotAccessPanel, { props: { botId } })
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/bots/${botId}/access`, { decode: expect.any(Function) })
    expect(w.find('[data-testid="bot-elevated"]').exists()).toBe(false)
    const rows = w.findAll('[data-testid="bot-access-channel"]')
    expect(rows[0]?.text()).toContain('# room1')
    expect(rows[0]?.text()).toContain('Send messages')
    expect(rows[1]?.text()).toContain('No permissions')
  })

  it('warns when the bot is elevated and reports nothing granted', async () => {
    apiMock.mockResolvedValueOnce({ permissions: 0, elevated: true, mode: 'channels', granted: 0, mixed: false, selected_channel_ids: [], channels: [] })
    const w = mount(BotAccessPanel, { props: { botId } })
    await flushPromises()
    expect(w.get('[data-testid="bot-elevated"]').text()).toContain('Administrator or admin-area')
    expect(w.find('[data-testid="bot-access-none"]').exists()).toBe(true)
    expect(w.text()).toContain('No channel overrides')
  })

  it('reloads when the version changes and reports failures', async () => {
    apiMock.mockResolvedValueOnce({ permissions: 0, elevated: false, mode: 'channels', granted: 0, mixed: false, selected_channel_ids: [], channels: [] })
    const w = mount(BotAccessPanel, { props: { botId, version: 0 } })
    await flushPromises()
    apiMock.mockRejectedValueOnce(new Error('access failed'))
    await w.setProps({ version: 1 })
    await flushPromises()
    expect(apiMock).toHaveBeenCalledTimes(2)
    expect(useToastStore().toasts.map(t => t.text)).toContain('access failed')
  })
})
