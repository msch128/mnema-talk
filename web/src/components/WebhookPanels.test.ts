import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { ApiOptions } from '../lib/api'
import IncomingWebhooksPanel from './IncomingWebhooksPanel.vue'
import OutgoingWebhooksPanel from './OutgoingWebhooksPanel.vue'
import ChannelWebhooksDialog from './ChannelWebhooksDialog.vue'
import { useToastStore } from '../stores/toast'
import { useChatStore } from '../stores/chat'
import { fixtureId, userFixture } from '../test-fixtures.fixture'
import { setLocale } from '../i18n'
import type { Webhook, OutgoingWebhook, WebhookDelivery } from '../types/webhooks'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn()
}))
const confirmMock = vi.fn<() => Promise<boolean>>()
vi.mock('../lib/confirm', () => ({ confirm: () => confirmMock() }))

const channelId = fixtureId(2)
const stamp = '2026-01-02T03:04:05Z'
const hook = (n: number, extra: Partial<Webhook> = {}): Webhook => ({
  id: fixtureId(40 + n), channel_id: channelId, name: `Hook ${n}`, created_by: null, created_at: stamp, updated_at: stamp,
  allow_everyone: false, allow_attachments: true, allow_embeds: true, allow_user_mentions: true, allow_identity_override: true, ...extra
})
const out = (n: number, extra: Partial<OutgoingWebhook> = {}): OutgoingWebhook => ({
  id: fixtureId(60 + n), channel_id: channelId, created_by: null, name: `Out ${n}`, url: `https://example.org/${n}`, events: ['MESSAGE_CREATE'], status: 'active',
  consecutive_failures: 0, last_status: null, last_error: null, last_attempt_at: null, last_success_at: null, created_at: stamp, url_set_by: null, url_set_at: stamp, ...extra
})
const delivery = (extra: Partial<WebhookDelivery> = {}): WebhookDelivery => ({ id: fixtureId(80), event_id: fixtureId(81), event_type: 'MESSAGE_CREATE', attempt: 1, success: true, status_code: 204, error: null, duration_ms: 5, created_at: stamp, ...extra })
const userFixtureForMember = () => userFixture({ id: fixtureId(7), username: 'max' })
const toastTexts = () => useToastStore().toasts.map(t => t.text)

let clipboard: ReturnType<typeof vi.fn>
beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  confirmMock.mockReset()
  clipboard = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', { value: { writeText: clipboard }, configurable: true })
})

describe('IncomingWebhooksPanel', () => {
  const mountPanel = async (list: Webhook[] = [hook(1, { avatar_url: '/api/media/x' }), hook(2)]) => {
    apiMock.mockResolvedValueOnce(list)
    const w = mount(IncomingWebhooksPanel, { props: { channelId } })
    await flushPromises()
    return w
  }

  it('lists webhooks and shows the empty state', async () => {
    const w = await mountPanel()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/channels/${channelId}/webhooks`, { decode: expect.any(Function) })
    const rows = w.findAll('[data-testid="incoming-webhook"]')
    expect(rows).toHaveLength(2)
    expect(rows[0]?.find('img').attributes('src')).toBe('/api/media/x')
    expect(rows[1]?.text()).toContain('H')
    expect((await mountPanel([])).text()).toContain('No incoming webhooks')
  })

  it('reports a failed load', async () => {
    apiMock.mockRejectedValueOnce(new Error('boom'))
    mount(IncomingWebhooksPanel, { props: { channelId } })
    await flushPromises()
    expect(toastTexts()).toContain('boom')
  })

  it('creates a webhook, shows its URL once and copies it', async () => {
    const w = await mountPanel([])
    await w.find('button[type="submit"]').trigger('submit')
    expect(apiMock).toHaveBeenCalledTimes(1) // nothing typed
    await w.find('input[type="text"]').setValue('  CI  ')
    apiMock.mockResolvedValueOnce({ ...hook(3), token: 't', url: 'https://talk.example/api/webhooks/id/t' }).mockResolvedValueOnce([hook(3)])
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/channels/${channelId}/webhooks`, expect.objectContaining({ method: 'POST', json: { name: 'CI' } }))
    expect(w.find('[data-testid="revealed-url"]').text()).toContain('https://talk.example/api/webhooks/id/t')
    await w.find('[data-testid="revealed-url"] button').trigger('click')
    expect(clipboard).toHaveBeenCalledWith('https://talk.example/api/webhooks/id/t')
    expect(toastTexts()).toContain('Copied')
    clipboard.mockRejectedValueOnce(new Error('denied'))
    await w.find('[data-testid="revealed-url"] button').trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.some(t => t.detail === 'https://talk.example/api/webhooks/id/t')).toBe(true)
  })

  it('shows a creation error', async () => {
    const w = await mountPanel([])
    await w.find('input[type="text"]').setValue('CI')
    apiMock.mockRejectedValueOnce(new Error('conflict'))
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(toastTexts()).toContain('conflict')
    expect(w.find('[data-testid="revealed-url"]').exists()).toBe(false)
  })

  it('renames inline and ignores empty or cancelled names', async () => {
    const w = await mountPanel()
    const rename = () => w.find('button[aria-label="Rename Hook 1"]')
    await rename().trigger('click')
    const input = w.find('li input[type="text"]')
    await input.setValue('Renamed')
    apiMock.mockResolvedValueOnce({ ...hook(1), name: 'Renamed' })
    await w.find('li form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/webhooks/${hook(1).id}`, expect.objectContaining({ method: 'PATCH', json: { name: 'Renamed' } }))
    expect(w.findAll('[data-testid="incoming-name"]')[0]?.text()).toBe('Renamed')

    await w.find('button[aria-label="Rename Hook 2"]').trigger('click')
    await w.find('li input[type="text"]').setValue('  ')
    await w.find('li input[type="text"]').trigger('blur')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledTimes(2) // load + the one rename
    await w.find(`button[aria-label="Rename Renamed"]`).trigger('click')
    await w.find('li input[type="text"]').trigger('keydown.esc')
    expect(w.find('li input[type="text"]').exists()).toBe(false)

    await w.find('button[aria-label="Rename Hook 2"]').trigger('click')
    await w.find('li input[type="text"]').setValue('Nope')
    apiMock.mockRejectedValueOnce(new Error('forbidden'))
    // Enter and the blur that follows it both arrive before the field is gone.
    const field = w.find('li input[type="text"]').element
    field.dispatchEvent(new Event('blur'))
    field.dispatchEvent(new Event('blur'))
    await flushPromises()
    expect(toastTexts()).toContain('forbidden')
  })

  it('makes a new URL after confirming', async () => {
    const w = await mountPanel()
    confirmMock.mockResolvedValueOnce(false)
    await w.find('button[aria-label="New URL for Hook 1"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledTimes(1)
    confirmMock.mockResolvedValueOnce(true)
    apiMock.mockResolvedValueOnce({ ...hook(1), token: 'n', url: 'https://talk.example/api/webhooks/id/n' })
    await w.find('button[aria-label="New URL for Hook 1"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="revealed-url"]').text()).toContain('/n')
    confirmMock.mockResolvedValueOnce(true)
    apiMock.mockRejectedValueOnce(new Error('nope'))
    await w.find('button[aria-label="New URL for Hook 1"]').trigger('click')
    await flushPromises()
    expect(toastTexts()).toContain('nope')
  })

  it('deletes after confirming and hides a revealed URL of that webhook', async () => {
    const w = await mountPanel()
    confirmMock.mockResolvedValueOnce(true)
    apiMock.mockResolvedValueOnce({ ...hook(1), token: 'n', url: 'https://x/n' })
    await w.find('button[aria-label="New URL for Hook 1"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="revealed-url"]').exists()).toBe(true)

    confirmMock.mockResolvedValueOnce(false)
    await w.find('button[aria-label="Delete Hook 2"]').trigger('click')
    await flushPromises()
    expect(w.findAll('[data-testid="incoming-webhook"]')).toHaveLength(2)

    confirmMock.mockResolvedValueOnce(true)
    apiMock.mockResolvedValueOnce(undefined)
    await w.find('button[aria-label="Delete Hook 1"]').trigger('click')
    await flushPromises()
    expect(w.findAll('[data-testid="incoming-webhook"]')).toHaveLength(1)
    expect(w.find('[data-testid="revealed-url"]').exists()).toBe(false)

    confirmMock.mockResolvedValueOnce(true)
    apiMock.mockResolvedValueOnce(undefined)
    await w.find('button[aria-label="Delete Hook 2"]').trigger('click')
    await flushPromises()
    expect(w.findAll('[data-testid="incoming-webhook"]')).toHaveLength(0)
  })

  it('reports a failed deletion', async () => {
    const w = await mountPanel()
    confirmMock.mockResolvedValueOnce(true)
    apiMock.mockRejectedValueOnce(new Error('gone wrong'))
    await w.find('button[aria-label="Delete Hook 1"]').trigger('click')
    await flushPromises()
    expect(toastTexts()).toContain('gone wrong')
    expect(w.findAll('[data-testid="incoming-webhook"]')).toHaveLength(2)
  })

  it('shows and changes the settings of a webhook and who made it', async () => {
    useChatStore().members = [{ ...userFixtureForMember(), display_name: 'Max' }]
    const w = await mountPanel([hook(1, { created_by: fixtureId(7) }), hook(2)])
    expect(w.find('[data-testid="webhook-settings"]').exists()).toBe(false)
    const open = (n: number) => w.find(`button[aria-label="Settings of Hook ${n}"]`)
    await open(1).trigger('click')
    expect(w.find('[data-testid="incoming-audit"]').text()).toBe('Created by Max')
    const box = (key: string) => w.find(`input[data-setting="${key}"]`)
    expect((box('allow_everyone').element as HTMLInputElement).checked).toBe(false)
    expect((box('allow_embeds').element as HTMLInputElement).checked).toBe(true)
    apiMock.mockResolvedValueOnce(hook(1, { created_by: fixtureId(7), allow_everyone: true }))
    ;(box('allow_everyone').element as HTMLInputElement).checked = true
    await box('allow_everyone').trigger('change')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith(`/api/admin/webhooks/${hook(1).id}`, expect.objectContaining({ method: 'PATCH', json: { allow_everyone: true } }))
    expect((box('allow_everyone').element as HTMLInputElement).checked).toBe(true)
    apiMock.mockRejectedValueOnce(new Error('cannot'))
    ;(box('allow_embeds').element as HTMLInputElement).checked = false
    await box('allow_embeds').trigger('change')
    await flushPromises()
    expect(toastTexts()).toContain('cannot')
    // Another webhook, another switch; the same button closes it.
    await open(2).trigger('click')
    expect(w.find('[data-testid="incoming-audit"]').text()).toBe('Created by an unknown member')
    await open(2).trigger('click')
    expect(w.find('[data-testid="webhook-settings"]').exists()).toBe(false)
  })

  it('names a member by username when they have no display name', async () => {
    useChatStore().members = [{ ...userFixtureForMember(), display_name: '' }]
    const w = await mountPanel([hook(1, { created_by: fixtureId(7) })])
    await w.find('button[aria-label="Settings of Hook 1"]').trigger('click')
    expect(w.find('[data-testid="incoming-audit"]').text()).toBe('Created by max')
  })

  it('uploads an avatar', async () => {
    const w = await mountPanel()
    const input = w.find('input[type="file"]')
    const file = new File(['x'], 'a.png', { type: 'image/png' })
    Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
    apiMock.mockResolvedValueOnce({ ...hook(1), avatar_url: '/api/media/new' })
    await input.trigger('change')
    await flushPromises()
    const call = apiMock.mock.calls.at(-1)
    expect(call?.[0]).toBe(`/api/admin/webhooks/${hook(1).id}/avatar`)
    expect(call?.[1]?.form?.get('avatar')).toBe(file)
    expect(w.find('img').attributes('src')).toBe('/api/media/new')

    // No file chosen: nothing happens. A refusal is shown.
    Object.defineProperty(input.element, 'files', { value: [], configurable: true })
    await input.trigger('change')
    expect(apiMock).toHaveBeenCalledTimes(2)
    Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
    apiMock.mockRejectedValueOnce(new Error('too big'))
    await input.trigger('change')
    await flushPromises()
    expect(toastTexts()).toContain('too big')
  })
})

describe('OutgoingWebhooksPanel', () => {
  const mountPanel = async (list: OutgoingWebhook[] = [out(1), out(2, { status: 'auto_disabled', consecutive_failures: 5, last_error: 'http_500', last_attempt_at: stamp }), out(3, { status: 'disabled', last_attempt_at: stamp })], channel: string = channelId) => {
    apiMock.mockResolvedValueOnce(list)
    const w = mount(OutgoingWebhooksPanel, { props: { channelId: channel } })
    await flushPromises()
    return w
  }
  const row = (w: Awaited<ReturnType<typeof mountPanel>>, n: number) => w.findAll('[data-testid="outgoing-webhook"]')[n - 1]!

  it('lists webhooks with their state', async () => {
    const w = await mountPanel()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/channels/${channelId}/outgoing-webhooks`, { decode: expect.any(Function) })
    expect(row(w, 1).find('[data-testid="outgoing-status"]').text()).toBe('Active')
    expect(row(w, 1).find('[data-testid="outgoing-last"]').exists()).toBe(false)
    expect(row(w, 2).text()).toContain('Switched off after 5 failed events')
    expect(row(w, 2).find('[data-testid="outgoing-last"]').text()).toContain('failed: http_500')
    expect(row(w, 3).find('[data-testid="outgoing-status"]').text()).toBe('Off')
    expect(row(w, 3).find('[data-testid="outgoing-last"]').text()).toContain('Last delivery')
    expect(row(w, 1).text()).toContain('Message posted')
    expect((await mountPanel([])).text()).toContain('No outgoing webhooks yet')
  })

  it('shows who last set the URL and offers only channel events', async () => {
    useChatStore().members = [{ ...userFixtureForMember(), id: fixtureId(7), display_name: 'Max' }]
    const w = await mountPanel([out(1, { url_set_by: fixtureId(7) }), out(2)])
    expect(row(w, 1).find('[data-testid="outgoing-audit"]').text()).toContain('URL set by Max')
    expect(row(w, 2).find('[data-testid="outgoing-audit"]').text()).toContain('an unknown member')
    useChatStore().members = [{ ...userFixtureForMember(), id: fixtureId(7), display_name: '', username: 'max' }]
    const byUsername = await mountPanel([out(1, { url_set_by: fixtureId(7) })])
    expect(byUsername.find('[data-testid="outgoing-audit"]').text()).toContain('URL set by max')
    const stranger = await mountPanel([out(1, { url_set_by: fixtureId(9) })])
    expect(stranger.find('[data-testid="outgoing-audit"]').text()).toContain('an unknown member')
    await w.find('[data-testid="outgoing-new"]').trigger('click')
    expect(w.findAll('input[data-event]')).toHaveLength(5)
    expect(w.find('input[data-event="MEMBER_ADD"]').exists()).toBe(false)
  })

  it('reports a failed load', async () => {
    apiMock.mockRejectedValueOnce(new Error('boom'))
    mount(OutgoingWebhooksPanel, { props: { channelId } })
    await flushPromises()
    expect(toastTexts()).toContain('boom')
  })

  it('creates a webhook, picks events and shows the secret once', async () => {
    const w = await mountPanel([])
    await w.find('[data-testid="outgoing-new"]').trigger('click')
    const form = w.find('[data-testid="outgoing-form"]')
    const submit = form.find('button[type="submit"]')
    expect((submit.element as HTMLButtonElement).disabled).toBe(true)
    await form.find('input[type="text"]').setValue(' CI ')
    await form.find('input[type="url"]').setValue(' https://ci.example/hook ')
    await form.find('input[data-event="MESSAGE_DELETE"]').trigger('change')
    await form.find('input[data-event="MESSAGE_CREATE"]').trigger('change') // off
    await form.find('input[data-event="MESSAGE_CREATE"]').trigger('change') // on again
    apiMock.mockResolvedValueOnce({ ...out(4), secret: 'whsec_abc' }).mockResolvedValueOnce([out(4)])
    await form.trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/channels/${channelId}/outgoing-webhooks`, expect.objectContaining({ method: 'POST', json: { name: 'CI', url: 'https://ci.example/hook', events: ['MESSAGE_DELETE', 'MESSAGE_CREATE'] } }))
    expect(w.find('[data-testid="revealed-secret"]').text()).toContain('whsec_abc')
    expect(w.find('[data-testid="outgoing-form"]').exists()).toBe(false)
    await w.find('[data-testid="revealed-secret"] button').trigger('click')
    expect(clipboard).toHaveBeenCalledWith('whsec_abc')
    clipboard.mockRejectedValueOnce(new Error('denied'))
    await w.find('[data-testid="revealed-secret"] button').trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.some(t => t.detail === 'whsec_abc')).toBe(true)
  })

  it('keeps the form open when saving fails, and can be cancelled', async () => {
    const w = await mountPanel([])
    await w.find('[data-testid="outgoing-new"]').trigger('click')
    await w.find('input[type="text"]').setValue('CI')
    await w.find('input[type="url"]').setValue('http://10.0.0.1/')
    apiMock.mockRejectedValueOnce(new Error('url not allowed'))
    await w.find('[data-testid="outgoing-form"]').trigger('submit')
    await flushPromises()
    expect(toastTexts()).toContain('url not allowed')
    expect(w.find('[data-testid="outgoing-form"]').exists()).toBe(true)
    await w.find('[data-testid="outgoing-form"] button[type="button"]').trigger('click')
    expect(w.find('[data-testid="outgoing-form"]').exists()).toBe(false)
    // Submitting a closed form does nothing.
    expect(apiMock).toHaveBeenCalledTimes(2)
  })

  it('edits an existing webhook', async () => {
    const w = await mountPanel()
    await row(w, 1).find('button[aria-label="Edit Out 1"]').trigger('click')
    const form = w.find('[data-testid="outgoing-form"]')
    expect((form.find('input[type="text"]').element as HTMLInputElement).value).toBe('Out 1')
    await form.find('input[type="text"]').setValue('Renamed')
    apiMock.mockResolvedValueOnce(out(1, { name: 'Renamed' })).mockResolvedValueOnce([out(1, { name: 'Renamed' })])
    await form.trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/outgoing-webhooks/${out(1).id}`, expect.objectContaining({ method: 'PATCH', json: expect.objectContaining({ name: 'Renamed' }) }))
    expect(w.find('[data-testid="revealed-secret"]').exists()).toBe(false)
  })

  it('switches a webhook off and on', async () => {
    const w = await mountPanel()
    apiMock.mockResolvedValueOnce(out(1, { status: 'disabled' }))
    await row(w, 1).find('button[aria-label="Switch off Out 1"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/outgoing-webhooks/${out(1).id}`, expect.objectContaining({ json: { enabled: false } }))
    expect(row(w, 1).find('[data-testid="outgoing-status"]').text()).toBe('Off')
    apiMock.mockResolvedValueOnce(out(2))
    await row(w, 2).find('button[aria-label="Switch on Out 2"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith(`/api/admin/outgoing-webhooks/${out(2).id}`, expect.objectContaining({ json: { enabled: true } }))
    apiMock.mockRejectedValueOnce(new Error('no'))
    await row(w, 1).find('button[aria-label="Switch on Out 1"]').trigger('click')
    await flushPromises()
    expect(toastTexts()).toContain('no')
  })

  it('sends a test event and shows the result', async () => {
    const w = await mountPanel()
    apiMock.mockResolvedValueOnce(delivery())
    await row(w, 1).find('button[aria-label="Send a test event to Out 1"]').trigger('click')
    await flushPromises()
    expect(row(w, 1).find('[data-testid="outgoing-test"]').text()).toBe('Test delivered (HTTP 204)')
    apiMock.mockResolvedValueOnce(delivery({ success: false, status_code: null, error: 'blocked_address' }))
    await row(w, 1).find('button[aria-label="Send a test event to Out 1"]').trigger('click')
    await flushPromises()
    expect(row(w, 1).find('[data-testid="outgoing-test"]').text()).toContain('blocked_address')
    apiMock.mockResolvedValueOnce(delivery({ success: true, status_code: null }))
    await row(w, 1).find('button[aria-label="Send a test event to Out 1"]').trigger('click')
    await flushPromises()
    expect(row(w, 1).find('[data-testid="outgoing-test"]').text()).toBe('Test delivered (HTTP 0)')
    apiMock.mockResolvedValueOnce(delivery({ success: false, error: null }))
    await row(w, 1).find('button[aria-label="Send a test event to Out 1"]').trigger('click')
    await flushPromises()
    expect(row(w, 1).find('[data-testid="outgoing-test"]').text()).toBe('Test failed:')
    apiMock.mockRejectedValueOnce(new Error('limited'))
    await row(w, 1).find('button[aria-label="Send a test event to Out 1"]').trigger('click')
    await flushPromises()
    expect(toastTexts()).toContain('limited')
  })

  it('shows and hides the delivery log', async () => {
    const w = await mountPanel()
    const toggle = () => row(w, 1).find('button[aria-label="Recent deliveries of Out 1"]')
    apiMock.mockResolvedValueOnce([])
    await toggle().trigger('click')
    await flushPromises()
    expect(row(w, 1).find('[data-testid="deliveries"]').text()).toContain('Nothing delivered yet')
    await toggle().trigger('click')
    expect(row(w, 1).find('[data-testid="deliveries"]').exists()).toBe(false)
    apiMock.mockResolvedValueOnce([delivery(), delivery({ id: fixtureId(82), success: false, status_code: 503, error: 'http_503', attempt: 2 }), delivery({ id: fixtureId(83), success: true, status_code: null }), delivery({ id: fixtureId(84), success: false, error: null })])
    await toggle().trigger('click')
    await flushPromises()
    const items = row(w, 1).findAll('[data-testid="delivery"]')
    expect(items).toHaveLength(4)
    expect(items[0]?.text()).toContain('204')
    expect(items[1]?.text()).toContain('http_503')
    expect(items[2]?.text()).toContain('OK')
    apiMock.mockRejectedValueOnce(new Error('no log'))
    await row(w, 3).find('button[aria-label="Recent deliveries of Out 3"]').trigger('click')
    await flushPromises()
    expect(toastTexts()).toContain('no log')
  })

  it('rotates the secret after confirming', async () => {
    const w = await mountPanel()
    confirmMock.mockResolvedValueOnce(false)
    await row(w, 1).find('button[aria-label="New secret for Out 1"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="revealed-secret"]').exists()).toBe(false)
    confirmMock.mockResolvedValueOnce(true)
    apiMock.mockResolvedValueOnce({ ...out(1), secret: 'whsec_new' })
    await row(w, 1).find('button[aria-label="New secret for Out 1"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="revealed-secret"]').text()).toContain('whsec_new')
    confirmMock.mockResolvedValueOnce(true)
    apiMock.mockRejectedValueOnce(new Error('rotate failed'))
    await row(w, 1).find('button[aria-label="New secret for Out 1"]').trigger('click')
    await flushPromises()
    expect(toastTexts()).toContain('rotate failed')
  })

  it('deletes after confirming', async () => {
    const w = await mountPanel()
    confirmMock.mockResolvedValueOnce(true)
    apiMock.mockResolvedValueOnce({ ...out(1), secret: 'whsec_new' })
    await row(w, 1).find('button[aria-label="New secret for Out 1"]').trigger('click')
    await flushPromises()
    confirmMock.mockResolvedValueOnce(false)
    await row(w, 1).find('button[aria-label="Delete Out 1"]').trigger('click')
    await flushPromises()
    expect(w.findAll('[data-testid="outgoing-webhook"]')).toHaveLength(3)
    confirmMock.mockResolvedValueOnce(true)
    apiMock.mockResolvedValueOnce(undefined)
    await row(w, 1).find('button[aria-label="Delete Out 1"]').trigger('click')
    await flushPromises()
    expect(w.findAll('[data-testid="outgoing-webhook"]')).toHaveLength(2)
    expect(w.find('[data-testid="revealed-secret"]').exists()).toBe(false)
    confirmMock.mockResolvedValueOnce(true)
    apiMock.mockResolvedValueOnce(undefined)
    await row(w, 1).find('button[aria-label="Delete Out 2"]').trigger('click')
    await flushPromises()
    expect(w.findAll('[data-testid="outgoing-webhook"]')).toHaveLength(1)
    confirmMock.mockResolvedValueOnce(true)
    apiMock.mockRejectedValueOnce(new Error('cannot delete'))
    await row(w, 1).find('button[aria-label="Delete Out 3"]').trigger('click')
    await flushPromises()
    expect(toastTexts()).toContain('cannot delete')
  })
})

describe('ChannelWebhooksDialog', () => {
  it('switches between incoming and outgoing webhooks', async () => {
    apiMock.mockResolvedValue([])
    const w = mount(ChannelWebhooksDialog, { props: { channel: { id: channelId, name: 'general' } }, attachTo: document.body })
    await flushPromises()
    expect(document.body.textContent).toContain('Webhooks of #general')
    expect(w.findComponent(IncomingWebhooksPanel).exists()).toBe(true)
    const tabs = w.findAll('[role="tab"]')
    expect(tabs[0]?.attributes('aria-selected')).toBe('true')
    await tabs[1]?.trigger('click')
    await flushPromises()
    expect(w.findComponent(OutgoingWebhooksPanel).exists()).toBe(true)
    expect(w.findComponent(IncomingWebhooksPanel).exists()).toBe(false)
    w.findComponent({ name: 'BaseDialog' }).vm.$emit('close')
    expect(w.emitted('close')).toHaveLength(1)
    w.unmount()
  })
})
