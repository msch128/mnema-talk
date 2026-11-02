import type { VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { ref } from 'vue'
import EventRolesEditor from './EventRolesEditor.vue'
import { usePermissionsStore } from '../stores/permissions'
import { setLocale } from '../i18n'
import * as api from '../lib/eventRolesApi'
import { blankDraft, draftsOf, MAX_EVENT_ROLES } from '../lib/eventRoleDrafts'
import type { RoleDraft, RoleRemoval } from '../lib/eventRoleDrafts'
import { roleFixture } from '../eventRoles.fixture'

vi.mock('../lib/eventRolesApi', () => ({
  fetchRoleTemplates: vi.fn(), createRoleTemplate: vi.fn(), setRoleTemplateEnabled: vi.fn(), deleteRoleTemplate: vi.fn()
}))

let wrapper: VueWrapper
const rows = ref<RoleDraft[]>([])
const removals = ref<RoleRemoval[]>([])

const templates = [
  { id: 't1', name: 'Party of five', roles: [{ name: 'Tank', capacity: 1 }, { name: 'Heal', capacity: 1 }], enabled: true, preset: true },
  { id: 't2', name: 'Hidden', roles: [{ name: 'Secret' }], enabled: false, preset: true }
]

async function mountEditor(admin = false) {
  usePermissionsStore().info = { permissions: admin ? 1 : 0, catalog: [], channels: [], role_ids: [], max_upload_mb: 50, owner: false, top_position: 0 }
  wrapper = mount(EventRolesEditor, {
    props: {
      modelValue: rows.value, 'onUpdate:modelValue': (v: RoleDraft[]) => { rows.value = v; void wrapper.setProps({ modelValue: v }) },
      removals: removals.value, 'onUpdate:removals': (v: RoleRemoval[]) => { removals.value = v; void wrapper.setProps({ removals: v }) }
    }
  })
  await flushPromises()
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  rows.value = []
  removals.value = []
  vi.clearAllMocks()
  vi.mocked(api.fetchRoleTemplates).mockResolvedValue(templates)
})
afterEach(() => wrapper?.unmount())

describe('EventRolesEditor', () => {
  it('adds, orders and removes rows, and switches a capacity off', async () => {
    await mountEditor()
    await wrapper.get('[data-testid="event-role-add"]').trigger('click')
    await wrapper.get('[data-testid="event-role-add"]').trigger('click')
    expect(rows.value).toHaveLength(2)
    await wrapper.findAll('[data-testid="event-role-name"]')[0]!.setValue('Tank')
    await wrapper.findAll('[data-testid="event-role-name"]')[1]!.setValue('Heal')
    await wrapper.findAll('button[aria-label="Move down"]')[0]!.trigger('click')
    expect(rows.value.map(r => r.name)).toEqual(['Heal', 'Tank'])
    await wrapper.findAll('button[aria-label="Move up"]')[1]!.trigger('click')
    expect(rows.value.map(r => r.name)).toEqual(['Tank', 'Heal'])
    await wrapper.findAll('button[aria-label="Move up"]')[0]!.trigger('click')
    expect(rows.value.map(r => r.name)).toEqual(['Tank', 'Heal'])

    const unlimited = wrapper.findAll('input[type="checkbox"]')[0]!
    await unlimited.setValue(false)
    expect(rows.value[0]!.capacity).toBe(1)
    await wrapper.get('[data-testid="event-role-capacity"]').setValue(4)
    expect(rows.value[0]!.capacity).toBe(4)
    await unlimited.setValue(true)
    expect(rows.value[0]!.capacity).toBeNull()

    await wrapper.findAll('button[aria-label^="Remove role"]')[1]!.trigger('click')
    expect(rows.value.map(r => r.name)).toEqual(['Tank'])
  })

  it('stops adding at the limit', async () => {
    rows.value = Array.from({ length: MAX_EVENT_ROLES }, () => blankDraft())
    await mountEditor()
    expect(wrapper.get('[data-testid="event-role-add"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="event-role-add"]').trigger('click')
    expect(rows.value).toHaveLength(MAX_EVENT_ROLES)
  })

  it('asks where the members of a role go before it is removed, and records the removal', async () => {
    rows.value = draftsOf([roleFixture(1, { name: 'Tank', confirmed: 2 }), roleFixture(2, { name: 'DPS' })])
    await mountEditor()
    await wrapper.findAll('button[aria-label^="Remove role"]')[0]!.trigger('click')
    const ask = wrapper.get('[data-testid="event-role-move"]')
    expect(ask.text()).toContain('2 members are signed up')
    expect(ask.findAll('option').map(o => o.text())).toEqual(['Another role', 'DPS'])
    expect(ask.findAll('button')[0]!.attributes('disabled')).toBeDefined()
    await ask.get('select').setValue(roleFixture(2).id)
    await ask.findAll('button')[0]!.trigger('click')
    expect(rows.value.map(r => r.name)).toEqual(['DPS'])
    expect(removals.value).toEqual([{ id: roleFixture(1).id, name: 'Tank', moveTo: roleFixture(2).id }])
  })

  it('can back out of the question', async () => {
    rows.value = draftsOf([roleFixture(1, { confirmed: 1 }), roleFixture(2)])
    await mountEditor()
    await wrapper.findAll('button[aria-label^="Remove role"]')[0]!.trigger('click')
    await wrapper.get('[data-testid="event-role-move"]').findAll('button')[1]!.trigger('click')
    expect(wrapper.find('[data-testid="event-role-move"]').exists()).toBe(false)
    expect(rows.value).toHaveLength(2)
  })

  it('fills the list from an enabled template and never offers a disabled one', async () => {
    await mountEditor()
    const select = wrapper.get<HTMLSelectElement>('[data-testid="event-role-template"]')
    expect(select.findAll('option').map(o => o.text())).toEqual(['Use a template…', 'Party of five'])
    await select.setValue('t1')
    expect(rows.value.map(r => r.name)).toEqual(['Tank', 'Heal'])
  })

  it('shows no template picker when there are none, and survives a failing template list', async () => {
    vi.mocked(api.fetchRoleTemplates).mockRejectedValue(new Error('down'))
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined)
    await mountEditor()
    expect(wrapper.find('[data-testid="event-role-template"]').exists()).toBe(false)
    expect(warn).toHaveBeenCalled()
    warn.mockRestore()
  })

  it('shows the template administration to administrators only', async () => {
    await mountEditor(false)
    expect(wrapper.find('[data-testid="event-role-templates"]').exists()).toBe(false)
    wrapper.unmount()
    await mountEditor(true)
    expect(wrapper.find('[data-testid="event-role-templates"]').exists()).toBe(true)
  })
})
