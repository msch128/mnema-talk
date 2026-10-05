/* eslint-disable vue/one-component-per-file */
import { describe, it, expect, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, defineComponent, h } from 'vue'
import BaseDialog from './BaseDialog.vue'

let wrapper
afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  document.body.innerHTML = ''
})

function key(el, k, opts = {}) {
  el.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true, ...opts }))
}

function mountDialog(props = {}) {
  const opener = document.createElement('button')
  opener.textContent = 'open'
  document.body.appendChild(opener)
  opener.focus()
  wrapper = mount(
    defineComponent({
      components: { BaseDialog },
      setup: () => () => h(BaseDialog, { title: 'Titel', ...props }, {
        default: () => [
          h('input', { id: 'first' }),
          h('button', { id: 'second' }, 'Ok'),
          h('button', { id: 'last' }, 'Ende')
        ]
      })
    }),
    { attachTo: document.body }
  )
  return opener
}

describe('BaseDialog', () => {
  it('has dialog semantics and is labelled by its title', async () => {
    mountDialog()
    await nextTick()
    const dlg = document.querySelector('[role="dialog"]')
    expect(dlg.getAttribute('aria-modal')).toBe('true')
    const heading = document.getElementById(dlg.getAttribute('aria-labelledby'))
    expect(heading.textContent).toBe('Titel')
  })

  it('moves focus into the dialog, skipping the close button', async () => {
    mountDialog()
    await nextTick()
    await nextTick()
    expect(document.activeElement.id).toBe('first')
  })

  it('traps Tab and Shift+Tab inside', async () => {
    mountDialog()
    await nextTick()
    await nextTick()
    document.getElementById('last').focus()
    key(document.activeElement, 'Tab')
    // wraps to the first focusable element: the close button in the header
    expect(document.activeElement.hasAttribute('data-dialog-close')).toBe(true)
    key(document.activeElement, 'Tab', { shiftKey: true })
    expect(document.activeElement.id).toBe('last')
  })

  it('closes on Escape', async () => {
    mountDialog()
    await nextTick()
    key(document.activeElement || document.body, 'Escape')
    expect(wrapper.findComponent(BaseDialog).emitted('close')).toHaveLength(1)
  })

  it('restores focus to the opener when unmounted', async () => {
    const opener = mountDialog()
    await nextTick()
    await nextTick()
    expect(document.activeElement).not.toBe(opener)
    wrapper.unmount()
    wrapper = null
    expect(document.activeElement).toBe(opener)
  })

  it('only the topmost of stacked dialogs reacts to Escape', async () => {
    const closed = []
    wrapper = mount(
      defineComponent({
        setup: () => () => h('div', [
          h(BaseDialog, { title: 'A', onClose: () => closed.push('A') }, { default: () => h('input') }),
          h(BaseDialog, { title: 'B', onClose: () => closed.push('B') }, { default: () => h('input') })
        ])
      }),
      { attachTo: document.body }
    )
    await nextTick()
    key(document.body, 'Escape')
    expect(closed).toEqual(['B'])
  })

  it('supports alertdialog role', async () => {
    mountDialog({ role: 'alertdialog' })
    await nextTick()
    expect(document.querySelector('[role="alertdialog"]')).not.toBeNull()
  })
})
