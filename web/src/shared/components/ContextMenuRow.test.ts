import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { Check } from '@lucide/vue'
import ContextMenuRow from './ContextMenuRow.vue'
import type { ContextMenuItem } from './menuTypes'
let wrapper: ReturnType<typeof mount<typeof ContextMenuRow>>
afterEach(() => wrapper?.unmount())
describe('ContextMenuRow', () => {
  it.each<ContextMenuItem>([
    { type: 'separator' }, { type: 'label', label: 'Group' },
    { type: 'info', id: 'info', label: 'Transport', value: 'DTLS' },
    { type: 'info', label: 'Unavailable', value: null },
    { label: 'Action', id: 'action', icon: Check, subtitle: 'Details', shortcut: 'Ctrl+A', danger: true },
    { label: 'Disabled', disabled: true },
    { type: 'radio', label: 'Selected', checked: true },
    { type: 'radio', label: 'Unselected', checked: false },
    { type: 'checkbox', label: 'Checked', checked: true },
    { type: 'checkbox', label: 'Unchecked', checked: false }
  ])('renders the row $label with the appropriate accessible state', async item => {
    wrapper = mount(ContextMenuRow, { props: { item } })
    if (item.type === 'separator') expect(wrapper.attributes('role')).toBe('separator')
    else if (item.type === 'label') expect(wrapper.attributes('role')).toBe('presentation')
    else if (item.type === 'info') expect(wrapper.get('[aria-disabled]').attributes('aria-disabled')).toBe('true')
    else {
      expect(wrapper.attributes('role')).toBe(item.type === 'radio' ? 'menuitemradio' : item.type === 'checkbox' ? 'menuitemcheckbox' : 'menuitem')
      await wrapper.trigger('click')
      expect(wrapper.emitted('select')).toEqual(item.disabled ? undefined : [[item]])
      if (item.type === 'radio' || item.type === 'checkbox') expect(wrapper.attributes('aria-checked')).toBe(String(!!item.checked))
    }
  })
  it('supports custom slider limits and forwards numeric input without bubbling', async () => {
    const onInput = vi.fn<(value: number) => void>()
    wrapper = mount(ContextMenuRow, { props: { item: { type: 'slider', label: 'Volume', value: 40, min: 10, max: 90, step: 5, onInput } } })
    expect(wrapper.get('input').attributes()).toMatchObject({ min: '10', max: '90', step: '5', 'aria-valuetext': '40%' })
    await wrapper.get('input').setValue('55')
    expect(onInput).toHaveBeenCalledWith(55)
    await wrapper.trigger('mousedown'); await wrapper.trigger('click')
    expect(wrapper.emitted('select')).toBeUndefined()
    await wrapper.setProps({ item: { type: 'slider', label: 'Default limits', value: 0 } })
    expect(wrapper.get('input').attributes()).toMatchObject({ min: '0', max: '200', step: '1' })
    await wrapper.get('input').setValue('20')
  })
})
