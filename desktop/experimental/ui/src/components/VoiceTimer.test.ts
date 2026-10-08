import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import VoiceTimer from './VoiceTimer.vue'
import { now } from '../lib/clock'
describe('VoiceTimer', () => {
  it('hides without timestamp and updates elapsed server time', async () => {
    const previous = now.value; const wrapper = mount(VoiceTimer)
    try {
      expect(wrapper.find('[data-voice-timer]').exists()).toBe(false)
      now.value = Date.parse('2026-01-01T10:01:00Z'); await wrapper.setProps({ since: '2026-01-01T10:00:00Z' })
      expect(wrapper.text()).toBe('1:00'); now.value += 1000; await wrapper.vm.$nextTick(); expect(wrapper.text()).toBe('1:01')
    } finally { now.value = previous; wrapper.unmount() }
  })
})
