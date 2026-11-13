import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { ApiOptions } from '../../shared/lib/api'
import { ApiError } from '../../shared/lib/api'
import BotChannelAccessPanel from './BotChannelAccessPanel.vue'
import { useChatStore } from '../../stores/chat'
import { useToastStore } from '../../stores/toast'
import { PERMISSION_BITS } from '../../shared/lib/permissions'
import { channelFixture, fixtureId } from '../../test-fixtures.fixture'
import { setLocale } from '../../i18n'
import type { BotAccess } from '../../types/rest'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../../shared/lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../shared/lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn()
}))

const botId = fixtureId(7)
const path = `/api/admin/bots/${botId}/access`
const general = channelFixture({ id: fixtureId(50), name: 'general' })
const staff = channelFixture({ id: fixtureId(51), name: 'staff' })
const view = PERMISSION_BITS.view_channel
const send = PERMISSION_BITS.send_messages

function access(overrides: Partial<BotAccess> = {}): BotAccess {
  return { permissions: 0, elevated: false, channels: [], mode: 'channels', granted: view | send, mixed: false, selected_channel_ids: [general.id], ...overrides }
}

let current: BotAccess

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  current = access()
  const chat = useChatStore()
  chat.categories = []
  chat.uncategorized = [general, staff]
  apiMock.mockImplementation(async (p, options) => {
    if (p !== path) return undefined
    if (options?.method === 'PUT') {
      const body = options.json as { access: 'all' | 'channels'; permissions: string[]; channel_ids: string[] }
      current = access({ mode: body.access, selected_channel_ids: body.channel_ids })
    }
    return current
  })
})

async function mountPanel() {
  const w = mount(BotChannelAccessPanel, { props: { botId } })
  await flushPromises()
  return w
}

const putCall = () => apiMock.mock.calls.find(([, o]) => o?.method === 'PUT')?.[1]

describe('the access choice in a bot’s settings', () => {
  it('shows the bot’s current choice, permissions and channels', async () => {
    const w = await mountPanel()
    expect((w.get('[data-access="channels"]').element as HTMLInputElement).checked).toBe(true)
    const ticked = w.findAll('[data-permission]').filter(i => (i.element as HTMLInputElement).checked).map(i => i.attributes('data-permission'))
    expect(ticked).toEqual(['view_channel', 'send_messages'])
    expect((w.get(`[data-channel="${general.id}"]`).element as HTMLInputElement).checked).toBe(true)
    expect((w.get(`[data-channel="${staff.id}"]`).element as HTMLInputElement).checked).toBe(false)
  })

  it('offers only a manifest’s channel permissions, never Administrator or the admin area', async () => {
    const w = await mountPanel()
    expect(w.findAll('[data-permission]').map(i => i.attributes('data-permission'))).toEqual(
      ['view_channel', 'send_messages', 'attach_files', 'add_reactions', 'manage_messages', 'mention_everyone'])
  })

  it('switches to all channels and saves the permissions with the private channels ticked', async () => {
    const w = await mountPanel()
    await w.get('[data-access="all"]').setValue(true)
    expect(w.get('[data-testid="bot-access-private-note"]').text()).toContain('Private channels stay private')
    await w.get('[data-permission="add_reactions"]').setValue(true)
    await w.get(`[data-channel="${staff.id}"]`).setValue(true)
    await w.get('[data-testid="bot-channel-access-save"]').trigger('click')
    await flushPromises()
    expect(putCall()).toMatchObject({ method: 'PUT', json: { access: 'all', permissions: ['view_channel', 'send_messages', 'add_reactions'], channel_ids: [general.id, staff.id] } })
    expect(w.emitted('saved')).toHaveLength(1)
    expect(useToastStore().toasts.map(t => t.text)).toContain('Access saved.')
  })

  it('shows what the bot has now: all channels, with its permissions', async () => {
    current = access({ mode: 'all', selected_channel_ids: [] })
    const w = await mountPanel()
    expect((w.get('[data-access="all"]').element as HTMLInputElement).checked).toBe(true)
    expect(w.find('[data-testid="bot-access-private-note"]').exists()).toBe(true)
  })

  it('says so when the selected channels allow different sets, and not otherwise', async () => {
    const quiet = await mountPanel()
    expect(quiet.find('[data-testid="bot-channel-access-mixed"]').exists()).toBe(false)
    current = access({ mixed: true })
    const w = await mountPanel()
    expect(w.get('[data-testid="bot-channel-access-mixed"]').text()).toContain('different permissions')
  })

  it('shows a refusal inline, keeps the choice and does not say it was saved', async () => {
    const w = await mountPanel()
    apiMock.mockImplementation(async (_p, options) => {
      if (options?.method === 'PUT') throw new ApiError(403, 'forbidden', 'the change would give a bot a permission you do not have')
      return current
    })
    await w.get('[data-access="all"]').setValue(true)
    await w.get('[data-testid="bot-channel-access-save"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="bot-channel-access-error"]').text()).toContain('a permission you do not have')
    expect((w.get('[data-access="all"]').element as HTMLInputElement).checked).toBe(true)
    expect(w.emitted('saved')).toBeUndefined()
  })
})
