import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ChannelAutoDeleteField from './ChannelAutoDeleteField.vue'
import ChannelAutoDeleteNote from './ChannelAutoDeleteNote.vue'
import ChatHeader from './ChatHeader.vue'
import { useChatStore } from '../stores/chat'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useToastStore } from '../stores/toast'
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
  it('is absent on a server without the feature', () => {
    const w = mount(ChannelAutoDeleteField, { props: { channel: channelFixture() }, global: { plugins: [i18nPlugin] } })
    expect(w.find('[data-testid="auto-delete-field"]').exists()).toBe(false)
  })

  it('applies nothing until the choice changes, and asks before turning deletion on', async () => {
    enable()
    const channel = channelFixture({ id: 'c1', name: 'general' })
    const w = mount(ChannelAutoDeleteField, { props: { channel }, global: { plugins: [i18nPlugin] } })
    expect(w.get('[data-testid="auto-delete-apply"]').attributes('disabled')).toBeDefined()
    expect(w.findAll('option').map(o => o.text())).toEqual(['Off, keep messages', 'After 1 day', 'After 7 days', 'After 30 days', 'After 90 days'])

    await w.get('[data-testid="auto-delete-select"]').setValue('7')
    confirmMock.mockResolvedValueOnce(false)
    await w.get('[data-testid="auto-delete-apply"]').trigger('click')
    await flushPromises()
    expect(requests).toEqual([])

    await w.get('[data-testid="auto-delete-apply"]').trigger('click')
    await flushPromises()
    expect(requests[0]).toEqual({ url: '/api/admin/channels/c1/auto-delete', method: 'PUT', body: { days: 7 } })
    expect(useToastStore().toasts[0]?.text).toBe('Auto-delete updated.')
  })

  it('settles after saving: the select matches the stored value and Apply is disabled again', async () => {
    enable()
    const chat = useChatStore()
    const stale = channelFixture({ id: 'c1', name: 'general', auto_delete_days: 0 })
    chat.uncategorized = [stale]
    const w = mount(ChannelAutoDeleteField, { props: { channel: stale }, global: { plugins: [i18nPlugin] } })
    await w.get('[data-testid="auto-delete-select"]').setValue('7')
    // The store replaces the channel object, the dialog still holds the old one.
    chat.uncategorized = [channelFixture({ id: 'c1', name: 'general', auto_delete_days: 7 })]
    await flushPromises()
    expect(w.get('[data-testid="auto-delete-apply"]').attributes('disabled')).toBeDefined()
  })

  it('follows a change made elsewhere while nothing is pending', async () => {
    enable()
    const chat = useChatStore()
    const stale = channelFixture({ id: 'c1', auto_delete_days: 0 })
    chat.uncategorized = [stale]
    const w = mount(ChannelAutoDeleteField, { props: { channel: stale }, global: { plugins: [i18nPlugin] } })
    chat.uncategorized = [channelFixture({ id: 'c1', auto_delete_days: 30 })]
    await flushPromises()
    expect((w.get('[data-testid="auto-delete-select"]').element as HTMLSelectElement).value).toBe('30')
  })

  it('turns it off without a warning', async () => {
    enable()
    const w = mount(ChannelAutoDeleteField, { props: { channel: channelFixture({ id: 'c1', auto_delete_days: 30 }) }, global: { plugins: [i18nPlugin] } })
    await w.get('[data-testid="auto-delete-select"]').setValue('0')
    await w.get('[data-testid="auto-delete-apply"]').trigger('click')
    await flushPromises()
    expect(confirmMock).not.toHaveBeenCalled()
    expect(requests[0]?.body).toEqual({ days: 0 })
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
