import { requireValue } from '../test-fixtures.fixture'
/* eslint-disable vue/one-component-per-file */
import { describe, it, expect, afterEach } from 'vitest'
import type { VueWrapper } from '@vue/test-utils'
import type { ComponentPublicInstance } from 'vue'
import { mount } from '@vue/test-utils'
import { nextTick, defineComponent, h } from 'vue'
import BaseDialog from './BaseDialog.vue'

let wrapper!: VueWrapper<ComponentPublicInstance>
afterEach(() => {
  wrapper?.unmount()
  document.body.innerHTML = ''
})

function key(el: EventTarget, k: string, opts: KeyboardEventInit = {}) {
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
    expect(requireValue(dlg).getAttribute('aria-modal')).toBe('true')
    const heading = document.getElementById(requireValue(dlg).getAttribute('aria-labelledby') ?? '')
    expect(requireValue(heading).textContent).toBe('Titel')
  })

  it('moves focus into the dialog, skipping the close button', async () => {
    mountDialog()
    await nextTick()
    await nextTick()
    expect(requireValue(document.activeElement).id).toBe('first')
  })

  it('traps Tab and Shift+Tab inside', async () => {
    mountDialog()
    await nextTick()
    await nextTick()
    requireValue(document.getElementById('last')).focus()
    key(requireValue(document.activeElement), 'Tab')
    // wraps to the first focusable element: the close button in the header
    expect(requireValue(document.activeElement).hasAttribute('data-dialog-close')).toBe(true)
    key(requireValue(document.activeElement), 'Tab', { shiftKey: true })
    expect(requireValue(document.activeElement).id).toBe('last')
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
      expect(document.activeElement).toBe(opener)
  })

  it('only the topmost of stacked dialogs reacts to Escape', async () => {
    const closed: string[] = []
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

it('renders subtitle, badge and description and dismisses only full scrim presses', async () => {
  wrapper = mount(BaseDialog, { props: { title: 'Title', subtitle: 'Details', describedby: 'description', align: 'top' }, slots: { badge: '<span>Badge</span>', default: '<p id="description">Body</p>' }, attachTo: document.body })
  expect(wrapper.text()).toContain('Badge')
  expect(wrapper.text()).toContain('Details')
  expect(wrapper.get('[role="dialog"]').attributes('aria-describedby')).toBe('description')
  await wrapper.get('[role="dialog"]').trigger('pointerdown')
  await wrapper.trigger('pointerup')
  expect(wrapper.emitted('close')).toBeUndefined()
  await wrapper.trigger('pointerdown')
  await wrapper.get('[role="dialog"]').trigger('pointerup')
  expect(wrapper.emitted('close')).toBeUndefined()
  await wrapper.trigger('pointerdown'); await wrapper.trigger('pointerup')
  expect(wrapper.emitted('close')).toHaveLength(1)
  await wrapper.setProps({ closeOnScrim: false })
  await wrapper.trigger('pointerdown'); await wrapper.trigger('pointerup')
  expect(wrapper.emitted('close')).toHaveLength(1)
  await wrapper.get('[data-dialog-close]').trigger('click')
  expect(wrapper.emitted('close')).toHaveLength(2)
})

it('accepts custom slot headings and explicit initial focus', async () => {
  wrapper = mount(BaseDialog, { props: { initialFocus: '#custom' }, slots: { default: '<h2>Custom</h2><input id="custom" />' }, attachTo: document.body })
  await nextTick(); await nextTick()
  expect(wrapper.find('header').exists()).toBe(false)
  expect(document.activeElement?.id).toBe('custom')
})
