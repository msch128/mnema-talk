import { describe, it, expect, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import ConfirmDialog from './ConfirmDialog.vue'
import { confirm, pendingConfirm } from '../lib/confirm'

let wrapper
afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  pendingConfirm.value = null
  document.body.innerHTML = ''
})

const buttons = () => [...document.querySelectorAll('[role="alertdialog"] button')].filter(b => !b.hasAttribute('data-dialog-close'))

describe('confirm() + ConfirmDialog', () => {
  it('names the object, focuses Cancel by default and resolves false on Cancel', async () => {
    wrapper = mount(ConfirmDialog, { attachTo: document.body })
    const p = confirm({ title: '#musik löschen?', body: 'Weg ist weg.', confirmLabel: 'Löschen', danger: true })
    await nextTick()
    await nextTick()
    const dlg = document.querySelector('[role="alertdialog"]')
    expect(dlg.textContent).toContain('#musik löschen?')
    const [cancel, ok] = buttons()
    expect(cancel.textContent.trim()).toBe('Abbrechen')
    expect(ok.textContent.trim()).toBe('Löschen')
    expect(ok.className).toContain('bg-mnema-danger')
    expect(document.activeElement).toBe(cancel)
    cancel.click()
    expect(await p).toBe(false)
    await nextTick()
    expect(document.querySelector('[role="alertdialog"]')).toBeNull()
  })

  it('resolves true on confirm', async () => {
    wrapper = mount(ConfirmDialog, { attachTo: document.body })
    const p = confirm({ title: 'Sicher?', confirmLabel: 'Ja' })
    await nextTick()
    buttons()[1].click()
    expect(await p).toBe(true)
  })

  it('resolves false on Escape', async () => {
    wrapper = mount(ConfirmDialog, { attachTo: document.body })
    const p = confirm({ title: 'Sicher?' })
    await nextTick()
    await nextTick()
    document.activeElement.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    expect(await p).toBe(false)
  })

  it('a second request cancels the first', async () => {
    wrapper = mount(ConfirmDialog, { attachTo: document.body })
    const first = confirm({ title: 'A' })
    const second = confirm({ title: 'B' })
    expect(await first).toBe(false)
    await nextTick()
    expect(document.querySelector('[role="alertdialog"]').textContent).toContain('B')
    pendingConfirm.value.resolve(true)
    expect(await second).toBe(true)
  })
})
