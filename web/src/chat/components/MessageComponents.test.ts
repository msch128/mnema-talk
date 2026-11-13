import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { ApiOptions } from '../../shared/lib/api'
import { ApiError } from '../../shared/lib/api'
import MessageComponents from './MessageComponents.vue'
import BotMessageComponents from './BotMessageComponents.vue'
import EphemeralNotes from './EphemeralNotes.vue'
import { useChatStore } from '../../stores/chat'
import { setLocale } from '../../i18n'
import { fixtureId, messageFixture } from '../../test-fixtures.fixture'
import type { ChatActionRow, ChatComponent } from '../../types/rest'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../../shared/lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../shared/lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))
const confirmMock = vi.fn<(options: { excerpt?: string }) => Promise<boolean>>()
vi.mock('../../shared/lib/confirm', () => ({ confirm: (options: { excerpt?: string }) => confirmMock(options) }))

const row = (...components: ChatComponent[]): ChatActionRow => ({ type: 1, components })
const deploy: ChatComponent = { type: 2, style: 3, label: 'Deploy', custom_id: 'deploy:v2.4' }
const cancel: ChatComponent = { type: 2, style: 2, label: 'Cancel', custom_id: 'cancel' }
const remove: ChatComponent = { type: 2, style: 4, label: 'Remove', custom_id: 'rm', disabled: true }
const go: ChatComponent = { type: 2, style: 1, label: 'Go', emoji: { name: '🚀' }, custom_id: 'go' }
const changelog: ChatComponent = { type: 2, style: 5, label: 'Changelog', url: 'https://example.org/changes' }
const ENDPOINT = '/api/channels/c/messages/m/components'

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset().mockResolvedValue(undefined)
  confirmMock.mockReset().mockResolvedValue(false)
})
afterEach(() => vi.useRealTimers())

const mountRows = (components: ChatActionRow[]) => mount(MessageComponents, { props: { components, endpoint: ENDPOINT, botName: 'Deployer' } })
const button = (w: ReturnType<typeof mountRows>, label: string) => w.findAll('[data-testid="component-button"]').find(b => b.text().includes(label))!

describe('buttons of a bot message', () => {
  it('shows each row with its buttons in their styles, emoji and disabled state', () => {
    const w = mountRows([row(deploy, cancel, remove), row(go, changelog), row({ type: 4, custom_id: 'x' })])
    expect(w.findAll('[data-testid="component-row"]')).toHaveLength(2)
    expect(button(w, 'Go').classes()).toContain('btn-primary')
    expect(button(w, 'Go').text()).toContain('🚀')
    expect(button(w, 'Cancel').classes()).toContain('btn-secondary')
    expect(button(w, 'Deploy').classes()).toContain('text-mnema-mint')
    expect(button(w, 'Remove').classes()).toContain('btn-danger')
    expect(button(w, 'Remove').attributes('disabled')).toBeDefined()
    expect(button(w, 'Changelog').attributes('aria-label')).toBe('Changelog, opens example.org')
  })

  it('sends a click with its custom id and refuses a second one while it is on its way', async () => {
    vi.useFakeTimers()
    const w = mountRows([row(deploy, cancel)])
    await button(w, 'Deploy').trigger('click')
    await button(w, 'Cancel').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledTimes(1)
    expect(apiMock).toHaveBeenCalledWith(ENDPOINT, expect.objectContaining({ method: 'POST', json: { custom_id: 'deploy:v2.4' } }))
    expect(button(w, 'Deploy').attributes('aria-busy')).toBe('true')
    vi.advanceTimersByTime(3000)
    await flushPromises()
    expect(button(w, 'Deploy').attributes('aria-busy')).toBeUndefined()
    await button(w, 'Cancel').trigger('click')
    expect(apiMock).toHaveBeenCalledTimes(2)
  })

  it.each([
    [409, 'Deployer is not connected right now. Try again later.'],
    [404, 'This button no longer works.'],
    [429, 'Slow down a little, then try again.']
  ])('says quietly why a click was refused (%i)', async (status, text) => {
    apiMock.mockRejectedValue(new ApiError(status, 'x', 'refused'))
    const w = mountRows([row(deploy)])
    await button(w, 'Deploy').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="component-error"]').text()).toBe(text)
    expect(button(w, 'Deploy').attributes('aria-busy')).toBeUndefined()
  })

  it('opens a link button only after the usual question, and never sends it to the server', async () => {
    const w = mountRows([row(changelog)])
    await button(w, 'Changelog').trigger('click')
    await flushPromises()
    expect(confirmMock).toHaveBeenCalledWith(expect.objectContaining({ excerpt: 'https://example.org/changes' }))
    expect(apiMock).not.toHaveBeenCalled()
  })

  it('does not offer a link that is not http or https', () => {
    const w = mountRows([row({ type: 2, style: 5, label: 'Bad', url: 'javascript:alert(1)' })])
    expect(button(w, 'Bad').attributes('disabled')).toBeDefined()
  })
})

describe('components this app does not know', () => {
  it('are left out with one quiet line while the rest still works', async () => {
    const future = { type: 17, custom_id: 'future' } as unknown as ChatComponent
    const oddStyle: ChatComponent = { type: 2, style: 9 as unknown as 1, label: 'Odd', custom_id: 'odd' }
    const withExtra = { ...deploy, sparkle: true } as ChatComponent
    const mentionable = { type: 7, custom_id: 'who' } as unknown as ChatComponent
    const strangeRow = { type: 9, components: [deploy] } as unknown as ChatActionRow
    const w = mountRows([row(withExtra, oddStyle), row(future), row(mentionable), strangeRow])
    expect(w.findAll('[data-testid="component-button"]').map(b => b.text())).toEqual(['Deploy'])
    expect(w.findAll('[data-testid="component-unsupported"]')).toHaveLength(1)
    expect(w.get('[data-testid="component-unsupported"]').text()).toBe('This bot sent something this app cannot show.')
    await button(w, 'Deploy').trigger('click')
    expect(apiMock).toHaveBeenCalledWith(ENDPOINT, expect.objectContaining({ json: { custom_id: 'deploy:v2.4' } }))
  })

  it('show the line alone when nothing can be shown, and nothing when all is known', () => {
    expect(mountRows([row({ type: 17 } as unknown as ChatComponent)]).get('[data-testid="message-components"]').text()).toBe('This bot sent something this app cannot show.')
    expect(mountRows([row(deploy)]).find('[data-testid="component-unsupported"]').exists()).toBe(false)
  })

  it('skip select options without a text label and value', async () => {
    const w = mountRows([row({ type: 3, custom_id: 'env', options: [{ label: 'Staging', value: 'staging' }, { label: 7, value: 'x' } as never, null as never] })])
    await w.get('button[aria-haspopup="listbox"]').trigger('click')
    expect(w.findAll('[role="option"]').map(o => o.text())).toEqual(['Staging'])
  })
})

describe('where components show', () => {
  it('posts a message click to the message route', async () => {
    const msg = messageFixture({ id: fixtureId(7), channel_id: fixtureId(2), is_bot: true, display_name: 'Deployer', components: [row(deploy)] })
    const w = mount(BotMessageComponents, { props: { msg } })
    await w.get('[data-testid="component-button"]').trigger('click')
    expect(apiMock).toHaveBeenCalledWith(`/api/channels/${fixtureId(2)}/messages/${fixtureId(7)}/components`, expect.anything())
  })

  it('shows nothing for a message without components', () => {
    const w = mount(BotMessageComponents, { props: { msg: messageFixture() } })
    expect(w.find('[data-testid="message-components"]').exists()).toBe(false)
  })

  it('posts a private note click to the note route', async () => {
    const chat = useChatStore()
    chat.handleWSEvent({ type: 'interaction_response', payload: { id: fixtureId(9), channel_id: fixtureId(2), bot_id: fixtureId(3), bot_name: 'Deployer', content: 'Sure?', embeds: null, components: [row(deploy)], created_at: '2026-01-01T00:00:00Z' } })
    const w = mount(EphemeralNotes, { props: { channelId: fixtureId(2) } })
    await w.get('[data-testid="component-button"]').trigger('click')
    expect(apiMock).toHaveBeenCalledWith(`/api/channels/${fixtureId(2)}/ephemeral/${fixtureId(9)}/components`, expect.anything())
  })
})
