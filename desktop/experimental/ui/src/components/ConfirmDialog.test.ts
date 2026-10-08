import { requireValue } from '../test-fixtures.fixture'
import { describe, it, expect, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import ConfirmDialog from './ConfirmDialog.vue'
import { confirm, pendingConfirm } from '../lib/confirm'

let wrapper!: ReturnType<typeof mount<typeof ConfirmDialog>>
afterEach(() => {
  wrapper?.unmount()
  pendingConfirm.value = null
  document.body.innerHTML = ''
})

const buttons = () => [...document.querySelectorAll<HTMLButtonElement>('[role="alertdialog"] button')].filter(b => !b.hasAttribute('data-dialog-close'))

describe('confirm() + ConfirmDialog', () => {
  it('names the object, focuses Cancel by default and resolves false on Cancel', async () => {
    wrapper = mount(ConfirmDialog, { attachTo: document.body })
    const p = confirm({ title: '#musik löschen?', body: 'Weg ist weg.', confirmLabel: 'Löschen', danger: true })
    await nextTick()
    await nextTick()
    const dlg = document.querySelector('[role="alertdialog"]')
    expect(requireValue(dlg).textContent).toContain('#musik löschen?')
    const [cancel, ok] = buttons()
    expect(requireValue(cancel).textContent.trim()).toBe('Abbrechen')
    expect(requireValue(ok).textContent.trim()).toBe('Löschen')
    expect(requireValue(ok).className).toContain('bg-mnema-danger')
    expect(document.activeElement).toBe(cancel)
    requireValue(cancel).click()
    expect(await p).toBe(false)
    await nextTick()
    expect(document.querySelector('[role="alertdialog"]')).toBeNull()
  })

  it('resolves true on confirm', async () => {
    wrapper = mount(ConfirmDialog, { attachTo: document.body })
    const p = confirm({ title: 'Sicher?', confirmLabel: 'Ja' })
    await nextTick()
    requireValue(buttons()[1]).click()
    expect(await p).toBe(true)
  })

  it('resolves false on Escape', async () => {
    wrapper = mount(ConfirmDialog, { attachTo: document.body })
    const p = confirm({ title: 'Sicher?' })
    await nextTick()
    await nextTick()
    requireValue(document.activeElement).dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    expect(await p).toBe(false)
  })

  it('a second request cancels the first', async () => {
    wrapper = mount(ConfirmDialog, { attachTo: document.body })
    const first = confirm({ title: 'A' })
    const second = confirm({ title: 'B' })
    expect(await first).toBe(false)
    await nextTick()
    expect(requireValue(document.querySelector('[role="alertdialog"]')).textContent).toContain('B')
    requireValue(pendingConfirm.value).resolve(true)
    expect(await second).toBe(true)
  })
})

it('renders excerpt and explicit cancel label while defaulting the confirm label', async () => {
  wrapper = mount(ConfirmDialog, { attachTo: document.body })
  const promise = confirm({ title: 'Archive?', excerpt: 'Selected message', cancelLabel: 'Keep' })
  await nextTick()
  expect(requireValue(document.querySelector('[role="alertdialog"]')).textContent).toContain('Selected message')
  expect(requireValue(buttons()[0]).textContent).toBe('Keep')
  expect(requireValue(buttons()[1]).textContent).toBe('Bestätigen')
  requireValue(buttons()[0]).click()
  expect(await promise).toBe(false)
})
