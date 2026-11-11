import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import EventRepeatFields from './EventRepeatFields.vue'
import { setLocale } from '../../i18n'

let wrapper: VueWrapper
beforeEach(() => setLocale('en'))
afterEach(() => wrapper?.unmount())

describe('EventRepeatFields', () => {
  it('offers the three frequencies and asks for an end date only once the event repeats', async () => {
    wrapper = mount(EventRepeatFields, { props: { repeat: '', until: '' } })
    const options = wrapper.findAll('option').map(o => o.text())
    expect(options).toEqual(['Does not repeat', 'Every week', 'Every two weeks', 'Every month'])
    expect(wrapper.find('[data-testid="event-repeat-until"]').exists()).toBe(false)

    await wrapper.get('[data-testid="event-repeat-select"]').setValue('monthly')
    expect(wrapper.emitted('update:repeat')?.at(-1)).toEqual(['monthly'])
    await wrapper.setProps({ repeat: 'monthly' })
    await wrapper.get('[data-testid="event-repeat-until"]').setValue('2027-01-31')
    expect(wrapper.emitted('update:until')?.at(-1)).toEqual(['2027-01-31'])
  })
})
