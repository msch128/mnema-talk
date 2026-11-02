import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import RepeatBadge from './RepeatBadge.vue'
import { setLocale } from '../i18n'
import { eventWithRoles } from '../eventRoles.fixture'

let wrapper: VueWrapper
beforeEach(() => setLocale('en'))
afterEach(() => wrapper?.unmount())

const series = (over = {}) => ({ id: 's', frequency: 'weekly' as const, timezone: 'UTC', index: 2, ended: false, ...over })

describe('RepeatBadge', () => {
  it('says that an event repeats, and when the series is over', async () => {
    wrapper = mount(RepeatBadge, { props: { event: eventWithRoles({ series: series() }) } })
    expect(wrapper.text()).toBe('Repeats every week')
    await wrapper.setProps({ event: eventWithRoles({ series: series({ frequency: 'monthly', ended: true }) }) })
    expect(wrapper.text()).toContain('Repeats every month')
    expect(wrapper.text()).toContain('series over')
  })

  it('shows nothing for a single event', () => {
    wrapper = mount(RepeatBadge, { props: { event: eventWithRoles() } })
    expect(wrapper.find('[data-testid="repeat-badge"]').exists()).toBe(false)
  })
})
