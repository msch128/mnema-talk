import type { ApiOptions } from '../lib/api'

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ForwardedQuote from './ForwardedQuote.vue'
import ForwardDialog from './ForwardDialog.vue'
import { useChatStore } from '../stores/chat'
import { useForwardQuotesStore } from '../stores/forwardQuotes'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { closeForward, openForward, pendingForward } from '../lib/forwardDialog'
import { t } from '../i18n'
import { channelFixture, fixtureId, messageFixture, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

enableAutoUnmount(afterEach)
const forwarded = messageFixture({ id: fixtureId(20), content: 'my comment', forward: { forwarded: true } })
const available = {
  available: true, message_id: fixtureId(21), channel_id: fixtureId(22), channel_name: 'staff', content: 'the **original**',
  author: { id: fixtureId(5), username: 'alice', display_name: 'Alice' }, created_at: FIXTURE_TIMESTAMP, is_edited: true
}
const stubs = { global: { directives: { tooltip: () => undefined } } }

beforeEach(() => {
  setActivePinia(createPinia())
  apiMock.mockReset()
  closeForward()
})

describe('ForwardedQuote', () => {
  it('shows the original this reader may see and opens it', async () => {
    apiMock.mockResolvedValue(available)
    const go = vi.spyOn(useChatStore(), 'goToMessage').mockImplementation(() => undefined)
    const w = mount(ForwardedQuote, { props: { msg: forwarded }, ...stubs })
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/messages/${fixtureId(20)}/forward`, expect.anything())
    const box = w.find('[data-testid="forwarded"]')
    expect(box.text()).toContain('original')
    expect(box.text()).toContain('Alice')
    expect(box.text()).toContain('staff')
    await w.find('[data-testid="forwarded-open"]').trigger('click')
    expect(go).toHaveBeenCalledWith({ id: fixtureId(21), channel_id: fixtureId(22) })
  })

  it('says only "not available" when the server withholds the original, and "deleted" when it is gone', async () => {
    apiMock.mockResolvedValueOnce({ available: false })
    const w = mount(ForwardedQuote, { props: { msg: forwarded }, ...stubs })
    await flushPromises()
    expect(w.find('[data-testid="forwarded-unavailable"]').text()).toBe(t('forward.unavailable'))
    expect(w.find('[data-testid="forwarded"]').text()).not.toContain('original')
    apiMock.mockResolvedValueOnce({ available: false, deleted: true })
    useForwardQuotesStore().invalidate()
    await flushPromises()
    expect(w.find('[data-testid="forwarded-gone"]').text()).toBe(t('forward.deleted'))
  })

  it('asks again after an edit or a delete, and shows nothing while the quote cannot be read', async () => {
    apiMock.mockRejectedValueOnce(new Error('offline'))
    const w = mount(ForwardedQuote, { props: { msg: forwarded }, ...stubs })
    await flushPromises()
    expect(w.find('[data-testid="forwarded-unavailable"]').exists()).toBe(false)
    apiMock.mockResolvedValueOnce(available)
    useChatStore().handleWSEvent({ type: 'message_delete', payload: { id: fixtureId(99), channel_id: fixtureId(2), parent_id: null } })
    await flushPromises()
    expect(w.find('[data-testid="forwarded-open"]').exists()).toBe(true)
  })

  it('does not link an original without ids, and does not ask twice for one message', async () => {
    apiMock.mockResolvedValue({ ...available, message_id: undefined, channel_id: undefined, is_edited: false })
    const quotes = useForwardQuotesStore()
    const w = mount(ForwardedQuote, { props: { msg: forwarded }, ...stubs })
    await Promise.all([quotes.ensure(forwarded.id), quotes.ensure(forwarded.id)])
    await flushPromises()
    expect(apiMock).toHaveBeenCalledTimes(1)
    expect(w.find('[data-testid="forwarded-open"]').exists()).toBe(false)
  })
})

describe('ForwardedQuote details', () => {
  it('shows an attachment-only original without a link when it has no ids, and ignores a quote that went stale', async () => {
    const quotes = useForwardQuotesStore()
    let late: (value: unknown) => void = () => undefined
    apiMock.mockReturnValueOnce(new Promise(resolve => { late = resolve }))
    const first = quotes.ensure(fixtureId(40))
    quotes.invalidate()
    late(available)
    await first
    expect(quotes.quoteOf(fixtureId(40))).toBeNull()

    apiMock.mockResolvedValue({ available: true, content: '', author: { id: fixtureId(5), username: 'alice', display_name: '' } })
    const w = mount(ForwardedQuote, { props: { msg: forwarded }, ...stubs })
    await flushPromises()
    expect(w.text()).toContain(t('chat.attachment'))
    expect(w.text()).toContain('alice')
    expect(w.find('[data-testid="forwarded-open"]').exists()).toBe(false)
  })
})

describe('ForwardDialog', () => {
  function setup() {
    const chat = useChatStore()
    chat.uncategorized = [channelFixture({ id: fixtureId(2), name: 'general' }), channelFixture({ id: fixtureId(3), name: 'readonly' })]
    chat.activeChannel = chat.uncategorized[0] ?? null
    vi.spyOn(usePermissionsStore(), 'canIn').mockImplementation((id: string) => id === fixtureId(2))
  }

  it('forwards to a channel the member may write in, with a comment', async () => {
    setup()
    apiMock.mockResolvedValue(messageFixture())
    const success = vi.spyOn(useToastStore(), 'success')
    openForward({ messageId: fixtureId(20), excerpt: 'hello' })
    const w = mount(ForwardDialog, { attachTo: document.body, ...stubs })
    await flushPromises()
    expect(document.body.textContent).toContain('hello')
    expect(document.querySelector('[data-testid="forward-target-readonly"]')).toBeNull()
    const comment = document.querySelector<HTMLTextAreaElement>('[data-testid="forward-comment"]')!
    comment.value = ' look '
    comment.dispatchEvent(new Event('input'))
    document.querySelector<HTMLFormElement>('[data-testid="forward-dialog"]')!.dispatchEvent(new Event('submit'))
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/messages/${fixtureId(20)}/forward`, expect.objectContaining({ method: 'POST', json: { channel_id: fixtureId(2), comment: 'look' } }))
    expect(success).toHaveBeenCalledWith(t('forward.done'))
    expect(pendingForward.value).toBeNull()
    w.unmount()
  })

  it('filters the channels, says when none is left and closes on cancel', async () => {
    setup()
    openForward({ messageId: fixtureId(20), excerpt: 'hello' })
    const w = mount(ForwardDialog, { attachTo: document.body, ...stubs })
    await flushPromises()
    const filter = document.querySelector<HTMLInputElement>('[data-testid="forward-filter"]')!
    filter.value = 'zzz'
    filter.dispatchEvent(new Event('input'))
    await flushPromises()
    expect(document.body.textContent).toContain(t('forward.none'))
    expect(document.querySelector<HTMLButtonElement>('[data-testid="forward-send"]')!.disabled).toBe(false)
    const cancel = [...document.querySelectorAll('button')].find(b => b.textContent === t('common.cancel'))!
    cancel.click()
    expect(pendingForward.value).toBeNull()
    w.unmount()
  })

  it('keeps the dialog open and shows the refusal', async () => {
    setup()
    apiMock.mockRejectedValue(new Error('Couldn’t forward this message.'))
    openForward({ messageId: fixtureId(20), excerpt: 'hello' })
    const w = mount(ForwardDialog, { attachTo: document.body, ...stubs })
    await flushPromises()
    document.querySelector<HTMLFormElement>('[data-testid="forward-dialog"]')!.dispatchEvent(new Event('submit'))
    await flushPromises()
    expect(document.querySelector('[data-testid="forward-error"]')?.textContent).toContain('Couldn’t forward')
    expect(pendingForward.value).not.toBeNull()
    w.unmount()
  })
})
