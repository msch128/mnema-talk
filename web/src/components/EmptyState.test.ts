import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { Flag } from '@lucide/vue'
import EmptyState from './EmptyState.vue'

describe('EmptyState', () => {
  it('shows the sentence, the optional hint and the action', () => {
    const w = mount(EmptyState, { props: { icon: Flag, text: 'No reports here.', hint: 'They show up when someone reports a message.' }, slots: { default: '<button>Go</button>' } })
    expect(w.text()).toContain('No reports here.')
    expect(w.text()).toContain('They show up when')
    expect(w.find('button').exists()).toBe(true)
  })

  it('omits the hint and the action when there are none', () => {
    const w = mount(EmptyState, { props: { icon: Flag, text: 'Nothing.' } })
    expect(w.findAll('p')).toHaveLength(1)
    expect(w.find('button').exists()).toBe(false)
  })
})
