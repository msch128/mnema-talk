import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'
import { setLocale } from '../i18n'
import { tooltip } from '../directives/tooltip'
import FormField from './FormField.vue'
import PasswordInput from './PasswordInput.vue'
import RadioCard from './RadioCard.vue'
import SwitchRow from './SwitchRow.vue'

let w: VueWrapper
beforeEach(() => setLocale('en'))
afterEach(() => w?.unmount())

describe('FormField', () => {
  const field = (props: Record<string, unknown>) => mount(FormField, {
    props: { label: 'Name', ...props },
    slots: { default: ({ id, describedby, invalid }: { id: string; describedby: string | undefined; invalid: boolean }) => h('input', { id, 'aria-describedby': describedby, 'aria-invalid': invalid ? 'true' : undefined }) }
  })

  it('ties the visible label to the control', () => {
    w = field({})
    const input = w.get('input')
    expect(w.get('label').attributes('for')).toBe(input.attributes('id'))
    expect(w.get('label').text()).toBe('Name')
    expect(input.attributes('aria-describedby')).toBeUndefined()
  })

  it('announces the hint and the error through the control', () => {
    w = field({ hint: 'At least 10 characters.', error: 'Too short.' })
    const ids = (w.get('input').attributes('aria-describedby') ?? '').split(' ')
    expect(ids).toHaveLength(2)
    expect(w.get(`#${ids[0]}`).text()).toBe('At least 10 characters.')
    expect(w.get(`#${ids[1]}`).attributes('role')).toBe('alert')
    expect(w.get('input').attributes('aria-invalid')).toBe('true')
  })

  it('keeps a pinned id so links and tests stay stable', () => {
    w = field({ inputId: 'welcome-bio' })
    expect(w.get('input').attributes('id')).toBe('welcome-bio')
    expect(w.get('label').attributes('for')).toBe('welcome-bio')
  })
})

describe('PasswordInput', () => {
  it('hides the password until asked and puts attributes on the input', async () => {
    w = mount(PasswordInput, { props: { modelValue: 'secret' }, attrs: { id: 'pw', autocomplete: 'new-password', 'data-testid': 'pw-input' }, global: { directives: { tooltip } } })
    const input = w.get('input')
    expect(input.attributes('type')).toBe('password')
    expect(input.attributes('autocomplete')).toBe('new-password')
    expect(input.attributes('data-testid')).toBe('pw-input')
    const toggle = w.get('[data-testid="password-toggle"]')
    expect(toggle.attributes('aria-label')).toBe('Show characters')
    await toggle.trigger('click')
    expect(input.attributes('type')).toBe('text')
    expect(toggle.attributes('aria-label')).toBe('Hide characters')
    expect(toggle.attributes('aria-pressed')).toBe('true')
    await toggle.trigger('click')
    expect(input.attributes('type')).toBe('password')
  })

  it('works with v-model and exposes its input', async () => {
    const Host = defineComponent({
      setup() {
        const value = ref('')
        return () => h(PasswordInput, { modelValue: value.value, 'onUpdate:modelValue': (v: string) => { value.value = v }, id: 'x' })
      }
    })
    w = mount(Host)
    await w.get('input').setValue('hunter2hunter2')
    expect((w.get('input').element as HTMLInputElement).value).toBe('hunter2hunter2')
    const field = mount(PasswordInput, { props: { modelValue: 'a' } })
    expect((field.vm as unknown as { el: HTMLInputElement }).el).toBe(field.get('input').element)
    field.unmount()
  })
})

describe('RadioCard', () => {
  it('is a real radio with a title and a description, and reports its choice', async () => {
    w = mount(RadioCard, { props: { name: 'dm', value: 'nobody', selected: false, title: 'Nobody', description: 'No one can start a chat.' }, attrs: { 'data-choice': 'nobody' } })
    const input = w.get('input')
    expect(input.attributes('type')).toBe('radio')
    expect(input.attributes('data-choice')).toBe('nobody')
    expect(w.text()).toContain('No one can start a chat.')
    await input.trigger('change')
    expect(w.emitted('select')).toEqual([['nobody']])
  })

  it('shows the selected state and the disabled state', () => {
    w = mount(RadioCard, { props: { name: 'dm', value: 'everyone', selected: true, title: 'Everyone', disabled: true } })
    expect((w.get('input').element as HTMLInputElement).checked).toBe(true)
    expect(w.get('input').attributes('disabled')).toBeDefined()
    expect(w.get('label').classes()).toContain('bg-mnema-accent/10')
  })
})

describe('SwitchRow', () => {
  it('is a labelled switch that asks the parent for the new state', async () => {
    w = mount(SwitchRow, { props: { modelValue: false, title: 'Show text', hint: 'Notifications carry the message.', hintTestid: 'hint' }, attrs: { 'data-testid': 'sw' } })
    const sw = w.get('[data-testid="sw"]')
    expect(sw.attributes('role')).toBe('switch')
    expect(sw.attributes('aria-checked')).toBe('false')
    expect(w.get(`#${sw.attributes('aria-labelledby')}`).text()).toBe('Show text')
    expect(w.get(`#${sw.attributes('aria-describedby')}`).text()).toBe('Notifications carry the message.')
    expect(w.get('[data-testid="hint"]').text()).toBe('Notifications carry the message.')
    await sw.trigger('click')
    expect(w.emitted('toggle')).toEqual([[true]])
    expect(w.emitted('update:modelValue')).toEqual([[true]])
    // Controlled: the state only changes when the parent says so.
    expect(sw.attributes('aria-checked')).toBe('false')
  })

  it('can be disabled and has no description when there is no hint', () => {
    w = mount(SwitchRow, { props: { modelValue: true, title: 'On', disabled: true }, attrs: { 'data-testid': 'sw' } })
    expect(w.get('[data-testid="sw"]').attributes('disabled')).toBeDefined()
    expect(w.get('[data-testid="sw"]').attributes('aria-describedby')).toBeUndefined()
    expect(w.get('[data-testid="sw"]').attributes('aria-checked')).toBe('true')
  })
})
