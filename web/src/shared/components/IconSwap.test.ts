import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { Mic, MicOff } from '@lucide/vue'
import IconSwap from './IconSwap.vue'

describe('IconSwap', () => {
  it('shows the icon of the current state, hidden from assistive technology', async () => {
    const w = mount(IconSwap, { props: { on: false, onIcon: MicOff, offIcon: Mic } })
    expect(w.attributes('aria-hidden')).toBe('true')
    expect(w.find('svg.lucide-mic').exists()).toBe(true)
    await w.setProps({ on: true })
    expect(w.find('svg.lucide-mic-off').exists()).toBe(true)
    expect(w.find('svg.lucide-mic').exists()).toBe(false)
  })

  it('sizes the icon as asked', () => {
    const w = mount(IconSwap, { props: { on: true, onIcon: MicOff, offIcon: Mic, iconClass: 'h-[18px] w-[18px]' } })
    expect(w.find('svg').classes()).toContain('h-[18px]')
  })

  it('takes lucide icons (functional components) without a prop warning', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    mount(IconSwap, { props: { on: false, onIcon: MicOff, offIcon: Mic } })
    expect(warn.mock.calls.filter(c => String(c[0]).includes('Invalid prop'))).toEqual([])
    warn.mockRestore()
  })
})
