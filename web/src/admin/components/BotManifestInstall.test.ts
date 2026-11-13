import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { ApiOptions } from '../../shared/lib/api'
import { ApiError } from '../../shared/lib/api'
import BotManifestInstall from './BotManifestInstall.vue'
import AdminBotsTab from './AdminBotsTab.vue'
import { useChatStore } from '../../stores/chat'
import { useToastStore } from '../../stores/toast'
import { categoryFixture, channelFixture, fixtureId } from '../../test-fixtures.fixture'
import { setLocale } from '../../i18n'
import type { BotBot, BotCheckedManifest } from '../../types/rest'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../../shared/lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../shared/lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn()
}))
const confirmMock = vi.fn<() => Promise<boolean>>()
vi.mock('../../shared/lib/confirm', () => ({ confirm: () => confirmMock() }))

const PNG = 'data:image/png;base64,iVBORw0KGgo='
const manifest = {
  manifest_version: 1, name: 'Mnemo', description: 'Answers questions and keeps notes.', homepage: 'https://example.org/mnemo',
  avatar: PNG, intents: ['GUILDS', 'GUILD_MESSAGES'], permissions: ['view_channel', 'send_messages', 'add_reactions', 'manage_messages', 'mention_everyone'],
  commands: [{ name: 'ask', description: 'Ask Mnemo something', options: [] }, { name: 'poll', description: 'A poll of its own', options: [] }]
}
const checked: BotCheckedManifest = {
  manifest_version: 1, name: 'Mnemo', description: manifest.description, homepage: manifest.homepage,
  intents: [{ name: 'GUILDS', bit: 1 }, { name: 'GUILD_MESSAGES', bit: 512 }],
  permissions: [{ key: 'view_channel', name: 'view channels', bit: 1 }, { key: 'send_messages', name: 'send messages', bit: 2 }, { key: 'add_reactions', name: 'add reactions', bit: 8 }, { key: 'manage_messages', name: 'manage messages', bit: 32 }, { key: 'mention_everyone', name: 'mention everyone', bit: 64 }],
  commands: manifest.commands
}
const newBot: BotBot = { id: fixtureId(40), user_id: fixtureId(41), name: 'Mnemo', allowed_intents: 513, disabled: false, role_ids: [], created_at: '2026-01-02T03:04:05Z', description: manifest.description, homepage: manifest.homepage }
const general = channelFixture({ id: fixtureId(50), name: 'general' })
const forum = channelFixture({ id: fixtureId(51), name: 'ideas', type: 'forum', sort_order: 1 })
const talk = channelFixture({ id: fixtureId(52), name: 'lounge', type: 'voice' })
const loose = channelFixture({ id: fixtureId(53), name: 'welcome' })

let installReply: () => Promise<unknown>

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  confirmMock.mockReset()
  sessionStorage.clear()
  installReply = async () => ({ bot: newBot, token: { id: fixtureId(42), name: 'Mnemo', hint: 'abcd', secret: 'mnb_secret', state: 'active', created_at: '2026-01-02T03:04:05Z' } })
  const chat = useChatStore()
  chat.categories = [categoryFixture({ id: fixtureId(60), name: 'Community', channels: [general, forum, talk] })]
  chat.uncategorized = [loose]
  apiMock.mockImplementation(async (path, options) => {
    if (path === '/api/admin/bots/manifests/check') return checked
    if (path === '/api/admin/bots/install') return installReply()
    if (path.endsWith('/avatar')) return { ...newBot, avatar_url: '/api/media/avatar' }
    if (path === '/api/admin/bots' && !options?.method) return []
    if (path === '/api/roles') return []
    if (path.endsWith('/access')) return { permissions: 0, elevated: false, channels: [] }
    if (/\/(tokens|commands)$/.test(path)) return []
    return undefined
  })
})

async function pasteAndCheck(text = JSON.stringify(manifest)) {
  const w = mount(BotManifestInstall)
  await w.get('[data-testid="manifest-text"]').setValue(text)
  await w.get('[data-testid="manifest-paste"]').trigger('submit')
  await flushPromises()
  return w
}

const requestTo = (path: string) => apiMock.mock.calls.find(([p]) => p === path)?.[1]

describe('adding a bot from a pasted manifest', () => {
  it('sends the manifest to the check and shows what the bot asks for, all ticked, no channel ticked', async () => {
    const w = await pasteAndCheck()
    expect(requestTo('/api/admin/bots/manifests/check')).toMatchObject({ method: 'POST', json: manifest })
    const confirm = w.get('[data-testid="manifest-confirm"]')
    expect(w.text()).toContain('Add this bot?')
    expect(confirm.get('[data-testid="manifest-description"]').text()).toBe(manifest.description)
    expect(confirm.get('[data-testid="manifest-homepage"]').attributes('href')).toBe(manifest.homepage)
    expect(confirm.get('[data-testid="manifest-avatar"]').attributes('src')).toMatch(/^blob:/)
    expect(confirm.text()).toContain('Channel changes')
    expect(confirm.text()).toContain('Send messages')
    expect(confirm.text()).toContain('/ask')
    expect(confirm.findAll('[data-intent]').every(i => (i.element as HTMLInputElement).checked)).toBe(true)
    const ticked = Object.fromEntries(confirm.findAll('[data-permission]').map(i => [i.attributes('data-permission'), (i.element as HTMLInputElement).checked]))
    expect(ticked).toEqual({ view_channel: true, send_messages: true, add_reactions: true, manage_messages: false, mention_everyone: false })
    expect(confirm.findAll('[data-channel]').map(i => (i.element as HTMLInputElement).checked)).toEqual([false, false, false])
  })

  it('offers text and forum channels by category, never a Talk', async () => {
    const w = await pasteAndCheck()
    const channels = w.get('[data-testid="manifest-channels"]')
    expect(channels.findAll('legend').map(l => l.text())).toEqual(['Without a category', 'Community'])
    expect(channels.text()).toContain('welcome')
    expect(channels.text()).toContain('ideas')
    expect(channels.text()).not.toContain('lounge')
  })

  it('installs with what stayed ticked, uploads the avatar and hands over the token', async () => {
    const w = await pasteAndCheck()
    await w.get('[data-permission="add_reactions"]').setValue(false)
    await w.get('[data-intent="GUILDS"]').setValue(false)
    await w.get(`[data-channel="${general.id}"]`).setValue(true)
    await w.get(`[data-channel="${loose.id}"]`).setValue(true)
    await w.get(`[data-channel="${loose.id}"]`).setValue(false)
    await w.get('[data-testid="manifest-confirm"]').trigger('submit')
    await flushPromises()
    expect(requestTo('/api/admin/bots/install')).toMatchObject({
      method: 'POST',
      json: { manifest, permissions: ['view_channel', 'send_messages'], intents: ['GUILD_MESSAGES'], channel_ids: [general.id] }
    })
    const upload = requestTo(`/api/admin/bots/${newBot.id}/avatar`)
    expect((upload?.form as FormData).get('avatar')).toBeInstanceOf(File)
    expect(w.emitted('installed')?.[0]).toEqual([{ ...newBot, avatar_url: '/api/media/avatar' }, 'mnb_secret'])
  })

  it('shows a refusal inline and keeps every tick', async () => {
    installReply = async () => { throw new ApiError(403, 'forbidden', 'you do not have send messages in #general, so the bot cannot get it there') }
    const w = await pasteAndCheck()
    await w.get('[data-permission="add_reactions"]').setValue(false)
    await w.get(`[data-channel="${general.id}"]`).setValue(true)
    await w.get('[data-testid="manifest-confirm"]').trigger('submit')
    await flushPromises()
    expect(w.get('[data-testid="manifest-install-error"]').text()).toContain('#general')
    expect((w.get('[data-permission="add_reactions"]').element as HTMLInputElement).checked).toBe(false)
    expect((w.get(`[data-channel="${general.id}"]`).element as HTMLInputElement).checked).toBe(true)
    expect(w.emitted('installed')).toBeUndefined()
  })

  it('still finishes when only the avatar upload fails, and says so', async () => {
    apiMock.mockImplementation(async (path) => {
      if (path === '/api/admin/bots/manifests/check') return checked
      if (path === '/api/admin/bots/install') return installReply()
      throw new ApiError(500, 'internal', 'internal server error')
    })
    const w = await pasteAndCheck()
    await w.get('[data-testid="manifest-confirm"]').trigger('submit')
    await flushPromises()
    expect(w.emitted('installed')?.[0]?.[0]).toEqual(newBot)
    expect(useToastStore().toasts.map(t => t.text)).toContain('The bot was added, but its avatar could not be uploaded. Set one in the bot’s settings.')
  })

  it('explains text that is not a manifest without asking the server', async () => {
    const w = await pasteAndCheck('{"name": ')
    expect(w.get('[data-testid="manifest-check-error"]').text()).toContain('not valid JSON')
    await w.get('[data-testid="manifest-text"]').setValue('[1, 2]')
    await w.get('[data-testid="manifest-paste"]').trigger('submit')
    expect(w.get('[data-testid="manifest-check-error"]').text()).toContain('JSON object')
    expect(requestTo('/api/admin/bots/manifests/check')).toBeUndefined()
  })

  it('names an avatar it will not use instead of showing it', async () => {
    const w = await pasteAndCheck(JSON.stringify({ ...manifest, avatar: 'https://example.org/a.png' }))
    expect(w.find('[data-testid="manifest-avatar"]').exists()).toBe(false)
    expect(w.get('[data-testid="manifest-avatar-problem"]').text()).toContain('Remote images are never loaded')
  })

  it('goes back to the pasted text without losing it', async () => {
    const w = await pasteAndCheck()
    await w.get('[data-testid="manifest-back"]').trigger('click')
    expect((w.get('[data-testid="manifest-text"]').element as HTMLTextAreaElement).value).toBe(JSON.stringify(manifest))
  })

  it('asks before cancelling with a manifest typed in', async () => {
    confirmMock.mockResolvedValue(false)
    const w = mount(BotManifestInstall)
    await w.get('[data-testid="manifest-text"]').setValue('{}')
    await w.get('[data-testid="manifest-cancel"]').trigger('click')
    await flushPromises()
    expect(confirmMock).toHaveBeenCalled()
    expect(w.emitted('cancel')).toBeUndefined()
  })
})

describe('what the confirm screen makes clear', () => {
  it('starts what pings everyone or moderates unticked, and says why', async () => {
    const w = await pasteAndCheck()
    expect(w.findAll('[data-off-at-first]').map(n => n.attributes('data-off-at-first'))).toEqual(['manage_messages', 'mention_everyone'])
    expect(w.get('[data-off-at-first="manage_messages"]').text()).toBe('Off at first: tick it only if the bot really needs it.')
    await w.get('[data-permission="mention_everyone"]').setValue(true)
    await w.get('[data-testid="manifest-confirm"]').trigger('submit')
    await flushPromises()
    expect((requestTo('/api/admin/bots/install')?.json as { permissions: string[] }).permissions).toEqual(['view_channel', 'send_messages', 'add_reactions', 'mention_everyone'])
  })

  it('shows the name, description and homepage as the bot\u2019s own quoted words, the homepage as an external link', async () => {
    const w = await pasteAndCheck()
    const identity = w.get('[data-testid="manifest-identity"]')
    expect(identity.get('figcaption').text()).toBe('From the bot’s manifest')
    expect(identity.get('[data-testid="manifest-name"]').element.tagName).toBe('Q')
    expect(identity.get('[data-testid="manifest-description"]').element.tagName).toBe('Q')
    expect(identity.find('h4').exists()).toBe(false)
    const link = identity.get('[data-testid="manifest-homepage"]')
    expect(link.attributes('target')).toBe('_blank')
    expect(link.attributes('aria-label')).toBe('Homepage of the bot, an external site: example.org')
    expect(identity.get('[data-testid="manifest-homepage-external"]').text()).toBe('(external site)')
  })

  it('marks a command named like a built-in one as the bot\u2019s', async () => {
    const w = await pasteAndCheck()
    expect(w.findAll('[data-builtin-clash]').map(n => n.attributes('data-builtin-clash'))).toEqual(['poll'])
    expect(w.get('[data-testid="manifest-commands"]').text()).toContain('Has the name of the built-in /poll. It does not replace it: members see both.')
  })

  it('says that its allows count even where the Bot role is denied', async () => {
    const w = await pasteAndCheck()
    expect(w.get('[data-testid="manifest-bot-role-note"]').text()).toContain('shared Bot role is denied')
  })
})

describe('adding a bot from an add link', () => {
  const base64url = (text: string) => btoa(String.fromCharCode(...new TextEncoder().encode(text))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')

  it('checks the linked manifest at once and lands on the confirm screen', async () => {
    const w = mount(BotManifestInstall, { props: { linked: base64url(JSON.stringify(manifest)) } })
    await flushPromises()
    expect(requestTo('/api/admin/bots/manifests/check')).toMatchObject({ json: manifest })
    expect(w.find('[data-testid="manifest-confirm"]').exists()).toBe(true)
  })

  it('says when a link is damaged', async () => {
    const w = mount(BotManifestInstall, { props: { linked: '_-_-' } })
    await flushPromises()
    expect(w.get('[data-testid="manifest-check-error"]').text()).toContain('damaged')
  })

  it('opens from the Bots tab with the link kept for it, and shows the token once after adding', async () => {
    sessionStorage.setItem('mnema_pending_bot_manifest', base64url(JSON.stringify(manifest)))
    const w = mount(AdminBotsTab, { attachTo: document.body })
    await flushPromises()
    expect(sessionStorage.getItem('mnema_pending_bot_manifest')).toBeNull()
    await w.get('[data-testid="manifest-confirm"]').trigger('submit')
    await flushPromises()
    expect(w.findAll('[data-testid="bot-row"]').map(r => r.text())).toEqual([expect.stringContaining('Mnemo')])
    expect(w.find('[data-testid="bot-detail"]').exists()).toBe(true)
    expect(document.querySelector('[data-testid="bot-secret-value"]')?.textContent).toBe('mnb_secret')
    expect(w.get('[data-testid="bot-about"]').text()).toContain(manifest.description)
    w.unmount()
  })
})

describe('the Bots tab entry', () => {
  it('opens the paste step next to New bot', async () => {
    const w = mount(AdminBotsTab)
    await flushPromises()
    expect(w.find('[data-testid="manifest-paste"]').exists()).toBe(false)
    await w.get('[data-testid="bot-add-manifest"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="manifest-paste"]').exists()).toBe(true)
    await w.get('[data-testid="manifest-cancel"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="manifest-paste"]').exists()).toBe(false)
  })
})
