import type { VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import EventRoster from './EventRoster.vue'
import { useEventsStore } from '../stores/events'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'
import { setLocale } from '../i18n'
import { fixtureId, userFixture } from '../test-fixtures.fixture'
import { eventWithRoles, roleFixture, signupFixture } from '../eventRoles.fixture'
import type { CommunityEvent } from '../types/domain'

let wrapper: VueWrapper

const tank = roleFixture(1, { name: 'Tank', emoji: '🛡️', capacity: 1, confirmed: 1, waitlist: 1 })
const dps = roleFixture(2, { name: 'DPS', confirmed: 3 })

function mountRoster(event: CommunityEvent, canManage = false) {
  wrapper = mount(EventRoster, { props: { event, canManage } })
  return wrapper
}

const roster = (extra: Partial<CommunityEvent> = {}) => eventWithRoles({
  roles: [tank, dps],
  signups: [signupFixture(2, tank.id), signupFixture(3, tank.id, 'waitlist'), signupFixture(4, dps.id)],
  ...extra
})

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  useAuthStore().user = userFixture({ id: fixtureId(9) })
})
afterEach(() => wrapper?.unmount())

describe('EventRoster', () => {
  it('shows each role with its fill, a progress bar only for a capacity, and the members', () => {
    mountRoster(roster())
    const cards = wrapper.findAll('[data-testid="event-role"]')
    expect(cards).toHaveLength(2)
    expect(cards[0]!.get('[data-testid="event-role-fill"]').text()).toBe('1/1')
    expect(cards[1]!.get('[data-testid="event-role-fill"]').text()).toBe('3')
    expect(cards[0]!.find('[aria-hidden="true"]').exists()).toBe(true)
    expect(cards[1]!.find('[aria-hidden="true"]').exists()).toBe(false)
    expect(cards[0]!.text()).toContain('user2')
    expect(cards[0]!.get('[data-testid="event-waitlist"]').text()).toContain('user3')
    expect(cards[0]!.text()).toContain('🛡️')
  })

  it('offers to join a free role, the waitlist of a full one, and nothing on a finished event', () => {
    mountRoster(roster())
    const label = (i: number) => wrapper.findAll('[data-testid="event-role"]')[i]!.get('[data-testid="event-role-action"]').text()
    expect(label(0)).toBe('Join waitlist')
    expect(label(1)).toBe('Join')
    wrapper.unmount()
    mountRoster(roster({ status: 'cancelled' }))
    expect(wrapper.find('[data-testid="event-role-action"]').exists()).toBe(false)
  })

  it('signs up through the store and reports a failure as a toast', async () => {
    const store = useEventsStore()
    const signUp = vi.spyOn(store, 'signUp').mockRejectedValueOnce(new Error('role is full')).mockResolvedValue(roster())
    mountRoster(roster())
    await wrapper.findAll('[data-testid="event-role-action"]')[1]!.trigger('click')
    await flushPromises()
    expect(signUp).toHaveBeenCalledWith(fixtureId(1), dps.id)
    expect(useToastStore().toasts.map(t => t.text).join()).toContain('role is full')
    await wrapper.findAll('[data-testid="event-role-action"]')[1]!.trigger('click')
    await flushPromises()
    expect(signUp).toHaveBeenCalledTimes(2)
  })

  it('lets a member who holds a role switch to another or leave, and shows the place on a waitlist', async () => {
    const store = useEventsStore()
    const leave = vi.spyOn(store, 'leaveRole').mockResolvedValue(roster())
    mountRoster(roster({ my_signup: { role_id: tank.id, state: 'waitlist', waitlist_position: 2 } }))
    expect(wrapper.get('[data-testid="event-my-waitlist"]').text()).toContain('place 2')
    const actions = wrapper.findAll('[data-testid="event-role-action"]')
    expect(actions[0]!.text()).toBe('Leave')
    expect(actions[1]!.text()).toBe('Switch to this role')
    await actions[0]!.trigger('click')
    await flushPromises()
    expect(leave).toHaveBeenCalledWith(fixtureId(1))
  })

  it('keeps leaving possible on a finished event', () => {
    mountRoster(roster({ status: 'completed', my_signup: { role_id: dps.id, state: 'confirmed', waitlist_position: 0 } }))
    const actions = wrapper.findAll('[data-testid="event-role-action"]')
    expect(actions).toHaveLength(1)
    expect(actions[0]!.text()).toBe('Leave')
  })

  it('gives managers a remove button for others, never for themselves, and the role editor', async () => {
    const store = useEventsStore()
    const remove = vi.spyOn(store, 'removeSignup').mockResolvedValue(roster())
    useAuthStore().user = userFixture({ id: fixtureId(2) })
    mountRoster(roster(), true)
    const removers = wrapper.findAll('button[aria-label^="Remove "]')
    // user2 is the caller: only the waitlisted user3 and DPS member user4 can be removed
    expect(removers.map(b => b.attributes('aria-label'))).toEqual(['Remove user3', 'Remove user4'])
    await removers[0]!.trigger('click')
    await flushPromises()
    expect(remove).toHaveBeenCalledWith(fixtureId(1), fixtureId(3))
    await wrapper.get('[data-testid="event-roles-edit"]').trigger('click')
    expect(wrapper.emitted('edit-roles')).toHaveLength(1)
  })

  it('shows no remove buttons to members and nothing at all without roles for them', () => {
    mountRoster(roster())
    expect(wrapper.find('button[aria-label^="Remove "]').exists()).toBe(false)
    wrapper.unmount()
    mountRoster(eventWithRoles())
    expect(wrapper.find('[data-testid="event-roster"]').exists()).toBe(false)
  })

  it('invites a manager to add roles to an event that has none, and counts a waitlist without names', () => {
    mountRoster(eventWithRoles(), true)
    expect(wrapper.text()).toContain('no roles yet')
    expect(wrapper.get('[data-testid="event-roles-edit"]').text()).toBe('Add roles')
    wrapper.unmount()
    mountRoster(eventWithRoles({ roles: [roleFixture(1, { capacity: 2, confirmed: 2, waitlist: 4 })] }))
    expect(wrapper.text()).toContain('Waitlist (4)')
  })

  it('treats a missing role list as empty', () => {
    const event = eventWithRoles()
    delete (event as { roles?: unknown }).roles
    mountRoster(event, true)
    expect(wrapper.text()).toContain('no roles yet')
  })
})
