import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { ApiOptions } from '../../shared/lib/api'
import { ApiError } from '../../shared/lib/api'
import BotModalDialog from './BotModalDialog.vue'
import { useChatStore, type BotModal } from '../../stores/chat'
import { useToastStore } from '../../stores/toast'
import { setLocale } from '../../i18n'
import { maxChars } from '../../directives/maxChars'
import { fixtureId } from '../../test-fixtures.fixture'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../../shared/lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../shared/lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))
const confirmMock = vi.fn<() => Promise<boolean>>()
vi.mock('../../shared/lib/confirm', () => ({ confirm: () => confirmMock() }))

const modal: BotModal = {
  id: fixtureId(80), channel_id: fixtureId(2), bot_id: fixtureId(3), bot_name: 'Feedback', bot_avatar_url: '', title: 'Feedback',
  components: [
    { type: 1, components: [{ type: 4, custom_id: 'subject', label: 'Subject', style: 1, max_length: 80, min_length: 3, value: 'Login' }] },
    { type: 1, components: [{ type: 4, custom_id: 'body', label: 'What happened?', style: 2, required: false, placeholder: 'Tell us more' }] }
  ]
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset().mockResolvedValue(undefined)
  confirmMock.mockReset().mockResolvedValue(true)
})

const mountModal = () => mount(BotModalDialog, { props: { modal }, attachTo: document.body, global: { directives: { maxChars, tooltip: () => undefined } } })
const field = (name: string) => document.querySelector<HTMLInputElement | HTMLTextAreaElement>(`[data-input="${name}"]`)!
const typeInto = async (name: string, value: string) => {
  const el = field(name)
  el.value = value
  el.dispatchEvent(new Event('input'))
  await flushPromises()
}

describe('a form a bot opened', () => {
  it('shows its title, the bot and its inputs with labels, placeholders, prefilled values and a counter', () => {
    const w = mountModal()
    const dialog = document.querySelector('[data-testid="bot-modal"]')!
    expect(document.body.textContent).toContain('Feedback')
    expect(dialog.textContent).toContain('A form from Feedback')
    expect(field('subject').value).toBe('Login')
    expect(field('subject').tagName).toBe('INPUT')
    expect(field('body').tagName).toBe('TEXTAREA')
    expect(field('body').getAttribute('placeholder')).toBe('Tell us more')
    expect(dialog.querySelector('label[for$="-body"]')?.textContent).toContain('(optional)')
    expect(dialog.querySelector('[data-testid="bot-modal-count"]')?.textContent).toBe('5/80')
    w.unmount()
  })

  it('sends the values once they fit, and closes', async () => {
    const w = mountModal()
    await typeInto('body', 'It broke')
    document.querySelector<HTMLButtonElement>('[data-testid="bot-modal-submit"]')!.click()
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/channels/${fixtureId(2)}/modals/${fixtureId(80)}`, expect.objectContaining({ method: 'POST', json: { values: { subject: 'Login', body: 'It broke' } } }))
    expect(w.emitted('close')).toHaveLength(1)
    w.unmount()
  })

  it('keeps a required or too short input from being sent and points at it', async () => {
    const w = mountModal()
    await typeInto('subject', 'ab')
    document.querySelector<HTMLButtonElement>('[data-testid="bot-modal-submit"]')!.click()
    await flushPromises()
    expect(apiMock).not.toHaveBeenCalled()
    expect(field('subject').getAttribute('aria-invalid')).toBe('true')
    expect(document.activeElement).toBe(field('subject'))
    expect(document.body.textContent).toContain('At least 3 characters.')
    await typeInto('subject', '')
    expect(document.body.textContent).toContain('Fill this in.')
    expect(field('body').getAttribute('aria-invalid')).toBeNull()
    w.unmount()
  })

  it('says when the form expired and closes', async () => {
    apiMock.mockRejectedValue(new ApiError(404, 'not_found', 'unknown modal'))
    const w = mountModal()
    document.querySelector<HTMLButtonElement>('[data-testid="bot-modal-submit"]')!.click()
    await flushPromises()
    expect(useToastStore().toasts.map(t => t.text)).toContain('This form expired. Start again from the bot.')
    expect(w.emitted('close')).toHaveLength(1)
    w.unmount()
  })

  it('shows another refusal inside the form and stays open', async () => {
    apiMock.mockRejectedValue(new ApiError(400, 'bad', 'something brand new happened'))
    const w = mountModal()
    document.querySelector<HTMLButtonElement>('[data-testid="bot-modal-submit"]')!.click()
    await flushPromises()
    expect(document.querySelector('[data-testid="bot-modal-error"]')).not.toBeNull()
    expect(w.emitted('close')).toBeUndefined()
    w.unmount()
  })

  it('sends nothing when closed with Escape, and asks first when something was typed', async () => {
    const w = mountModal()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(confirmMock).not.toHaveBeenCalled()
    expect(w.emitted('close')).toHaveLength(1)
    w.unmount()
    const again = mountModal()
    await typeInto('body', 'draft')
    confirmMock.mockResolvedValue(false)
    document.querySelector<HTMLButtonElement>('[data-testid="bot-modal-cancel"]')!.click()
    await flushPromises()
    expect(confirmMock).toHaveBeenCalled()
    expect(again.emitted('close')).toBeUndefined()
    expect(apiMock).not.toHaveBeenCalled()
    again.unmount()
  })
})

describe('a form with parts this app does not know', () => {
  it('still shows the text inputs it knows', () => {
    const odd = { ...modal, components: [{ type: 1, components: [{ type: 99, custom_id: 'pick' }] }, { type: 1, components: [{ type: 4, custom_id: 'note', label: 'Note', style: 1, max_length: 'long', extra: 1 }] }] } as unknown as BotModal
    const w = mount(BotModalDialog, { props: { modal: odd }, attachTo: document.body, global: { directives: { maxChars, tooltip: () => undefined } } })
    expect(document.querySelectorAll('[data-input]')).toHaveLength(1)
    expect(field('note')).not.toBeNull()
    expect(document.querySelector('[data-testid="bot-modal-count"]')).toBeNull()
    w.unmount()
  })
})

describe('the form events', () => {
  it('open the newest form and are forgotten with the community', () => {
    const chat = useChatStore()
    chat.handleWSEvent({ type: 'interaction_modal', payload: modal })
    expect(chat.botModal?.id).toBe(modal.id)
    chat.resetCommunityState()
    expect(chat.botModal).toBeNull()
  })
})
