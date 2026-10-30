import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ChannelAutoDeleteField from './ChannelAutoDeleteField.vue'
import ChannelAutoDeleteNote from './ChannelAutoDeleteNote.vue'
import ChatHeader from './ChatHeader.vue'
import { useChatStore } from '../stores/chat'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { i18nPlugin, setLocale } from '../i18n'
import { tooltip } from '../directives/tooltip'
import { channelFixture } from '../test-fixtures.fixture'

const confirmMock = vi.fn<() => Promise<boolean>>(async () => true)
vi.mock('../lib/confirm', () => ({ confirm: () => confirmMock() }))

let requests: { url: string; method: string; body: unknown }[]

function enable() {
  useServerFeaturesStore().list = ['channels.auto_delete']
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  confirmMock.mockClear()
  requests = []
  vi.stubGlobal('fetch', vi.fn<typeof fetch>(async (url, init) => {
    requests.push({ url: String(url), method: init?.method || 'GET', body: typeof init?.body === 'string' ? JSON.parse(init.body) : undefined })
    const body = String(url).endsWith('/auto-delete') ? channelFixture({ auto_delete_days: 7 }) : { categories: [], uncategorized: [] }
    return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })
  }))
})
afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('ChannelAutoDeleteField', () => {
  const mountField = (days = 0) => mount(ChannelAutoDeleteField, { props: { modelValue: days }, global: { plugins: [i18nPlugin] } })

  it('is absent on a server without the feature', () => {
    expect(mountField().find('[data-testid="auto-delete-field"]').exists()).toBe(false)
  })

  it('offers the periods, has no button of its own and reports the choice', async () => {
    enable()
    const w = mountField()
    expect(w.find('[data-testid="auto-delete-apply"]').exists()).toBe(false)
    expect(w.findAll('option').map(o => o.text())).toEqual(['Off, keep messages', 'After 1 day', 'After 7 days', 'After 30 days', 'After 90 days'])
    await w.get('[data-testid="auto-delete-select"]').setValue('7')
    expect(w.emitted('update:modelValue')).toEqual([[7]])
    expect(requests).toEqual([])
  })

  it('shows the value it is given', () => {
    enable()
    expect((mountField(30).get('[data-testid="auto-delete-select"]').element as HTMLSelectElement).value).toBe('30')
  })
})

describe('confirmAutoDelete', () => {
  it('asks before switching deletion on and not before switching it off', async () => {
    const { confirmAutoDelete } = await import('../lib/autoDelete')
    expect(await confirmAutoDelete('general', 0)).toBe(true)
    expect(confirmMock).not.toHaveBeenCalled()
    confirmMock.mockResolvedValueOnce(false)
    expect(await confirmAutoDelete('general', 7)).toBe(false)
    expect(confirmMock).toHaveBeenCalledTimes(1)
  })
})

describe('auto-delete in the channel header and under it', () => {
  function mountWith(days: number, features = true) {
    if (features) enable()
    const chat = useChatStore()
    chat.activeChannel = channelFixture({ name: 'general', auto_delete_days: days })
    return {
      header: mount(ChatHeader, { global: { plugins: [i18nPlugin], directives: { tooltip } } }),
      note: mount(ChannelAutoDeleteNote, { global: { plugins: [i18nPlugin] } })
    }
  }

  it('states the rule with an icon chip in the header and a line under it', () => {
    const { header, note } = mountWith(7)
    expect(header.get('[data-testid="auto-delete-chip"]').text()).toBe('7 days')
    expect(note.get('[data-testid="auto-delete-note"]').text()).toBe('Messages in this channel are deleted automatically once they are older than 7 days.')
  })

  it('shows nothing when it is off or the server does not know the feature', () => {
    const off = mountWith(0)
    expect(off.header.find('[data-testid="auto-delete-chip"]').exists()).toBe(false)
    expect(off.note.find('[data-testid="auto-delete-note"]').exists()).toBe(false)
    setActivePinia(createPinia())
    const old = mountWith(7, false)
    expect(old.header.find('[data-testid="auto-delete-chip"]').exists()).toBe(false)
    expect(old.note.find('[data-testid="auto-delete-note"]').exists()).toBe(false)
  })

  it('speaks German', () => {
    setLocale('de')
    const { note } = mountWith(30)
    expect(note.text()).toContain('30')
    expect(note.text()).not.toContain('Messages')
  })
})
