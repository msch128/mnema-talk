import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import EditNameDialog from './EditNameDialog.vue'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { i18nPlugin } from '../i18n'
import { tooltip } from '../directives/tooltip'

const mountOpts = { global: { plugins: [i18nPlugin], directives: { tooltip } } }

let wrapper
beforeEach(() => setActivePinia(createPinia()))
afterEach(() => {
  wrapper?.unmount()
  wrapper = null
})

describe('EditNameDialog', () => {
  it('shows a translated message when the name is blank', async () => {
    wrapper = mount(EditNameDialog, { ...mountOpts, props: { kind: 'category', entity: { id: 'c1', name: '   ' } } })
    await wrapper.find('form').trigger('submit')
    const alert = wrapper.find('[role="alert"]')
    expect(alert.exists()).toBe(true)
    expect(alert.text()).toBe('Bitte gib einen Namen an.')
  })

  it('creates a category after the existing ones when it has no entity', async () => {
    const chat = useChatStore()
    chat.categories = [{ id: 'a', sort_order: 0 }, { id: 'b', sort_order: 4 }]
    chat.createCategory = vi.fn(() => Promise.resolve({ id: 'new', name: 'Projekte' }))
    wrapper = mount(EditNameDialog, { ...mountOpts, attachTo: document.body, props: { kind: 'category' } })
    expect(document.body.textContent).toContain('Kategorie erstellen')
    expect(wrapper.find('button[type="submit"]').text()).toBe('Erstellen')
    await wrapper.find('#edit-name').setValue('  Projekte ')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(chat.createCategory).toHaveBeenCalledWith('Projekte', 5)
    expect(wrapper.emitted('created')).toEqual([[{ id: 'new', name: 'Projekte' }]])
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(useToastStore().toasts.map(t => t.text)).toEqual(['Kategorie erstellt'])
  })

  it('shows why a new category could not be created', async () => {
    const chat = useChatStore()
    chat.createCategory = vi.fn(() => Promise.reject(new Error('')))
    wrapper = mount(EditNameDialog, { ...mountOpts, props: { kind: 'category' } })
    await wrapper.find('#edit-name').setValue('Projekte')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').text()).toBe('Die Kategorie konnte nicht erstellt werden.')
    expect(wrapper.emitted('close')).toBeUndefined()
  })
})
