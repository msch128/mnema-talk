import type { ApiOptions } from '../lib/api'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { ref } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { useSlashCommands } from './useSlashCommands'
import { useToastStore } from '../stores/toast'
import { setLocale } from '../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const ID = '00000000-0000-4000-8000-000000000001'
const wire = (name: string, options: unknown[] = []) => ({
  id: `00000000-0000-4000-8000-0000000000${name.length}${name.charCodeAt(0) % 10}`, bot_id: ID, bot_name: 'helper', created_at: '2026-01-01T00:00:00Z',
  description: 'd', disabled: false, name, options
})
const ping = wire('ping', [{ name: 'text', description: 't', type: 3, required: true }, { name: 'n', description: 'n', type: 4, required: false }])
const key = (k: string, init: KeyboardEventInit = {}) => new KeyboardEvent('keydown', { key: k, cancelable: true, ...init })

let channel: string | undefined
let text: ReturnType<typeof ref<string>>
const setup = () => useSlashCommands(text as never, () => channel)
const toasts = () => useToastStore().toasts.map(t => t.text)

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  channel = ID
  text = ref('')
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-10-18T10:00:00Z'))
})
afterEach(() => vi.useRealTimers())

async function typeSlash(s: ReturnType<typeof setup>, value: string) {
  text.value = value
  s.onInput()
  await flushPromises()
}

describe('useSlashCommands', () => {
  it('loads the channel commands when a slash is typed and filters them', async () => {
    apiMock.mockResolvedValue([ping, wire('help')])
    const s = setup()
    await typeSlash(s, 'hello')
    expect(apiMock).not.toHaveBeenCalled()
    expect(s.open.value).toBe(false)

    await typeSlash(s, '/')
    expect(apiMock).toHaveBeenCalledWith(`/api/channels/${ID}/commands`, expect.anything())
    expect(s.suggestions.value.map(c => c.name)).toEqual(['ping', 'help'])
    await typeSlash(s, '/he')
    expect(s.suggestions.value.map(c => c.name)).toEqual(['help'])
    expect(apiMock).toHaveBeenCalledTimes(1) // reused while fresh
    vi.setSystemTime(new Date('2026-10-18T10:05:00Z'))
    await typeSlash(s, '/p')
    expect(apiMock).toHaveBeenCalledTimes(2)
  })

  it('offers nothing when the list cannot be loaded, or without a channel', async () => {
    apiMock.mockRejectedValue(new Error('down'))
    const s = setup()
    await typeSlash(s, '/')
    expect(s.open.value).toBe(false)
    expect(s.commands.value).toEqual([])
    channel = undefined
    await typeSlash(s, '/x')
    expect(apiMock).toHaveBeenCalledTimes(1)
  })

  it('moves with the arrow keys, picks with Enter or Tab, closes with Escape', async () => {
    apiMock.mockResolvedValue([ping, wire('pong'), wire('help')])
    const s = setup()
    await typeSlash(s, '/p')
    expect(s.suggestions.value.map(c => c.name)).toEqual(['ping', 'pong', 'help'])
    expect(s.onKeydown(key('ArrowDown'))).toBe(true)
    expect(s.active.value).toBe(1)
    expect(s.onKeydown(key('ArrowDown'))).toBe(true)
    expect(s.onKeydown(key('ArrowDown'))).toBe(true)
    expect(s.active.value).toBe(0)
    expect(s.onKeydown(key('ArrowUp'))).toBe(true)
    expect(s.active.value).toBe(2)
    expect(s.onKeydown(key('a'))).toBe(false)
    expect(s.onKeydown(key('Escape'))).toBe(true)
    expect(s.open.value).toBe(false)
    await typeSlash(s, '/pi')
    expect(s.open.value).toBe(true)
    expect(s.onKeydown(key('Tab'))).toBe(true)
    expect(s.picked.value?.name).toBe('ping')
    expect(text.value).toBe('')

    await typeSlash(s, '/po')
    s.close()
    expect(s.open.value).toBe(false)
    await typeSlash(s, '/pon')
    expect(s.onKeydown(key('Enter'))).toBe(true)
    expect(s.picked.value?.name).toBe('pong')
    s.pick(undefined)
    expect(s.picked.value?.name).toBe('pong')
  })

  it('runs a picked command with its options and clears it', async () => {
    apiMock.mockResolvedValueOnce([ping])
    const s = setup()
    await typeSlash(s, '/ping')
    s.pick(s.suggestions.value[0])
    s.values['text'] = 'hello'
    s.values['n'] = '3'
    apiMock.mockResolvedValueOnce({ interaction_id: ID })
    expect(await s.run()).toBe(true)
    expect(apiMock).toHaveBeenLastCalledWith(`/api/channels/${ID}/interactions`, expect.objectContaining({
      method: 'POST', json: { command_id: ping.id, options: { text: 'hello', n: 3 } }
    }))
    expect(s.picked.value).toBeNull()
    expect(Object.keys(s.values)).toEqual([])
    expect(await s.run()).toBe(false) // nothing picked
  })

  it('tells what is missing before it asks the server', async () => {
    apiMock.mockResolvedValueOnce([ping])
    const s = setup()
    await typeSlash(s, '/ping')
    s.pick(s.suggestions.value[0])
    expect(await s.run()).toBe(false)
    expect(toasts()).toEqual(['Fill in “text” first.'])
    s.values['text'] = 'x'
    s.values['n'] = '1.5'
    expect(await s.run()).toBe(false)
    expect(toasts()[1]).toBe('“n” needs a whole number.')
    expect(apiMock).toHaveBeenCalledTimes(1)
  })

  it('explains an offline bot and other failures, and stays picked', async () => {
    apiMock.mockResolvedValueOnce([ping])
    const s = setup()
    await typeSlash(s, '/ping')
    s.pick(s.suggestions.value[0])
    s.values['text'] = 'x'
    const { ApiError } = await import('../lib/api')
    apiMock.mockRejectedValueOnce(new ApiError(409, 'CONFLICT', 'the bot is not connected'))
    expect(await s.run()).toBe(false)
    expect(toasts()).toEqual(['helper is not connected right now.'])
    apiMock.mockRejectedValueOnce(new ApiError(429, 'RATE_LIMITED', 'too many requests'))
    expect(await s.run()).toBe(false)
    apiMock.mockRejectedValueOnce(undefined)
    expect(await s.run()).toBe(false)
    expect(toasts()[2]).toBe('Couldn’t run the command. Try again.')
    expect(s.picked.value?.name).toBe('ping')
    expect(s.running.value).toBe(false)
  })

  it('runs with Enter on an empty composer, cancels with Escape, leaves typed text alone', async () => {
    apiMock.mockResolvedValueOnce([wire('hi')])
    const s = setup()
    await typeSlash(s, '/hi')
    s.pick(s.suggestions.value[0])
    apiMock.mockResolvedValueOnce({ interaction_id: ID })
    expect(s.onKeydown(key('Enter'))).toBe(true)
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith(`/api/channels/${ID}/interactions`, expect.anything())

    await typeSlash(s, '/hi')
    s.pick(s.suggestions.value[0])
    text.value = 'a message'
    expect(s.onKeydown(key('Enter'))).toBe(false)
    expect(s.onKeydown(key('Enter', { shiftKey: true }))).toBe(false)
    text.value = ''
    expect(s.onKeydown(key('Escape'))).toBe(true)
    expect(s.picked.value).toBeNull()
    expect(s.onKeydown(key('Escape'))).toBe(false)
  })

  it('does not run twice at once', async () => {
    apiMock.mockResolvedValueOnce([wire('hi')])
    const s = setup()
    await typeSlash(s, '/hi')
    s.pick(s.suggestions.value[0])
    let finish: (v: unknown) => void = () => {}
    apiMock.mockReturnValueOnce(new Promise(r => { finish = r }))
    const first = s.run()
    expect(await s.run()).toBe(false)
    finish({ interaction_id: ID })
    expect(await first).toBe(true)
  })
})
