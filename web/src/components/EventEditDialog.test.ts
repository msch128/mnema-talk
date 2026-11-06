import type { VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import EventEditDialog from './EventEditDialog.vue'
import { eventWithRoles, roleFixture } from '../eventRoles.fixture'
import { useEventsStore } from '../stores/events'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { setLocale } from '../i18n'
import { categoryFixture, channelFixture, fixtureId, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'
import type { CommunityEvent } from '../types/domain'
import { pendingConfirm } from '../lib/confirm'

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
    pendingConfirm.value = null
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
  const saveButton = () => wrapper!.get('[data-testid="unsaved-save"]')
  const barError = () => wrapper!.get('[data-testid="unsaved-error"]').text()

  it('rejects an empty title without calling the API', async () => {
    const create = vi.spyOn(useEventsStore(), 'create')
    wrapper = mount(EventEditDialog)
    await titleInput()?.setValue('   ')
    await submit()
    expect(wrapper.find('.text-red-400').text()).toBe('Enter a title.')
    expect(wrapper.find('[role="alert"]').text()).toBe('Enter a title.')
    expect(create).not.toHaveBeenCalled()
  })

  it('requires a start time', async () => {
    const create = vi.spyOn(useEventsStore(), 'create')
    wrapper = mount(EventEditDialog)
    await titleInput()?.setValue('Party')
    await submit()
    expect(wrapper.find('.text-red-400').text()).toBe('Choose a start time.')
    expect(create).not.toHaveBeenCalled()
  })

  it('rejects an end before the start', async () => {
    const create = vi.spyOn(useEventsStore(), 'create')
    wrapper = mount(EventEditDialog)
    await titleInput()?.setValue('Party')
    await setText('input[type="datetime-local"]', '2026-11-01T18:00', 0)
    await setText('input[type="datetime-local"]', '2026-11-01T17:00', 1)
    await submit()
    expect(wrapper.find('.text-red-400').text()).toBe('The end must not be before the start.')
    expect(create).not.toHaveBeenCalled()
  })

  it('requires a Talk when the location is a Talk, and a text otherwise', async () => {
    const create = vi.spyOn(useEventsStore(), 'create')
    wrapper = mount(EventEditDialog)
    await titleInput()?.setValue('Party')
    await setText('input[type="datetime-local"]', '2026-11-01T18:00')
    await submit()
    expect(wrapper.find('.text-red-400').text()).toBe('Enter a location.')

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
    await wrapper.find('input[data-max-chars="255"]').setValue(' Room 42 ')
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

  it('prefills an existing event and saves only what changed', async () => {
    const existing = makeEvent({ location: 'Room 1', end_time: '2026-11-01T20:00:00Z' })
    const store = useEventsStore()
    const update = vi.spyOn(store, 'update').mockResolvedValue(existing)
    wrapper = mount(EventEditDialog, { props: { event: existing } })
    expect((titleInput()?.element as HTMLInputElement).value).toBe('Game Night')
    expect((wrapper.find('textarea').element as HTMLTextAreaElement).value).toBe('Playing games')
    await titleInput()?.setValue('Game Night II')
    await submit()
    expect(update).toHaveBeenCalledWith(fixtureId(1), { title: 'Game Night II' })
    expect(useToastStore().toasts.some(t => t.text === 'Event updated')).toBe(true)
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('names the place together when either part of it changed, and sends nothing for an untouched form', async () => {
    const existing = makeEvent({ location: 'Room 1' })
    const store = useEventsStore()
    const update = vi.spyOn(store, 'update').mockResolvedValue(existing)
    wrapper = mount(EventEditDialog, { props: { event: existing } })
    expect(saveButton().attributes('disabled')).toBeDefined()
    await submit()
    expect(update).not.toHaveBeenCalled()
    await wrapper.find('input[data-max-chars="255"]').setValue('Room 2')
    await submit()
    expect(update).toHaveBeenLastCalledWith(fixtureId(1), { channel_id: null, location: 'Room 2' })
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
    await wrapper.find('input[data-max-chars="255"]').setValue('Here')
    await submit()
    expect(upload).not.toHaveBeenCalled()
    expect(del).not.toHaveBeenCalled()
  })

  it('shows the error and stays open when saving fails', async () => {
    vi.spyOn(useEventsStore(), 'create').mockRejectedValue(new Error('Server said no'))
    wrapper = mount(EventEditDialog)
    await titleInput()?.setValue('Party')
    await setText('input[type="datetime-local"]', '2026-11-01T18:00')
    await wrapper.find('input[data-max-chars="255"]').setValue('Here')
    await submit()
    expect(wrapper.text()).toContain('Server said no')
    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('closes on cancel', async () => {
    wrapper = mount(EventEditDialog)
    await wrapper.findAll('button').find(b => b.text() === 'Cancel')?.trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
  it('does not create a second event when the cover upload failed after the create', async () => {
    const created = makeEvent({ id: fixtureId(7), title: 'Party' })
    const store = useEventsStore()
    const create = vi.spyOn(store, 'create').mockResolvedValue(created)
    const update = vi.spyOn(store, 'update').mockResolvedValue(created)
    const upload = vi.spyOn(store, 'uploadCover').mockRejectedValueOnce(new Error('too large')).mockResolvedValueOnce(created)
    wrapper = mount(EventEditDialog)
    await titleInput()?.setValue('Party')
    await setText('input[type="datetime-local"]', '2026-11-01T18:00')
    await wrapper.find('input[data-max-chars="255"]').setValue('Room 42')
    const input = wrapper.find('input[type="file"]')
    Object.defineProperty(input.element, 'files', { value: [new File(['x'], 'cover.png', { type: 'image/png' })], configurable: true })
    await input.trigger('change')
    await submit()
    expect(barError()).toContain('too large')
    expect(barError()).toContain('Some changes were saved')
    expect(wrapper.emitted('close')).toBeUndefined()

    await submit()
    expect(create).toHaveBeenCalledTimes(1)
    expect(update).not.toHaveBeenCalled()
    expect(upload).toHaveBeenCalledTimes(2)
    expect(wrapper.emitted('saved')).toHaveLength(1)
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('names its fields for assistive technology, labels the remove button and frees replaced previews', async () => {
    const revoke = vi.fn()
    Object.defineProperty(URL, 'revokeObjectURL', { value: revoke, configurable: true })
    wrapper = mount(EventEditDialog)
    for (const input of wrapper.findAll('input[type="text"], textarea, input[type="datetime-local"]')) {
      const id = input.attributes('id')
      if (input.attributes('data-max-chars') === '255') continue
      expect(id).toBeTruthy()
      expect(wrapper.find(`label[for="${id}"]`).exists()).toBe(true)
    }
    const pick = async () => {
      const input = wrapper!.find('input[type="file"]')
      Object.defineProperty(input.element, 'files', { value: [new File(['x'], 'c.png', { type: 'image/png' })], configurable: true })
      await input.trigger('change')
    }
    await pick()
    expect(wrapper.find('button[aria-label="Remove cover"]').exists()).toBe(true)
    expect(revoke).not.toHaveBeenCalled()
    await pick()
    expect(revoke).toHaveBeenCalledTimes(1)
    wrapper.unmount()
    wrapper = undefined
    expect(revoke).toHaveBeenCalledTimes(2)
  })

  describe('one save model', () => {
    const cancelButton = () => wrapper!.findAll('button').find(b => b.text() === 'Cancel')!
    async function answer(yes: boolean) {
      await flushPromises()
      pendingConfirm.value?.resolve(yes)
      await flushPromises()
    }

    it('keeps the create button labelled Create and closes at once while nothing is typed', async () => {
      wrapper = mount(EventEditDialog)
      expect(wrapper.find('button[type="submit"]').text()).toBe('Create event')
      await cancelButton().trigger('click')
      await flushPromises()
      expect(pendingConfirm.value).toBeNull()
      expect(wrapper.emitted('close')).toHaveLength(1)
    })

    it('asks before closing a create dialog with typed input', async () => {
      wrapper = mount(EventEditDialog, { attachTo: document.body })
      await titleInput()?.setValue('Party')
      await cancelButton().trigger('click')
      await flushPromises()
      expect(pendingConfirm.value?.title).toBe('Discard changes?')
      await answer(false)
      expect(wrapper.emitted('close')).toBeUndefined()
      await cancelButton().trigger('click')
      await answer(true)
      expect(wrapper.emitted('close')).toHaveLength(1)
    })

    it('edits with a Save that is off until something changed, and Reset brings the old values back', async () => {
      wrapper = mount(EventEditDialog, { props: { event: makeEvent({ location: 'Hall' }) } })
      expect(saveButton().attributes('disabled')).toBeDefined()
      await titleInput()?.setValue('  Game Night ')
      expect(saveButton().attributes('disabled')).toBeDefined()
      await titleInput()?.setValue('Game Night II')
      expect(saveButton().attributes('disabled')).toBeUndefined()
      await wrapper.get('[data-testid="unsaved-reset"]').trigger('click')
      expect((titleInput()?.element as HTMLInputElement).value).toBe('Game Night')
      expect(saveButton().attributes('disabled')).toBeDefined()
    })

    it('asks before an edit dialog with unsaved changes is closed', async () => {
      wrapper = mount(EventEditDialog, { props: { event: makeEvent({ location: 'Hall' }) }, attachTo: document.body })
      await titleInput()?.setValue('Other')
      await wrapper.get('[data-dialog-close]').trigger('click')
      await answer(false)
      expect(wrapper.emitted('close')).toBeUndefined()
      await wrapper.get('[data-dialog-close]').trigger('click')
      await answer(true)
      expect(wrapper.emitted('close')).toHaveLength(1)
    })

    it('counts the cover and the roles as unsaved changes', async () => {
      useServerFeaturesStore().list = ['events', 'events.roles']
      const existing = eventWithRoles({ roles: [roleFixture(1)], location: 'Hall', cover_url: '/c.png' })
      wrapper = mount(EventEditDialog, { props: { event: existing } })
      await flushPromises()
      expect(saveButton().attributes('disabled')).toBeDefined()
      await wrapper.find('button[aria-label="Remove cover"]').trigger('click')
      expect(saveButton().attributes('disabled')).toBeUndefined()
      await wrapper.get('[data-testid="unsaved-reset"]').trigger('click')
      expect(wrapper.find('img').exists()).toBe(true)
      expect(saveButton().attributes('disabled')).toBeDefined()
      await wrapper.get('[data-testid="event-role-name"]').setValue('Renamed')
      expect(saveButton().attributes('disabled')).toBeUndefined()
      await wrapper.get('[data-testid="event-role-name"]').setValue(roleFixture(1).name)
      expect(saveButton().attributes('disabled')).toBeDefined()
    })

    it('writes event, cover and roles by separate calls and keeps only the failed part unsaved', async () => {
      useServerFeaturesStore().list = ['events', 'events.roles']
      const existing = eventWithRoles({ roles: [roleFixture(1)], location: 'Hall', cover_url: '/c.png' })
      const store = useEventsStore()
      const update = vi.spyOn(store, 'update').mockResolvedValue(existing)
      const del = vi.spyOn(store, 'deleteCover').mockRejectedValue(new Error('storage offline'))
      const setRoles = vi.spyOn(store, 'setRoles').mockResolvedValue(existing)
      wrapper = mount(EventEditDialog, { props: { event: existing } })
      await titleInput()?.setValue('Renamed')
      await wrapper.find('button[aria-label="Remove cover"]').trigger('click')
      await wrapper.get('[data-testid="event-role-name"]').setValue('Other')
      await submit()
      expect(barError()).toContain('Some changes were saved, the rest was not')
      expect(barError()).toContain('storage offline')
      expect(wrapper.emitted('close')).toBeUndefined()
      expect(setRoles).not.toHaveBeenCalled()

      del.mockResolvedValue(existing)
      await submit()
      expect(update).toHaveBeenCalledTimes(1)
      expect(del).toHaveBeenCalledTimes(2)
      expect(setRoles).toHaveBeenCalledTimes(1)
      expect(useToastStore().toasts.some(t => t.text === 'Event updated')).toBe(true)
      expect(wrapper.emitted('close')).toHaveLength(1)
    })
  })

  describe('roles', () => {
    beforeEach(() => {
      useServerFeaturesStore().list = ['events', 'events.roles']
    })

    async function fillBasics() {
      await titleInput()?.setValue('Raid')
      await setText('input[type="datetime-local"]', '2026-11-01T18:00')
      await wrapper!.find('input[data-max-chars="255"]').setValue('Hall')
    }

    it('shows the role editor only when the server has roles', async () => {
      wrapper = mount(EventEditDialog)
      expect(wrapper.find('[data-testid="event-roles-editor"]').exists()).toBe(true)
      wrapper.unmount()
      useServerFeaturesStore().list = ['events']
      wrapper = mount(EventEditDialog)
      expect(wrapper.find('[data-testid="event-roles-editor"]').exists()).toBe(false)
    })

    it('sets the roles after creating the event', async () => {
      const store = useEventsStore()
      const created = makeEvent({ id: fixtureId(8) })
      vi.spyOn(store, 'create').mockResolvedValue(created)
      const setRoles = vi.spyOn(store, 'setRoles').mockResolvedValue(created)
      wrapper = mount(EventEditDialog)
      await fillBasics()
      await wrapper.get('[data-testid="event-role-add"]').trigger('click')
      await wrapper.get('[data-testid="event-role-name"]').setValue('Tank')
      await submit()
      expect(setRoles).toHaveBeenCalledWith(fixtureId(8), [{ name: 'Tank' }])
      expect(wrapper.emitted('saved')).toHaveLength(1)
    })

    it('sends no role list for an event that never had roles', async () => {
      const store = useEventsStore()
      vi.spyOn(store, 'create').mockResolvedValue(makeEvent())
      const setRoles = vi.spyOn(store, 'setRoles')
      wrapper = mount(EventEditDialog)
      await fillBasics()
      await submit()
      expect(setRoles).not.toHaveBeenCalled()
    })

    it('sends the roles of an existing event only when the editor changed them', async () => {
      const store = useEventsStore()
      const existing = eventWithRoles({ roles: [roleFixture(1)], location: 'Hall' })
      vi.spyOn(store, 'update').mockResolvedValue(existing)
      vi.spyOn(store, 'fetchDetail').mockResolvedValue(existing)
      const setRoles = vi.spyOn(store, 'setRoles').mockResolvedValue(existing)
      wrapper = mount(EventEditDialog, { props: { event: existing } })
      await titleInput()?.setValue('Raid night, new title')
      await submit()
      expect(store.update).toHaveBeenCalledTimes(1)
      expect(setRoles).not.toHaveBeenCalled()

      wrapper.unmount()
      wrapper = mount(EventEditDialog, { props: { event: existing } })
      await wrapper.get('[data-testid="event-role-name"]').setValue('Renamed')
      await submit()
      expect(setRoles).toHaveBeenCalledTimes(1)
    })

    it('keeps the dialog open with the reason when the roles are refused, and retries them on the next submit', async () => {
      const store = useEventsStore()
      const created = makeEvent({ id: fixtureId(8) })
      vi.spyOn(store, 'create').mockResolvedValue(created)
      vi.spyOn(store, 'update').mockResolvedValue(created)
      const setRoles = vi.spyOn(store, 'setRoles').mockRejectedValueOnce(new Error('two roles are called Tank')).mockResolvedValue(created)
      vi.spyOn(store, 'fetchDetail').mockResolvedValue(created)
      wrapper = mount(EventEditDialog)
      await fillBasics()
      await wrapper.get('[data-testid="event-role-add"]').trigger('click')
      await wrapper.get('[data-testid="event-role-name"]').setValue('Tank')
      await submit()
      expect(barError()).toContain('two roles are called Tank')
      expect(wrapper.emitted('close')).toBeUndefined()
      await submit()
      expect(setRoles).toHaveBeenCalledTimes(2)
      expect(wrapper.emitted('close')).toHaveLength(1)
    })
  })

  describe('repeating events', () => {
    async function fillBasics() {
      await titleInput()?.setValue('Raid')
      await setText('input[type="datetime-local"]', '2026-11-01T18:00')
      await wrapper!.find('input[data-max-chars="255"]').setValue('Hall')
    }

    it('offers the repeat choice only for a new event on a server that knows recurrence', async () => {
      useServerFeaturesStore().list = ['events']
      wrapper = mount(EventEditDialog)
      expect(wrapper.find('[data-testid="event-repeat"]').exists()).toBe(false)
      wrapper.unmount()
      useServerFeaturesStore().list = ['events', 'events.recurrence']
      wrapper = mount(EventEditDialog)
      expect(wrapper.find('[data-testid="event-repeat"]').exists()).toBe(true)
      wrapper.unmount()
      wrapper = mount(EventEditDialog, { props: { event: makeEvent({ location: 'Hall' }) } })
      expect(wrapper.find('[data-testid="event-repeat"]').exists()).toBe(false)
    })

    it('creates the event as the first of a series, in the browser zone, ending on the chosen day', async () => {
      useServerFeaturesStore().list = ['events', 'events.recurrence']
      const store = useEventsStore()
      const create = vi.spyOn(store, 'create').mockResolvedValue(makeEvent({ id: fixtureId(8) }))
      wrapper = mount(EventEditDialog)
      await fillBasics()
      await wrapper.get('[data-testid="event-repeat-select"]').setValue('weekly')
      await wrapper.get('[data-testid="event-repeat-until"]').setValue('2027-01-31')
      await submit()
      const payload = create.mock.calls[0]![0] as { recurrence?: { frequency: string; timezone?: string; until?: string } }
      expect(payload.recurrence?.frequency).toBe('weekly')
      expect(payload.recurrence?.timezone).toBeTruthy()
      expect(new Date(payload.recurrence!.until!).getDate()).toBe(31)
    })

    it('gives the roles chosen while creating a repeating event to the whole series', async () => {
      useServerFeaturesStore().list = ['events', 'events.recurrence', 'events.roles']
      const store = useEventsStore()
      const series = { id: 's', frequency: 'weekly' as const, timezone: 'UTC', index: 0, ended: false }
      const created = makeEvent({ id: fixtureId(8), series })
      vi.spyOn(store, 'create').mockResolvedValue(created)
      const setRoles = vi.spyOn(store, 'setRoles').mockResolvedValue(created)
      wrapper = mount(EventEditDialog)
      await fillBasics()
      await wrapper.get('[data-testid="event-repeat-select"]').setValue('weekly')
      await wrapper.get('[data-testid="event-role-add"]').trigger('click')
      await wrapper.get('[data-testid="event-role-name"]').setValue('Tank')
      await submit()
      expect(setRoles).toHaveBeenCalledWith(fixtureId(8), [{ name: 'Tank' }], 'following')
    })

    it('sends no recurrence for an event that does not repeat', async () => {
      useServerFeaturesStore().list = ['events', 'events.recurrence']
      const store = useEventsStore()
      const create = vi.spyOn(store, 'create').mockResolvedValue(makeEvent({ id: fixtureId(8) }))
      wrapper = mount(EventEditDialog)
      await fillBasics()
      await submit()
      expect(create.mock.calls[0]![0]).not.toHaveProperty('recurrence')
    })

    it('asks whether an edit of an occurrence applies to it or to the following ones', async () => {
      const series = { id: 's', frequency: 'weekly' as const, timezone: 'UTC', index: 1, ended: false }
      const existing = makeEvent({ location: 'Hall', series })
      const store = useEventsStore()
      const update = vi.spyOn(store, 'update').mockResolvedValue(existing)
      wrapper = mount(EventEditDialog, { props: { event: existing } })
      expect(wrapper.find('[data-testid="scope-choice"]').exists()).toBe(true)
      await submit()
      expect(update).not.toHaveBeenCalled()
      await titleInput()?.setValue('Renamed')
      await submit()
      expect(update).toHaveBeenLastCalledWith(fixtureId(1), expect.anything(), 'this')
      await titleInput()?.setValue('Renamed again')
      await wrapper.get('[data-testid="scope-following"]').setValue(true)
      await submit()
      expect(update).toHaveBeenLastCalledWith(fixtureId(1), expect.anything(), 'following')
    })

    it('does not ask for a single event', () => {
      wrapper = mount(EventEditDialog, { props: { event: makeEvent({ location: 'Hall' }) } })
      expect(wrapper.find('[data-testid="scope-choice"]').exists()).toBe(false)
    })

    it('sends the chosen scope with the role list of an occurrence', async () => {
      useServerFeaturesStore().list = ['events', 'events.roles']
      const series = { id: 's', frequency: 'monthly' as const, timezone: 'UTC', index: 0, ended: false }
      const existing = eventWithRoles({ location: 'Hall', series, roles: [roleFixture(1)], start_time: '2026-11-01T18:00:00Z' })
      const store = useEventsStore()
      vi.spyOn(store, 'update').mockResolvedValue(existing)
      vi.spyOn(store, 'fetchDetail').mockResolvedValue(existing)
      const setRoles = vi.spyOn(store, 'setRoles').mockResolvedValue(existing)
      wrapper = mount(EventEditDialog, { props: { event: existing } })
      await wrapper.get('[data-testid="event-role-name"]').setValue('Renamed')
      await wrapper.get('[data-testid="scope-following"]').setValue(true)
      await submit()
      expect(setRoles).toHaveBeenCalledWith(fixtureId(1), expect.anything(), 'following')
    })
  })
})
