import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import StatusDialog from './StatusDialog.vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useToastStore } from '../stores/toast'
import { i18nPlugin, setLocale } from '../i18n'
import { userFixture } from '../test-fixtures.fixture'

let wrapper: VueWrapper
let setStatusText: ReturnType<typeof vi.fn>
const me = userFixture({ status_text: '' })

function open(features: string[], user = me) {
  useServerFeaturesStore().list = features
  useAuthStore().user = user
  wrapper = mount(StatusDialog, { attachTo: document.body, global: { plugins: [i18nPlugin] } })
}
const input = () => wrapper.get<HTMLInputElement>('[data-testid="status-input"]')
const expiryChoice = (c: string) => wrapper.get(`[data-expiry="${c}"]`)

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 9, 24, 14, 30, 0))
  const chat = useChatStore()
  setStatusText = vi.fn().mockResolvedValue({ ...me, status_text: 'Lunch' })
  chat.setStatusText = setStatusText as unknown as typeof chat.setStatusText
})
afterEach(() => {
  wrapper.unmount()
  vi.useRealTimers()
  document.body.innerHTML = ''
})

describe('the status dialog', () => {
  it('is titled for setting a status, and for editing once there is one', () => {
    open([])
    expect(wrapper.text()).toContain('Set a status message')
    wrapper.unmount()
    open([], userFixture({ status_text: 'Busy' }))
    expect(wrapper.text()).toContain('Edit status message')
    expect(input().element.value).toBe('Busy')
  })

  it('focuses the text field and counts characters against the server limit', async () => {
    open([])
    await vi.waitFor(() => expect(document.activeElement).toBe(input().element))
    await input().setValue('Lunch')
    expect(wrapper.get('[data-testid="status-count"]').text()).toBe('5/32')
    await input().setValue('x'.repeat(40))
    expect(input().element.value).toHaveLength(32)
    expect(wrapper.get('[data-testid="status-count"]').text()).toBe('32/32')
  })

  it('saves the trimmed text on Enter (submit) without an expiry', async () => {
    open([])
    await input().setValue('  Lunch  ')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(setStatusText).toHaveBeenCalledWith(me.id, 'Lunch', null)
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(useToastStore().toasts.at(-1)).toMatchObject({ type: 'success', text: 'Status saved' })
  })

  it('offers no "clear after" choice on a server without the feature, nor while the text is empty', async () => {
    open([])
    await input().setValue('Lunch')
    expect(wrapper.find('[data-testid="status-expiry"]').exists()).toBe(false)
    wrapper.unmount()
    open(['status.expiry'])
    expect(wrapper.find('[data-testid="status-expiry"]').exists()).toBe(false)
    await input().setValue('Lunch')
    expect(wrapper.findAll('[data-expiry]').map(b => b.text())).toEqual(['Don’t clear', '30 minutes', '1 hour', '4 hours', 'Today', 'This week'])
    expect(expiryChoice('never').attributes('aria-checked')).toBe('true')
  })

  it.each([
    ['min30', new Date(2026, 9, 24, 15, 0)],
    ['hour', new Date(2026, 9, 24, 15, 30)],
    ['hours4', new Date(2026, 9, 24, 18, 30)],
    ['today', new Date(2026, 9, 24, 23, 59)],
    ['week', new Date(2026, 9, 25, 23, 59)]
  ])('sends the absolute local time of "%s"', async (choice, at) => {
    open(['status.expiry'])
    await input().setValue('Lunch')
    await expiryChoice(choice).trigger('click')
    expect(expiryChoice(choice).attributes('aria-checked')).toBe('true')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(setStatusText).toHaveBeenCalledWith(me.id, 'Lunch', at)
  })

  it('does not offer "today" once the day is over', async () => {
    vi.setSystemTime(new Date(2026, 9, 24, 23, 59, 30))
    open(['status.expiry'])
    await input().setValue('Lunch')
    expect(wrapper.find('[data-expiry="today"]').exists()).toBe(false)
    expect(wrapper.find('[data-expiry="hour"]').exists()).toBe(true)
  })

  it('keeps the current expiry when editing a status that has one', async () => {
    const at = new Date(2026, 9, 24, 18, 0).toISOString()
    open(['status.expiry'], userFixture({ status_text: 'Busy', status_expires_at: at }))
    expect(wrapper.get('[data-expiry="keep"]').attributes('aria-checked')).toBe('true')
    await input().setValue('Busier')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(setStatusText).toHaveBeenCalledWith(me.id, 'Busier', new Date(at))
    wrapper.unmount()
    open(['status.expiry'], userFixture({ status_text: 'Busy', status_expires_at: at }))
    await expiryChoice('never').trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(setStatusText).toHaveBeenLastCalledWith(me.id, 'Busy', null)
  })

  it('offers no "keep" without an expiry', () => {
    open(['status.expiry'], userFixture({ status_text: 'Busy' }))
    expect(wrapper.find('[data-expiry="keep"]').exists()).toBe(false)
  })

  it('clears the status with the Clear button, and only offers it when there is one', async () => {
    open([])
    expect(wrapper.find('[data-testid="status-clear"]').exists()).toBe(false)
    wrapper.unmount()
    open([], userFixture({ status_text: 'Busy' }))
    setStatusText.mockResolvedValue({ ...me, status_text: '' })
    await wrapper.get('[data-testid="status-clear"]').trigger('click')
    await flushPromises()
    expect(setStatusText).toHaveBeenCalledWith(me.id, '', null)
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(useToastStore().toasts.at(-1)).toMatchObject({ text: 'Status cleared' })
  })

  it('keeps what was typed and says why when saving fails', async () => {
    open([])
    setStatusText.mockRejectedValue(new Error('Server said no'))
    await input().setValue('Lunch')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[data-testid="status-error"]').text()).toBe('Server said no')
    expect(input().element.value).toBe('Lunch')
    expect(wrapper.get('[data-testid="status-save"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('closes on Escape', async () => {
    open([])
    input().element.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await flushPromises()
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(setStatusText).not.toHaveBeenCalled()
  })

  it('opens the emoji picker and puts the pick at the cursor, within the limit', async () => {
    open([])
    await input().setValue('Lunch')
    input().element.setSelectionRange(2, 2)
    await wrapper.get('[data-testid="status-emoji"]').trigger('click')
    const picker = wrapper.findComponent({ name: 'EmojiPicker' })
    expect(picker.exists()).toBe(true)
    picker.vm.$emit('pick', '🍕')
    await flushPromises()
    expect(input().element.value).toBe('Lu🍕nch')
    expect(wrapper.findComponent({ name: 'EmojiPicker' }).exists()).toBe(false)
    expect(wrapper.get('[data-testid="status-count"]').text()).toBe('6/32')
    await input().setValue('x'.repeat(32))
    await wrapper.get('[data-testid="status-emoji"]').trigger('click')
    wrapper.findComponent({ name: 'EmojiPicker' }).vm.$emit('pick', '🍕')
    await flushPromises()
    expect(input().element.value).toBe('x'.repeat(32))
  })
})
