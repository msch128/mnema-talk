import type { VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import EventRolesDialog from './EventRolesDialog.vue'
import { useEventsStore } from '../stores/events'
import { setLocale } from '../i18n'
import * as api from '../lib/eventRolesApi'
import { eventWithRoles, roleFixture } from '../eventRoles.fixture'

vi.mock('../lib/eventRolesApi', () => ({ fetchRoleTemplates: vi.fn(() => Promise.resolve([])) }))

let wrapper: VueWrapper

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  vi.mocked(api.fetchRoleTemplates).mockResolvedValue([])
})
afterEach(() => {
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
})
