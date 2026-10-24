import type { VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import EventEditDialog from './EventEditDialog.vue'
import { useEventsStore } from '../stores/events'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { setLocale } from '../i18n'
import { categoryFixture, channelFixture, fixtureId, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'
import type { CommunityEvent } from '../types/domain'

function makeEvent(overrides: Partial<CommunityEvent> = {}): CommunityEvent {
  return {
    id: fixtureId(1),
    title: 'Game Night',
    description: 'Playing games',
    start_time: '2026-11-01T18:00:00Z',
    status: 'scheduled',
    created_at: FIXTURE_TIMESTAMP,
    updated_at: FIXTURE_TIMESTAMP,
    rsvp_counts: { going: 0, interested: 0, not_going: 0 },
    ...overrides
  }
}

const talkId = fixtureId(10)

describe('EventEditDialog', () => {
  let wrapper: VueWrapper | undefined

  beforeEach(() => {
    setActivePinia(createPinia())
    setLocale('en')
    useChatStore().categories = [categoryFixture({
      id: fixtureId(2),
      channels: [
        channelFixture({ id: talkId, type: 'voice', name: 'Lounge' }),
        channelFixture({ id: fixtureId(11), type: 'text', name: 'general' })
      ]
    })]
    Object.defineProperty(URL, 'createObjectURL', { value: vi.fn(() => 'blob:preview'), configurable: true })
  })

  afterEach(() => {
    wrapper?.unmount()
    wrapper = undefined
  })

  async function setText(selector: string, value: string, index = 0) {
    await wrapper?.findAll(selector)[index]?.setValue(value)
  }

  async function submit() {
    await wrapper?.find('form').trigger('submit')
    await flushPromises()
  }

  const titleInput = () => wrapper?.find('input[type="text"]')

  it('rejects an empty title without calling the API', async () => {
    const create = vi.spyOn(useEventsStore(), 'create')
    wrapper = mount(EventEditDialog)
    await titleInput()?.setValue('   ')
    await submit()
    expect(wrapper.find('.text-red-400').text()).toContain('e.g. Game night')
    expect(create).not.toHaveBeenCalled()
  })

  it('requires a start time', async () => {
    const create = vi.spyOn(useEventsStore(), 'create')
    wrapper = mount(EventEditDialog)
    await titleInput()?.setValue('Party')
    await submit()
    expect(wrapper.find('.text-red-400').text()).toBe('Start time')
    expect(create).not.toHaveBeenCalled()
  })

  it('rejects an end before the start', async () => {
    const create = vi.spyOn(useEventsStore(), 'create')
    wrapper = mount(EventEditDialog)
    await titleInput()?.setValue('Party')
    await setText('input[type="datetime-local"]', '2026-11-01T18:00', 0)
    await setText('input[type="datetime-local"]', '2026-11-01T17:00', 1)
    await submit()
    expect(wrapper.find('.text-red-400').text()).toContain('End time')
    expect(create).not.toHaveBeenCalled()
  })

  it('requires a Talk when the location is a Talk, and a text otherwise', async () => {
    const create = vi.spyOn(useEventsStore(), 'create')
    wrapper = mount(EventEditDialog)
    await titleInput()?.setValue('Party')
    await setText('input[type="datetime-local"]', '2026-11-01T18:00')
    await submit()
    expect(wrapper.find('.text-red-400').text()).toContain('e.g. Room 42')

    await wrapper.find('input[type="radio"][value="talk"]').setValue(true)
    await submit()
    expect(wrapper.find('.text-red-400').text()).toBe('Select a Talk channel')
    expect(create).not.toHaveBeenCalled()
  })

  it('lists only Talk channels to choose from', async () => {
    wrapper = mount(EventEditDialog)
    await wrapper.find('input[type="radio"][value="talk"]').setValue(true)
    const options = wrapper.findAll('select option').map(o => o.text())
    expect(options).toContain('Lounge')
    expect(options).not.toContain('general')
  })

  it('creates an event with a text location', async () => {
    const created = makeEvent({ title: 'Party' })
    const create = vi.spyOn(useEventsStore(), 'create').mockResolvedValue(created)
    wrapper = mount(EventEditDialog)
    await titleInput()?.setValue('  Party ')
    await setText('input[type="datetime-local"]', '2026-11-01T18:00')
    await setText('input[type="datetime-local"]', '2026-11-01T20:00', 1)
    await wrapper.find('input[maxlength="255"]').setValue(' Room 42 ')
    await submit()
    expect(create).toHaveBeenCalledWith(expect.objectContaining({
      title: 'Party',
      location: 'Room 42',
      channel_id: null,
      start_time: new Date('2026-11-01T18:00').toISOString(),
      end_time: new Date('2026-11-01T20:00').toISOString()
    }))
    expect(wrapper.emitted('saved')?.[0]).toEqual([created])
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(useToastStore().toasts.some(t => t.text === 'Event created')).toBe(true)
  })

  it('creates an event in a Talk and uploads the chosen cover', async () => {
    const created = makeEvent({ id: fixtureId(7) })
    const store = useEventsStore()
    const create = vi.spyOn(store, 'create').mockResolvedValue(created)
    const upload = vi.spyOn(store, 'uploadCover').mockResolvedValue(created)
    wrapper = mount(EventEditDialog)
    await titleInput()?.setValue('Party')
    await setText('input[type="datetime-local"]', '2026-11-01T18:00')
    await wrapper.find('input[type="radio"][value="talk"]').setValue(true)
    await wrapper.find('select').setValue(talkId)

    const file = new File(['x'], 'cover.png', { type: 'image/png' })
    const input = wrapper.find('input[type="file"]')
    Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
    await input.trigger('change')
    expect(wrapper.find('img').attributes('src')).toBe('blob:preview')
    expect(wrapper.text()).toContain('Change cover')

    await submit()
    expect(create).toHaveBeenCalledWith(expect.objectContaining({ channel_id: talkId, location: null, end_time: null }))
    expect(upload).toHaveBeenCalledWith(fixtureId(7), file)
  })

  it('ignores a file dialog that picked nothing', async () => {
    wrapper = mount(EventEditDialog)
    const input = wrapper.find('input[type="file"]')
    Object.defineProperty(input.element, 'files', { value: [], configurable: true })
    await input.trigger('change')
    expect(wrapper.find('img').exists()).toBe(false)
  })

  it('prefills an existing event and saves changes', async () => {
    const existing = makeEvent({ location: 'Room 1', end_time: '2026-11-01T20:00:00Z' })
    const store = useEventsStore()
    const update = vi.spyOn(store, 'update').mockResolvedValue(existing)
    wrapper = mount(EventEditDialog, { props: { event: existing } })
    expect((titleInput()?.element as HTMLInputElement).value).toBe('Game Night')
    expect((wrapper.find('textarea').element as HTMLTextAreaElement).value).toBe('Playing games')
    await submit()
    expect(update).toHaveBeenCalledWith(fixtureId(1), expect.objectContaining({ title: 'Game Night', location: 'Room 1' }))
    expect(useToastStore().toasts.some(t => t.text === 'Event updated')).toBe(true)
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('prefills the Talk of an existing event', () => {
    wrapper = mount(EventEditDialog, { props: { event: makeEvent({ channel_id: talkId }) } })
    expect((wrapper.find('select').element as HTMLSelectElement).value).toBe(talkId)
  })

  it('removes the existing cover when asked to', async () => {
    const existing = makeEvent({ cover_url: '/cover.png', location: 'Room 1' })
    const store = useEventsStore()
    vi.spyOn(store, 'update').mockResolvedValue(existing)
    const del = vi.spyOn(store, 'deleteCover').mockResolvedValue(makeEvent())
    wrapper = mount(EventEditDialog, { props: { event: existing } })
    expect(wrapper.find('img').attributes('src')).toBe('/cover.png')
    await wrapper.findAll('button').find(b => b.text() === 'Remove cover')?.trigger('click')
    expect(wrapper.find('img').exists()).toBe(false)
    await submit()
    expect(del).toHaveBeenCalledWith(fixtureId(1))
  })

  it('drops a freshly chosen cover without touching the server', async () => {
    const created = makeEvent()
    const store = useEventsStore()
    vi.spyOn(store, 'create').mockResolvedValue(created)
    const upload = vi.spyOn(store, 'uploadCover')
    const del = vi.spyOn(store, 'deleteCover')
    wrapper = mount(EventEditDialog)
    const input = wrapper.find('input[type="file"]')
    Object.defineProperty(input.element, 'files', { value: [new File(['x'], 'c.png')], configurable: true })
    await input.trigger('change')
    await wrapper.find('button.absolute').trigger('click')
    await titleInput()?.setValue('Party')
    await setText('input[type="datetime-local"]', '2026-11-01T18:00')
    await wrapper.find('input[maxlength="255"]').setValue('Here')
    await submit()
    expect(upload).not.toHaveBeenCalled()
    expect(del).not.toHaveBeenCalled()
  })

  it('shows the error and stays open when saving fails', async () => {
    vi.spyOn(useEventsStore(), 'create').mockRejectedValue(new Error('Server said no'))
    wrapper = mount(EventEditDialog)
    await titleInput()?.setValue('Party')
    await setText('input[type="datetime-local"]', '2026-11-01T18:00')
    await wrapper.find('input[maxlength="255"]').setValue('Here')
    await submit()
    expect(wrapper.text()).toContain('Server said no')
    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('closes on cancel', async () => {
    wrapper = mount(EventEditDialog)
    await wrapper.findAll('button').find(b => b.text() === 'Cancel')?.trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})
