import type { VueWrapper } from '@vue/test-utils'

import { channelFixture, categoryFixture } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import EditNameDialog from './EditNameDialog.vue'
import BaseDialog from './BaseDialog.vue'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { i18nPlugin } from '../i18n'
import { tooltip } from '../directives/tooltip'

const mountOpts = { global: { plugins: [i18nPlugin], directives: { tooltip } } }

let wrapper: VueWrapper
beforeEach(() => setActivePinia(createPinia()))
afterEach(() => {
  wrapper?.unmount()
  // Wrapper was unmounted above.
})

describe('EditNameDialog', () => {
  it('shows a translated message when the name is blank', async () => {
    wrapper = mount(EditNameDialog, { ...mountOpts, props: { kind: 'category', entity: categoryFixture({ id: "00000000-0000-4000-8000-0000000003ee", name: '   ' }) } })
    await wrapper.find('form')!.trigger('submit')
    const alert = wrapper.find('[role="alert"]')!
    expect(alert.exists()).toBe(true)
    expect(alert.text()).toBe('Bitte gib einen Namen an.')
  })

  it('creates a category after the existing ones when it has no entity', async () => {
    const chat = useChatStore()
    chat.categories = [categoryFixture({ id: "00000000-0000-4000-8000-0000000003ef", sort_order: 0 }), categoryFixture({ id: "00000000-0000-4000-8000-0000000003f0", sort_order: 4 })]
    chat.createCategory = vi.fn(() => Promise.resolve(categoryFixture({ id: "00000000-0000-4000-8000-0000000003f1", name: 'Projekte' })))
    wrapper = mount(EditNameDialog, { ...mountOpts, attachTo: document.body, props: { kind: 'category' } })
    expect(document.body.textContent).toContain('Kategorie erstellen')
    expect(wrapper.find('button[type="submit"]')!.text()).toBe('Erstellen')
    await wrapper.find<HTMLInputElement>('#edit-name')!.setValue('  Projekte ')
    await wrapper.find('form')!.trigger('submit')
    await flushPromises()
    expect(chat.createCategory).toHaveBeenCalledWith('Projekte', 5)
    expect(wrapper.emitted('created')!).toEqual([[categoryFixture({ id: "00000000-0000-4000-8000-0000000003f1", name: 'Projekte' })]])
    expect(wrapper.emitted('close')!).toHaveLength(1)
    expect(useToastStore().toasts.map(t => t.text)).toEqual(['Kategorie erstellt'])
  })

  it("shows why a new category could not be created", async () => {
    const chat = useChatStore()
    chat.createCategory = vi.fn(() => Promise.reject(new Error('')))
    wrapper = mount(EditNameDialog, { ...mountOpts, props: { kind: 'category' } })
    await wrapper.find<HTMLInputElement>('#edit-name')!.setValue('Projekte')
    await wrapper.find('form')!.trigger('submit')
    await flushPromises()
    expect(wrapper.find('[role="alert"]')!.text()).toBe('Die Kategorie konnte nicht erstellt werden. Versuche es noch einmal.')
    expect(wrapper.emitted('close')!).toBeUndefined()
  })
})

describe('EditNameDialog editing', () => {
  it('renames a text channel and trims its topic', async () => {
    const chat = useChatStore()
    chat.updateChannel = vi.fn().mockResolvedValue(undefined)
    wrapper = mount(EditNameDialog, { props: { kind: 'channel', entity: channelFixture({ name: 'old', topic: 'prior' }) } })
    await wrapper.get('#edit-name').setValue('  renamed ')
    await wrapper.get('#edit-topic').setValue('  description ')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(chat.updateChannel).toHaveBeenCalledWith(channelFixture().id, { name: 'renamed', topic: 'description', slow_mode_seconds: 0 })
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(useToastStore().toasts.map(t => t.text)).toEqual(['Kanal aktualisiert'])
  })
  it('renames voice channels without a topic input and closes on cancel and shell dismissal', async () => {
    const chat = useChatStore()
    chat.updateChannel = vi.fn().mockResolvedValue(undefined)
    wrapper = mount(EditNameDialog, { props: { entity: channelFixture({ type: 'voice' }) } })
    expect(wrapper.find('#edit-topic').exists()).toBe(false)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(chat.updateChannel).toHaveBeenCalledWith(channelFixture().id, { name: 'general', topic: '', slow_mode_seconds: 0 })
    await wrapper.findAll('button').find(b => b.text() === 'Abbrechen')!.trigger('click')
    wrapper.getComponent(BaseDialog).vm.$emit('close')
    expect(wrapper.emitted('close')).toHaveLength(3)
  })
  it('sets the slow mode from a preset', async () => {
    const chat = useChatStore()
    chat.updateChannel = vi.fn().mockResolvedValue(undefined)
    wrapper = mount(EditNameDialog, { props: { kind: 'channel', entity: channelFixture({ slow_mode_seconds: 0 }) } })
    const presets = wrapper.findAll('[data-testid^="slow-mode-"][aria-pressed]').map(b => b.text())
    expect(presets).toEqual(['Aus', '10 s', '30 s', '1 Min.', '5 Min.', '30 Min.', '1 T'])
    await wrapper.get('[data-testid="slow-mode-300"]').trigger('click')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(chat.updateChannel).toHaveBeenCalledWith(channelFixture().id, { name: 'general', topic: '', slow_mode_seconds: 300 })
  })
  it('sets a free slow mode value in the chosen unit, keeps an unusual stored value, and refuses more than 24 hours', async () => {
    const chat = useChatStore()
    chat.updateChannel = vi.fn().mockResolvedValue(undefined)
    wrapper = mount(EditNameDialog, { props: { kind: 'channel', entity: channelFixture({ slow_mode_seconds: 45 }) } })
    expect(wrapper.get<HTMLInputElement>('[data-testid="slow-mode-custom"]').element.value).toBe('45')
    await wrapper.get('[data-testid="slow-mode-custom"]').setValue('3')
    await wrapper.get('[data-testid="slow-mode-unit"]').setValue('hours')
    expect(wrapper.find('[data-testid="slow-mode-error"]').exists()).toBe(false)
    await wrapper.get('[data-testid="slow-mode-custom"]').setValue('25')
    expect(wrapper.get('[data-testid="slow-mode-error"]').text()).toContain('24 Stunden')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="slow-mode-custom"]').setValue('3')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(chat.updateChannel).toHaveBeenCalledWith(channelFixture().id, { name: 'general', topic: '', slow_mode_seconds: 10800 })
  })
  it('shows the topic helper text with its length', async () => {
    wrapper = mount(EditNameDialog, { props: { kind: 'channel', entity: channelFixture({ topic: 'abc' }) } })
    expect(wrapper.get('#edit-topic-help').text()).toContain('3/255')
    expect(wrapper.get('#edit-topic').attributes('maxlength')).toBe('255')
  })
  it('offers no slow mode for categories', () => {
    wrapper = mount(EditNameDialog, { props: { kind: 'category', entity: categoryFixture() } })
    expect(wrapper.find('[data-testid="slow-mode"]').exists()).toBe(false)
  })
  it('renames an existing category and retains server failure details', async () => {
    const chat = useChatStore()
    chat.updateCategory = vi.fn().mockRejectedValueOnce(new Error('synthetic conflict')).mockRejectedValueOnce(new Error('')).mockResolvedValueOnce(undefined)
    wrapper = mount(EditNameDialog, { props: { kind: 'category', entity: categoryFixture() } })
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('synthetic conflict')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('Das Aktualisieren hat nicht geklappt. Versuche es noch einmal.')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(chat.updateCategory).toHaveBeenCalledWith(categoryFixture().id, { name: 'Synthetic category' })
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})
