import type { VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import EventRoleTemplates from './EventRoleTemplates.vue'
import { useToastStore } from '../stores/toast'
import { setLocale } from '../i18n'
import * as api from '../lib/eventRolesApi'
import { blankDraft } from '../lib/eventRoleDrafts'
import type { RoleDraft } from '../lib/eventRoleDrafts'

vi.mock('../lib/eventRolesApi', () => ({ createRoleTemplate: vi.fn(), setRoleTemplateEnabled: vi.fn(), deleteRoleTemplate: vi.fn() }))

let wrapper: VueWrapper
const templates = [
  { id: 'p1', name: 'Party of five', roles: [{ name: 'Tank', capacity: 1 }, { name: 'DPS' }], enabled: false, preset: true },
  { id: 'c1', name: 'Movie night', roles: [{ name: 'Popcorn', capacity: 2 }], enabled: true, preset: false }
]
const rows: RoleDraft[] = [{ ...blankDraft(), name: 'Tank', capacity: 1 }]

function mountIt(props: Partial<{ rows: typeof rows }> = {}) {
  wrapper = mount(EventRoleTemplates, { props: { templates, rows, ...props } })
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  vi.clearAllMocks()
})
afterEach(() => wrapper?.unmount())

describe('EventRoleTemplates', () => {
  it('lists the templates with their roles; only a custom one can be deleted', () => {
    mountIt()
    expect(wrapper.text()).toContain('Party of five')
    expect(wrapper.text()).toContain('Tank 1 · DPS')
    expect(wrapper.findAll('button[aria-label^="Delete template"]')).toHaveLength(1)
  })

  it('switches a preset on and tells the editor to read the list again', async () => {
    vi.mocked(api.setRoleTemplateEnabled).mockResolvedValue(templates[0]!)
    mountIt()
    await wrapper.findAll('[role="switch"]')[0]!.trigger('click')
    await flushPromises()
    expect(api.setRoleTemplateEnabled).toHaveBeenCalledWith('p1', true)
    expect(wrapper.emitted('changed')).toHaveLength(1)
  })

  it('deletes a custom template', async () => {
    vi.mocked(api.deleteRoleTemplate).mockResolvedValue()
    mountIt()
    await wrapper.get('button[aria-label^="Delete template"]').trigger('click')
    await flushPromises()
    expect(api.deleteRoleTemplate).toHaveBeenCalledWith('c1')
  })

  it('saves the roles of the editor as a template, without their ids, once it has a name', async () => {
    vi.mocked(api.createRoleTemplate).mockResolvedValue(templates[1]!)
    mountIt({ rows: [{ ...rows[0]!, id: 'role-1' }] })
    const save = wrapper.get('[data-testid="event-template-save"]')
    expect(save.attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="event-template-name"]').setValue(' Raid ')
    await save.trigger('click')
    await flushPromises()
    expect(api.createRoleTemplate).toHaveBeenCalledWith('Raid', [{ name: 'Tank', capacity: 1 }])
    expect((wrapper.get('[data-testid="event-template-name"]').element as HTMLInputElement).value).toBe('')
  })

  it('does not save invalid or empty role lists and reports a failure', async () => {
    mountIt({ rows: [{ ...blankDraft(), name: '' }] })
    await wrapper.get('[data-testid="event-template-name"]').setValue('X')
    await wrapper.get('[data-testid="event-template-save"]').trigger('click')
    expect(api.createRoleTemplate).not.toHaveBeenCalled()
    wrapper.unmount()
    vi.mocked(api.deleteRoleTemplate).mockRejectedValue(new Error('conflict'))
    mountIt()
    await wrapper.get('button[aria-label^="Delete template"]').trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.map(t => t.text).join()).toContain('conflict')
  })
})
