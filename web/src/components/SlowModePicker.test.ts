import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import SlowModePicker from './SlowModePicker.vue'
import { setLocale } from '../i18n'

function mountPicker(modelValue: number) {
  setLocale('en')
  return mount(SlowModePicker, {
    props: { modelValue, 'onUpdate:modelValue': (v: number) => w.setProps({ modelValue: v }) }
  })
}
let w: ReturnType<typeof mountPicker>

describe('SlowModePicker', () => {
  it('falls back to Off when the custom amount is cleared, so the saved value is the shown one', async () => {
    w = mountPicker(0)
    await w.get('[data-testid="slow-mode-custom"]').setValue('45')
    expect(w.props('modelValue')).toBe(45)
    await w.get('[data-testid="slow-mode-custom"]').setValue('')
    expect(w.props('modelValue')).toBe(0)
    expect(w.get('[data-testid="slow-mode-0"]').attributes('aria-pressed')).toBe('true')
  })

  it('keeps a changed unit from overriding a chosen preset', async () => {
    w = mountPicker(30)
    await w.get('[data-testid="slow-mode-unit"]').setValue('minutes')
    expect(w.props('modelValue')).toBe(30)
  })

  it('follows a value that arrives from outside after the picker was created', async () => {
    w = mountPicker(0)
    await w.setProps({ modelValue: 45 })
    expect((w.get('[data-testid="slow-mode-custom"]').element as HTMLInputElement).value).toBe('45')
    await w.setProps({ modelValue: 7200 })
    expect((w.get('[data-testid="slow-mode-custom"]').element as HTMLInputElement).value).toBe('2')
    expect((w.get('[data-testid="slow-mode-unit"]').element as HTMLSelectElement).value).toBe('hours')
    await w.setProps({ modelValue: 60 })
    expect((w.get('[data-testid="slow-mode-custom"]').element as HTMLInputElement).value).toBe('')
    expect(w.get('[data-testid="slow-mode-60"]').attributes('aria-pressed')).toBe('true')
  })
})
