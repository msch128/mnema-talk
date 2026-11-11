import type { VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import EventRolesDialog from './EventRolesDialog.vue'
import { useEventsStore } from '../../stores/events'
import { setLocale } from '../../i18n'
import * as api from '../lib/eventRolesApi'
import { eventWithRoles, roleFixture } from '../../eventRoles.fixture'
import { useToastStore } from '../../stores/toast'
import { pendingConfirm } from '../../shared/lib/confirm'

vi.mock('../lib/eventRolesApi', () => ({ fetchRoleTemplates: vi.fn(() => Promise.resolve([])) }))

let wrapper: VueWrapper

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  vi.mocked(api.fetchRoleTemplates).mockResolvedValue([])
})
afterEach(() => {
  pendingConfirm.value = null
  wrapper?.unmount()
  document.body.innerHTML = ''
})

function mountDialog(event = eventWithRoles({ roles: [roleFixture(1, { name: 'Tank', capacity: 2 }), roleFixture(2, { name: 'DPS' })] })) {
  wrapper = mount(EventRolesDialog, { props: { event }, attachTo: document.body })
  return event
}

describe('EventRolesDialog', () => {
  it('saves the edited roles in order and closes', async () => {
    const store = useEventsStore()
    const setRoles = vi.spyOn(store, 'setRoles').mockResolvedValue(eventWithRoles())
    const event = mountDialog()
    await flushPromises()
    await wrapper.findAll('[data-testid="event-role-name"]')[1]!.setValue('Damage')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(setRoles).toHaveBeenCalledWith(event.id, [
      { id: roleFixture(1).id, name: 'Tank', capacity: 2 },
      { id: roleFixture(2).id, name: 'Damage' }
    ])
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('asks what the roles apply to for an occurrence of a series and sends the choice', async () => {
    const store = useEventsStore()
    const setRoles = vi.spyOn(store, 'setRoles').mockResolvedValue(eventWithRoles())
    const series = { id: 's', frequency: 'weekly' as const, timezone: 'UTC', index: 1, ended: false }
    const event = mountDialog(eventWithRoles({ series, roles: [roleFixture(1, { name: 'Tank' })] }))
    await flushPromises()
    expect(wrapper.find('[data-testid="scope-choice"]').exists()).toBe(true)
    await wrapper.get('[data-testid="scope-following"]').setValue(true)
    await wrapper.get('[data-testid="event-role-name"]').setValue('Heal')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(setRoles).toHaveBeenCalledWith(event.id, [{ id: roleFixture(1).id, name: 'Heal' }], 'following')
  })

  it('does not ask for a single event', async () => {
    mountDialog()
    await flushPromises()
    expect(wrapper.find('[data-testid="scope-choice"]').exists()).toBe(false)
  })

  it('hands the members of a removed role over first', async () => {
    const store = useEventsStore()
    const order: string[] = []
    vi.spyOn(store, 'deleteRole').mockImplementation(async (_id, roleId, moveTo) => { order.push(`delete ${roleId} -> ${moveTo}`); return eventWithRoles() })
    vi.spyOn(store, 'setRoles').mockImplementation(async () => { order.push('set'); return eventWithRoles() })
    mountDialog(eventWithRoles({ roles: [roleFixture(1, { name: 'Tank', confirmed: 1 }), roleFixture(2, { name: 'DPS' })] }))
    await flushPromises()
    await wrapper.findAll('button[aria-label^="Remove role"]')[0]!.trigger('click')
    await wrapper.get('[data-testid="event-role-move"] select').setValue(roleFixture(2).id)
    await wrapper.get('[data-testid="event-role-move"] button').trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(order).toEqual([`delete ${roleFixture(1).id} -> ${roleFixture(2).id}`, 'set'])
  })

  it('shows what the server refuses and what is not valid yet, and stays open', async () => {
    const store = useEventsStore()
    vi.spyOn(store, 'setRoles').mockRejectedValue(new Error('the role Tank has 3 confirmed members; remove 1 first'))
    mountDialog()
    await flushPromises()
    await wrapper.findAll('[data-testid="event-role-name"]')[0]!.setValue('Tanks')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('remove 1 first')
    expect(wrapper.emitted('close')).toBeUndefined()

    await wrapper.findAll('[data-testid="event-role-name"]')[0]!.setValue('')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('own name')
  })

  it('treats an event without a role list as having none', async () => {
    const event = eventWithRoles()
    delete (event as { roles?: unknown }).roles
    mountDialog(event)
    await flushPromises()
    expect(wrapper.findAll('[data-testid="event-role-row"]')).toHaveLength(0)
    await wrapper.get('button[type="button"]').trigger('click')
  })

  describe('one save model', () => {
    const save = () => wrapper.get('[data-testid="event-roles-save"]')
    const name = (i: number) => wrapper.findAll('[data-testid="event-role-name"]')[i]!

    it('keeps Save off until the roster differs from the saved one, and a change back switches it off again', async () => {
      mountDialog()
      await flushPromises()
      expect(save().attributes('disabled')).toBeDefined()
      await name(0).setValue('Tanks')
      expect(save().attributes('disabled')).toBeUndefined()
      await name(0).setValue(' Tank ')
      expect(save().attributes('disabled')).toBeDefined()
      await wrapper.get('[data-testid="event-role-add"]').trigger('click')
      await wrapper.findAll('[data-testid="event-role-name"]')[2]!.setValue('Heal')
      expect(save().attributes('disabled')).toBeUndefined()
      await wrapper.get('[data-testid="unsaved-reset"]').trigger('click')
      expect(wrapper.findAll('[data-testid="event-role-row"]')).toHaveLength(2)
      expect(save().attributes('disabled')).toBeDefined()
    })

    it('asks before closing with an edited roster and not for a clean one', async () => {
      mountDialog()
      await flushPromises()
      await wrapper.get('[data-dialog-close]').trigger('click')
      await flushPromises()
      expect(pendingConfirm.value).toBeNull()
      expect(wrapper.emitted('close')).toHaveLength(1)

      await name(0).setValue('Tanks')
      await wrapper.get('[data-dialog-close]').trigger('click')
      await flushPromises()
      expect(pendingConfirm.value?.title).toBe('Discard changes?')
      pendingConfirm.value?.resolve(false)
      await flushPromises()
      expect(wrapper.emitted('close')).toHaveLength(1)
    })

    it('confirms a saved roster with a toast', async () => {
      vi.spyOn(useEventsStore(), 'setRoles').mockResolvedValue(eventWithRoles())
      mountDialog()
      await flushPromises()
      await name(1).setValue('Damage')
      await save().trigger('click')
      await flushPromises()
      expect(useToastStore().toasts.map(t => t.text)).toContain('Roles saved')
      expect(wrapper.emitted('close')).toHaveLength(1)
    })
  })
})
