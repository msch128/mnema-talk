import type { ApiOptions } from '../lib/api'
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { ref } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { useSlashCommands } from './useSlashCommands'
import { setLocale } from '../i18n'
import { useToastStore } from '../stores/toast'
import type { BotChannelCommand } from '../types/rest'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const CH = '00000000-0000-4000-8000-000000000001'
const builtin = (name: string, options: BotChannelCommand['options'] = []): BotChannelCommand => ({
  id: `00000000-0000-4000-8000-00000000000${name.length}`, bot_id: '00000000-0000-0000-0000-000000000000', bot_name: '', builtin: true,
  created_at: '2026-01-01T00:00:00Z', description: `does ${name}`, disabled: false, name, options
})

let text: ReturnType<typeof ref<string>>
const opened: string[] = []

async function typeSlash(s: ReturnType<typeof useSlashCommands>, value: string) {
  text.value = value
  s.onInput()
  await flushPromises()
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  text = ref('')
  opened.length = 0
})

describe('built-in slash commands', () => {
  it('opens the form of a command that has one instead of the option bar', async () => {
    apiMock.mockResolvedValueOnce([builtin('poll', [{ name: 'question', description: 'q', type: 3, required: true }]), builtin('shrug')])
    const s = useSlashCommands(text as never, () => CH, command => { opened.push(command.name) })
    await typeSlash(s, '/po')
    expect(s.suggestions.value.map(c => c.name)).toEqual(['poll'])
    s.pick(s.suggestions.value[0])
    expect(opened).toEqual(['poll'])
    expect(s.picked.value).toBeNull()
    expect(text.value).toBe('')
    expect(s.open.value).toBe(false)
  })

  it('runs a command without a form through the option bar and reads the result, not an interaction', async () => {
    apiMock.mockResolvedValueOnce([builtin('shrug', [{ name: 'text', description: 't', type: 3, required: false }])])
    const s = useSlashCommands(text as never, () => CH, command => { opened.push(command.name) })
    await typeSlash(s, '/shrug')
    s.pick(s.suggestions.value[0])
    expect(opened).toEqual([])
    expect(s.picked.value?.name).toBe('shrug')
    s.values['text'] = 'fine'
    apiMock.mockResolvedValueOnce({ message_id: CH })
    expect(await s.run()).toBe(true)
    const [path, options] = apiMock.mock.lastCall!
    expect(path).toBe(`/api/channels/${CH}/interactions`)
    expect(options?.json).toEqual({ command_id: builtin('shrug').id, options: { text: 'fine' } })
    expect(s.picked.value).toBeNull()
  })

  it('says a command is done when it posted no message (/nick, /timeout, ...)', async () => {
    apiMock.mockResolvedValueOnce([builtin('nick', [{ name: 'name', description: 'n', type: 3, required: true }])])
    const s = useSlashCommands(text as never, () => CH)
    await typeSlash(s, '/nick')
    s.pick(s.suggestions.value[0])
    s.values['name'] = 'Grumpy'
    apiMock.mockResolvedValueOnce({})
    expect(await s.run()).toBe(true)
    expect(useToastStore().toasts.map(t => t.text)).toEqual(['Done.'])
  })

  it('sorts built-ins in server order, so they stay ahead of the bots\' commands', async () => {
    apiMock.mockResolvedValueOnce([builtin('poll'), { ...builtin('pong'), builtin: undefined, bot_name: 'helper' }])
    const s = useSlashCommands(text as never, () => CH)
    await typeSlash(s, '/p')
    expect(s.suggestions.value.map(c => c.name)).toEqual(['poll', 'pong'])
  })
})
