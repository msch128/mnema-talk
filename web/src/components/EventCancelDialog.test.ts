import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import EventCancelDialog from './EventCancelDialog.vue'
import { setLocale } from '../i18n'

let wrapper: VueWrapper
beforeEach(() => setLocale('en'))
afterEach(() => {
  wrapper?.unmount()
  document.body.innerHTML = ''
})

describe('EventCancelDialog', () => {
  it('cancels only this event by default', async () => {
    wrapper = mount(EventCancelDialog, { attachTo: document.body })
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('confirm')).toEqual([['this']])
  })

  it('says what ending the series means and confirms the following scope', async () => {
    wrapper = mount(EventCancelDialog, { attachTo: document.body })
    await wrapper.get('[data-testid="scope-following"]').setValue(true)
    expect(wrapper.text()).toContain('series ends')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('confirm')).toEqual([['following']])
  })

  it('closes without cancelling when the member keeps the event', async () => {
    wrapper = mount(EventCancelDialog, { attachTo: document.body })
    const keep = wrapper.findAll('button').find(b => b.text() === 'Keep event')
    await keep?.trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(wrapper.emitted('confirm')).toBeUndefined()
  })
})
