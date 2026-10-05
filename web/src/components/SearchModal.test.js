import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import SearchModal from './SearchModal.vue'
import { i18nPlugin, setLocale } from '../i18n'
import { tooltip } from '../directives/tooltip'

const mockApi = vi.fn()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal()),
  api: (...args) => mockApi(...args)
}))

const mountOpts = { global: { plugins: [i18nPlugin], directives: { tooltip } } }

let wrapper
let opener

async function open() {
  opener = document.createElement('button')
  document.body.appendChild(opener)
  opener.focus()
  wrapper = mount(SearchModal, { ...mountOpts, attachTo: document.body, props: { modelValue: false } })
  await wrapper.setProps({ modelValue: true })
  await nextTick()
  await nextTick()
}

const dialog = () => document.querySelector('[role="dialog"]')
const scrim = () => dialog().parentElement

beforeEach(() => {
  mockApi.mockReset()
  setActivePinia(createPinia())
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  document.body.innerHTML = ''
  setLocale('de')
})

describe('SearchModal', () => {
  it('is a labelled modal dialog that focuses the search input', async () => {
    await open()
    const dlg = dialog()
    expect(dlg.getAttribute('aria-modal')).toBe('true')
    const label = document.getElementById(dlg.getAttribute('aria-labelledby'))
    expect(label?.textContent).toBe('Suche')
    expect(document.activeElement).toBe(document.querySelector('[data-search-input]'))
  })

  it('closes on Escape and returns focus to the opener', async () => {
    await open()
    document.activeElement.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    expect(wrapper.emitted('update:modelValue')).toEqual([[false]])
    await wrapper.setProps({ modelValue: false })
    expect(dialog()).toBeNull()
    expect(document.activeElement).toBe(opener)
  })

  it('does not close when a text selection is released on the scrim', async () => {
    await open()
    const input = document.querySelector('[data-search-input]')
    input.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    scrim().dispatchEvent(new Event('pointerup', { bubbles: true }))
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()

    scrim().dispatchEvent(new Event('pointerdown', { bubbles: true }))
    scrim().dispatchEvent(new Event('pointerup', { bubbles: true }))
    expect(wrapper.emitted('update:modelValue')).toEqual([[false]])
  })

  it('formats result dates in the UI language', async () => {
    const created = '2026-03-04T10:15:00Z'
    mockApi.mockResolvedValue({ messages: [{ id: 'm1', channel_id: 'c1', username: 'ada', content: 'hello', created_at: created }], has_more: false })
    await open()
    const input = document.querySelector('[data-search-input]')
    input.value = 'hello'
    input.dispatchEvent(new Event('input'))
    await vi.waitFor(() => expect(document.querySelector('#search-result-0')).not.toBeNull())
    const expected = new Date(created).toLocaleDateString('de', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
    expect(document.querySelector('#search-result-0').textContent).toContain(expected)
  })
})
