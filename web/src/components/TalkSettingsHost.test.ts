import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import type { ApiOptions } from '../lib/api'
import TalkSettingsHost from './TalkSettingsHost.vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { closeTalkSettings, inviteConversationToTalk, openTalkSettings, setLobby, startConversationTalk, talkSettingsFor, updateTalk } from '../lib/talks'
import { channelFixture, fixtureId, userFixture } from '../test-fixtures.fixture'
import { setLocale } from '../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn()
}))

const talk = channelFixture({ id: fixtureId(30), type: 'voice', name: 'Talk 1', is_temp: true, leader_id: fixtureId(1) })

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  apiMock.mockResolvedValue({ overwrites: [] })
  closeTalkSettings()
  useAuthStore().user = userFixture({ id: fixtureId(1) })
})

describe('the Talk settings host', () => {
  it('shows the dialog for the asked Talk and closes when the Talk is gone', async () => {
    const chat = useChatStore()
    chat.uncategorized = [talk]
    const w = mount(TalkSettingsHost)
    expect(w.find('[data-testid="talk-settings"]').exists()).toBe(false)
    openTalkSettings(talk.id)
    await flushPromises()
    expect(w.find('[data-testid="talk-settings"]').exists()).toBe(true)
    chat.uncategorized = []
    await flushPromises()
    expect(w.find('[data-testid="talk-settings"]').exists()).toBe(false)
    expect(talkSettingsFor.value).toBeNull()
  })
})

describe('the calls behind the Talk interface', () => {
  const reply = { id: talk.id }
  it('use the admin route for managers and the leader route otherwise', async () => {
    apiMock.mockResolvedValue(talk)
    await updateTalk(talk.id, { name: 'x' }, true)
    await updateTalk(talk.id, { user_limit: 3 }, false)
    await setLobby(talk.id, true)
    expect(apiMock.mock.calls.map(c => [c[0], c[1]?.method])).toEqual([
      [`/api/admin/channels/${talk.id}`, 'PATCH'],
      [`/api/talks/${talk.id}`, 'PATCH'],
      [`/api/admin/channels/${talk.id}`, 'PATCH']
    ])
  })

  it('start a Talk from a conversation and invite one to a Talk', async () => {
    apiMock.mockResolvedValue({ channel_id: talk.id, message: reply })
    await startConversationTalk(fixtureId(2))
    expect(apiMock).toHaveBeenCalledWith(`/api/dms/${fixtureId(2)}/talk`, expect.objectContaining({ method: 'POST' }))
    await inviteConversationToTalk(talk.id, fixtureId(2))
    expect(apiMock).toHaveBeenCalledWith(`/api/talks/${talk.id}/invites`, expect.objectContaining({ json: { conversation_id: fixtureId(2) } }))
  })
})
