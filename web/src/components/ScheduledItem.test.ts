import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ScheduledItem from './ScheduledItem.vue'
import { useLaterStore } from '../stores/later'
import { useToastStore } from '../stores/toast'
import { setLocale, t } from '../i18n'
import { toLocalInputValue } from '../lib/laterTimes'
import { pendingConfirm } from '../lib/confirm'
import { fixtureId, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'

const inHours = (h: number) => new Date(Date.now() + h * 3600_000)
const item = (over: Record<string, unknown> = {}) => ({
  id: fixtureId(2), channel_id: fixtureId(3), channel_name: 'general', dm: false, parent_id: null, reply_to_id: null, content: 'good morning',
  send_at: inHours(1).toISOString(), state: 'pending', failure_reason: '', created_at: FIXTURE_TIMESTAMP, updated_at: FIXTURE_TIMESTAMP, ...over
}) as never

let later: ReturnType<typeof useLaterStore>
afterEach(() => { pendingConfirm.value = null })
beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  later = useLaterStore()
})

const mountItem = (over: Record<string, unknown> = {}) => mount(ScheduledItem, { props: { item: item(over) } })

describe('ScheduledItem', () => {
  it('shows the channel, or a hint for a direct message or a vanished channel', () => {
    expect(mountItem().text()).toContain('general')
    expect(mountItem({ dm: true }).text()).toContain(t('later.directMessage'))
    expect(mountItem({ channel_name: '' }).text()).toContain(t('later.channelGone'))
  })

  it('marks a refused message as a draft with the reason, and unknown reasons as a plain refusal', () => {
    const w = mountItem({ state: 'failed', failure_reason: 'slow_mode' })
    expect(w.text()).toContain(t('later.draft'))
    expect(w.get('[data-testid=scheduled-failed]').text()).toContain(t('later.reason.slow_mode'))
    const odd = mountItem({ state: 'failed', failure_reason: '<b>x</b>' })
    expect(odd.get('[data-testid=scheduled-failed]').text()).toContain(t('later.reason.refused'))
    expect(mountItem().find('[data-testid=scheduled-failed]').exists()).toBe(false)
  })

  it('saves only the text when only the text changed', async () => {
    const update = vi.spyOn(later, 'updateScheduled').mockResolvedValue(item())
    const w = mountItem({ send_at: new Date(inHours(1).setSeconds(0, 0)).toISOString() })
    await w.get('[data-testid=scheduled-edit]').trigger('click')
    expect((w.get('[data-testid=scheduled-when]').element as HTMLInputElement).value).not.toBe('')
    await w.get('[data-testid=scheduled-text]').setValue('  good evening ')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(update).toHaveBeenCalledWith(fixtureId(2), { content: 'good evening' })
    expect(w.find('form').exists()).toBe(false)
  })

  it('keeps an untouched time as it is instead of reading it back from the clock', async () => {
    // The input shows minutes only, so a time with seconds does not survive a
    // round trip; the same happens to a time in the hour that repeats when
    // daylight saving ends. An untouched field must not move the message.
    const update = vi.spyOn(later, 'updateScheduled').mockResolvedValue(item())
    const w = mountItem({ send_at: new Date(inHours(3).setSeconds(30, 0)).toISOString() })
    await w.get('[data-testid=scheduled-edit]').trigger('click')
    await w.get('[data-testid=scheduled-text]').setValue('only the words')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(update).toHaveBeenCalledWith(fixtureId(2), { content: 'only the words' })
  })

  it('saves a new time and leaves an unchanged text out', async () => {
    const update = vi.spyOn(later, 'updateScheduled').mockResolvedValue(item())
    const w = mountItem()
    await w.get('[data-testid=scheduled-edit]').trigger('click')
    await w.get('[data-testid=scheduled-when]').setValue(toLocalInputValue(inHours(2)))
    await w.get('form').trigger('submit')
    await flushPromises()
    const patch = update.mock.calls[0]![1]
    expect(patch.content).toBeUndefined()
    expect(patch.at).toBeInstanceOf(Date)
  })

  it('keeps Save off and sends nothing while nothing changed', async () => {
    const update = vi.spyOn(later, 'updateScheduled').mockResolvedValue(item())
    const w = mountItem({ send_at: new Date(inHours(1).setSeconds(0, 0)).toISOString() })
    await w.get('[data-testid=scheduled-edit]').trigger('click')
    expect(w.get('[data-testid=scheduled-save]').attributes('disabled')).toBeDefined()
    await w.get('[data-testid=scheduled-text]').setValue('  good morning ')
    expect(w.get('[data-testid=scheduled-save]').attributes('disabled')).toBeDefined()
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(update).not.toHaveBeenCalled()
  })

  it('confirms a saved edit with a toast and closes the editor', async () => {
    vi.spyOn(later, 'updateScheduled').mockResolvedValue(item())
    const w = mountItem()
    await w.get('[data-testid=scheduled-edit]').trigger('click')
    await w.get('[data-testid=scheduled-text]').setValue('new words')
    await w.get('[data-testid=scheduled-save]').trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.map(x => x.text)).toContain(t('draft.saved'))
    expect(w.find('form').exists()).toBe(false)
  })

  it('asks before Escape or Cancel drops an edit, and not when nothing changed', async () => {
    const w = mountItem()
    await w.get('[data-testid=scheduled-edit]').trigger('click')
    await w.get('[data-testid=scheduled-text]').trigger('keydown', { key: 'Escape' })
    await flushPromises()
    expect(pendingConfirm.value).toBeNull()
    expect(w.find('form').exists()).toBe(false)

    await w.get('[data-testid=scheduled-edit]').trigger('click')
    await w.get('[data-testid=scheduled-text]').setValue('changed')
    await w.get('[data-testid=scheduled-text]').trigger('keydown', { key: 'Escape' })
    await flushPromises()
    expect(pendingConfirm.value?.title).toBe(t('draft.discardTitle'))
    pendingConfirm.value?.resolve(false)
    await flushPromises()
    expect(w.find('form').exists()).toBe(true)
    await w.findAll('button').find(b => b.text() === t('common.cancel'))!.trigger('click')
    await flushPromises()
    pendingConfirm.value?.resolve(true)
    await flushPromises()
    expect(w.find('form').exists()).toBe(false)
    await w.get('[data-testid=scheduled-edit]').trigger('click')
    expect((w.get('[data-testid=scheduled-text]').element as HTMLTextAreaElement).value).toBe('good morning')
  })

  it('asks a failed draft for a new time and sends it with the text', async () => {
    const update = vi.spyOn(later, 'updateScheduled').mockResolvedValue(item())
    const w = mountItem({ state: 'failed', failure_reason: 'not_allowed' })
    await w.get('[data-testid=scheduled-edit]').trigger('click')
    expect((w.get('[data-testid=scheduled-when]').element as HTMLInputElement).value).toBe('')
    await w.get('[data-testid=scheduled-when]').setValue(toLocalInputValue(inHours(3)))
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(update.mock.calls[0]![1].at).toBeInstanceOf(Date)
  })

  it('rejects a time in the past and keeps the editor open', async () => {
    const update = vi.spyOn(later, 'updateScheduled')
    const w = mountItem()
    await w.get('[data-testid=scheduled-edit]').trigger('click')
    await w.get('[data-testid=scheduled-when]').setValue(toLocalInputValue(inHours(-1)))
    await w.get('form').trigger('submit')
    expect(w.get('[role=alert]').text()).toBe(t('later.pastTime'))
    expect(update).not.toHaveBeenCalled()
  })

  it('does not save an empty text', async () => {
    const update = vi.spyOn(later, 'updateScheduled')
    const w = mountItem()
    await w.get('[data-testid=scheduled-edit]').trigger('click')
    await w.get('[data-testid=scheduled-text]').setValue('   ')
    await w.get('form').trigger('submit')
    expect(update).not.toHaveBeenCalled()
    expect(w.get('[data-testid=scheduled-save]').attributes('disabled')).toBeDefined()
  })

  it('shows why saving failed and lets the member cancel', async () => {
    vi.spyOn(later, 'updateScheduled').mockRejectedValue(new Error('boom'))
    const w = mountItem()
    await w.get('[data-testid=scheduled-edit]').trigger('click')
    await w.get('[data-testid=scheduled-text]').setValue('other words')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.get('[role=alert]').text()).toBe('boom')
    expect(w.find('form').exists()).toBe(true)
  })

  it('sends one delete however often the button is pressed', async () => {
    let finish!: () => void
    const remove = vi.spyOn(later, 'removeScheduled').mockImplementation(() => new Promise<void>(resolve => { finish = resolve }))
    const w = mountItem()
    await w.get('[data-testid=scheduled-delete]').trigger('click')
    await w.get('[data-testid=scheduled-delete]').trigger('click')
    expect(remove).toHaveBeenCalledTimes(1)
    expect(w.get('[data-testid=scheduled-delete]').attributes('disabled')).toBeDefined()
    finish()
    await flushPromises()
    expect(w.get('[data-testid=scheduled-delete]').attributes('disabled')).toBeUndefined()
  })

  it('deletes the entry, and toasts when that fails', async () => {
    const remove = vi.spyOn(later, 'removeScheduled').mockResolvedValue()
    const w = mountItem()
    await w.get('[data-testid=scheduled-delete]').trigger('click')
    expect(remove).toHaveBeenCalledWith(fixtureId(2))
    remove.mockRejectedValue(new Error('nope'))
    await w.get('[data-testid=scheduled-delete]').trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.at(-1)?.type).toBe('error')
  })
})
