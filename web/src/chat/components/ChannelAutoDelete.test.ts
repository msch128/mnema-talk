import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ChannelAutoDeleteField from '../../admin/components/ChannelAutoDeleteField.vue'
import ChannelAutoDeleteNote from './ChannelAutoDeleteNote.vue'
import ChatHeader from './ChatHeader.vue'
import { useChatStore } from '../../stores/chat'
import { useServerFeaturesStore } from '../../stores/serverFeatures'
import { i18nPlugin, setLocale } from '../../i18n'
import { tooltip } from '../../directives/tooltip'
import { channelFixture } from '../../test-fixtures.fixture'

const confirmMock = vi.fn<() => Promise<boolean>>(async () => true)
vi.mock('../../shared/lib/confirm', () => ({ confirm: () => confirmMock() }))

let requests: { url: string; method: string; body: unknown }[]

function enable(v2 = true) {
  useServerFeaturesStore().list = v2 ? ['channels.auto_delete', 'channels.auto_delete.v2'] : ['channels.auto_delete']
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  confirmMock.mockClear()
  requests = []
  vi.stubGlobal('fetch', vi.fn<typeof fetch>(async (url, init) => {
    requests.push({ url: String(url), method: init?.method || 'GET', body: typeof init?.body === 'string' ? JSON.parse(init.body) : undefined })
    return new Response(JSON.stringify({ categories: [], uncategorized: [] }), { status: 200, headers: { 'Content-Type': 'application/json' } })
  }))
})
afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('ChannelAutoDeleteField', () => {
  const mountField = (value = 'off') => mount(ChannelAutoDeleteField, { props: { modelValue: value }, global: { plugins: [i18nPlugin] } })

  it('is absent on a server without the feature', () => {
    expect(mountField().find('[data-testid="auto-delete-field"]').exists()).toBe(false)
  })

  it('offers the default, off and a period of the messages or only the files, and says what will happen', async () => {
    enable()
    const w = mountField('default')
    expect(w.get('[data-testid="auto-delete-summary"]').text()).toBe('This channel follows the community default.')
    await w.get('[data-testid="auto-delete-mode"]').setValue('period')
    expect(w.emitted('update:modelValue')!.at(-1)).toEqual(['168:all'])
    await w.setProps({ modelValue: '36:attachments' })
    expect(w.get('[data-testid="auto-delete-summary"]').text()).toBe('Files in this channel are removed automatically once they are older than 36 hours. The messages stay.')
    expect(w.text()).toContain('The next check is at')
    await w.get('[data-testid="auto-delete-scope"]').setValue('all')
    expect(w.emitted('update:modelValue')!.at(-1)).toEqual(['36:all'])
    expect(requests).toEqual([])
  })

  it('offers an older server only the days it takes', async () => {
    enable(false)
    const w = mountField('off')
    expect(w.findAll('option').map(o => o.text())).toEqual(['Off, keep messages', 'After 1 day', 'After 7 days', 'After 30 days', 'After 90 days'])
    await w.get('[data-testid="auto-delete-select"]').setValue('7')
    expect(w.emitted('update:modelValue')!.at(-1)).toEqual(['168:all'])
  })
})

describe('confirmAutoDelete', () => {
  it('asks with the counts of the dry run, and not when nothing would go', async () => {
    const { confirmAutoDelete } = await import('../../shared/lib/autoDelete')
    expect(await confirmAutoDelete('general', { messages: 0, attachments: 0 })).toBe(true)
    expect(confirmMock).not.toHaveBeenCalled()
    confirmMock.mockResolvedValueOnce(false)
    expect(await confirmAutoDelete('general', { messages: 12, attachments: 3 })).toBe(false)
    expect(confirmMock).toHaveBeenCalledTimes(1)
  })
})

describe('auto-delete in the channel header and under it', () => {
  function mountWith(channel: Parameters<typeof channelFixture>[0], features = true) {
    if (features) enable()
    const chat = useChatStore()
    chat.activeChannel = channelFixture({ name: 'general', ...channel })
    return {
      header: mount(ChatHeader, { global: { plugins: [i18nPlugin], directives: { tooltip } } }),
      note: mount(ChannelAutoDeleteNote, { global: { plugins: [i18nPlugin] } })
    }
  }

  it('states the rule with an icon chip in the header and a line under it', () => {
    const { header, note } = mountWith({ auto_delete_days: 1, auto_delete_hours: 36, auto_delete_scope: 'all' })
    expect(header.get('[data-testid="auto-delete-chip"]').text()).toBe('36 hours')
    expect(note.get('[data-testid="auto-delete-note"]').text()).toBe('Messages in this channel are deleted automatically once they are older than 36 hours.')
  })

  it('reads the days of an older server', () => {
    const { header } = mountWith({ auto_delete_days: 30 })
    expect(header.get('[data-testid="auto-delete-chip"]').text()).toBe('30 days')
  })

  it('shows nothing when it is off or the server does not know the feature', () => {
    const off = mountWith({ auto_delete_days: 0, auto_delete_hours: 0 })
    expect(off.header.find('[data-testid="auto-delete-chip"]').exists()).toBe(false)
    expect(off.note.find('[data-testid="auto-delete-note"]').exists()).toBe(false)
    setActivePinia(createPinia())
    const old = mountWith({ auto_delete_days: 7 }, false)
    expect(old.header.find('[data-testid="auto-delete-chip"]').exists()).toBe(false)
    expect(old.note.find('[data-testid="auto-delete-note"]').exists()).toBe(false)
  })

  it('speaks German', () => {
    setLocale('de')
    const { note } = mountWith({ auto_delete_days: 14, auto_delete_hours: 336, auto_delete_scope: 'attachments' })
    expect(note.text()).toContain('2 Wochen')
    expect(note.text()).not.toContain('Files')
  })
})
