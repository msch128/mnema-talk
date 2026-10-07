import { describe, it, expect, afterEach, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { h, withDirectives, defineComponent, type DirectiveBinding } from 'vue'
import { tooltip, showTip, hideTip, type TooltipValue, type TooltipElement } from './tooltip'
import { requireValue } from '../test-fixtures.fixture'

const wrappers: VueWrapper[] = []
afterEach(() => { wrappers.splice(0).forEach(w => w.unmount()); hideTip(); vi.restoreAllMocks(); vi.useRealTimers(); document.body.innerHTML = '' })
const tip = () => requireValue(document.getElementById('mnema-tooltip'))
function button(value: TooltipValue, mods: Record<string, boolean> = {}) {
  const component = defineComponent((props: { v?: TooltipValue }) =>
    () => withDirectives(h('button', { type: 'button' }, 'x'), [[tooltip, props.v, '', mods]]),
  { props: { v: { type: [String, Object], default: undefined } } })
  const wrapper = mount(component, { attachTo: document.body, props: { v: value } })
  wrappers.push(wrapper)
  return wrapper
}
function enter(w: VueWrapper, pointerType = 'mouse') { w.element.dispatchEvent(new PointerEvent('pointerenter', { pointerType })) }
function focus(w: VueWrapper, match: boolean | Error = true) {
  vi.spyOn(w.element, 'matches').mockImplementation(() => { if (match instanceof Error) throw match; return match })
  w.element.dispatchEvent(new Event('focus'))
}
function binding(value: TooltipValue, modifiers: Record<string, boolean> = {}): DirectiveBinding<TooltipValue> {
  return { value, oldValue: undefined, instance: null, dir: tooltip, modifiers }
}

describe('v-tooltip', () => {
  it('delays hover, sets accessible text and cancels hover on leave', () => {
    vi.useFakeTimers()
    const w = button('Antworten')
    enter(w)
    vi.advanceTimersByTime(399)
    expect(document.getElementById('mnema-tooltip')?.hidden ?? true).toBe(true)
    vi.advanceTimersByTime(1)
    expect(tip().hidden).toBe(false)
    expect(tip().getAttribute('role')).toBe('tooltip')
    expect(tip().textContent).toBe('Antworten')
    expect(w.attributes('aria-describedby')).toBe('mnema-tooltip')
    w.element.dispatchEvent(new Event('pointerleave'))
    expect(tip().hidden).toBe(true)
    expect(w.attributes('aria-describedby')).toBeUndefined()
    enter(w)
    w.element.dispatchEvent(new Event('pointerleave'))
    vi.advanceTimersByTime(500)
    expect(tip().hidden).toBe(true)
  })
  it('uses keyboard focus only, with fallback for engines lacking focus-visible support', () => {
    const w = button('Mute')
    focus(w, false)
    expect(document.getElementById('mnema-tooltip')?.hidden ?? true).toBe(true)
    focus(w)
    expect(tip().hidden).toBe(false)
    w.element.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight' }))
    expect(tip().hidden).toBe(false)
    w.element.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(tip().hidden).toBe(true)
    focus(w, new Error('unsupported selector'))
    expect(tip().hidden).toBe(false)
    w.element.dispatchEvent(new Event('blur'))
    expect(tip().hidden).toBe(true)
  })
  it('ignores touch hover, hides on pointer press and unmount removes listeners', () => {
    vi.useFakeTimers()
    const w = button('Help')
    enter(w, 'touch')
    vi.advanceTimersByTime(500)
    expect(document.getElementById('mnema-tooltip')?.hidden ?? true).toBe(true)
    focus(w)
    w.element.dispatchEvent(new Event('pointerdown'))
    expect(tip().hidden).toBe(true)
    const remove = vi.spyOn(w.element, 'removeEventListener')
    w.unmount()
    expect(remove.mock.calls.map(call => call[0])).toEqual(expect.arrayContaining(['pointerenter', 'pointerleave', 'pointerdown', 'focus', 'blur', 'keydown']))
    enter(w)
    vi.advanceTimersByTime(500)
    expect(tip().hidden).toBe(true)
  })
  it('preserves the visual accessible name and updates text and shortcut while shown', async () => {
    const visual = button('Long name', { visual: true })
    expect(visual.attributes('aria-label')).toBeUndefined()
    const w = button({ text: 'Mute', shortcut: 'M' })
    focus(w)
    expect(tip().querySelector('.mnema-tooltip-kbd')?.textContent).toBe('M')
    await w.setProps({ v: 'Unmute' })
    expect(tip().textContent).toBe('Unmute')
    expect(w.attributes('aria-label')).toBe('Unmute')
    await w.setProps({ v: null })
    expect(w.attributes('aria-label')).toBeUndefined()
    expect(tip().hidden).toBe(true)
  })
  it('ignores missing/empty values and disconnected targets', () => {
    vi.useFakeTimers()
    for (const value of [undefined, null, '', { text: '' }, { text: 'No shortcut' }]) {
      const w = button(value)
      enter(w)
      vi.advanceTimersByTime(400)
      expect(w.attributes('aria-label')).toBe(value && typeof value === 'object' && value.text ? value.text : undefined)
      w.unmount()
    }
    const detached = document.createElement('button')
    showTip(detached)
    const w = button('Delayed')
    enter(w)
    w.element.remove()
    vi.advanceTimersByTime(400)
    expect(tip().hidden).toBe(true)
  })
  it('keeps an active tooltip when another target leaves and reattaches the shared element', () => {
    const a = button('First'), b = button('Second')
    focus(a)
    b.element.dispatchEvent(new Event('pointerleave'))
    expect(tip().hidden).toBe(false)
    tip().remove()
    focus(b)
    expect(tip().isConnected).toBe(true)
    expect(tip().textContent).toBe('Second')
  })
  it('positions above when possible, below near the top, and clamps both viewport edges', () => {
    const w = button('Position')
    vi.spyOn(w.element, 'getBoundingClientRect').mockReturnValue(new DOMRect(100, 100, 40, 20))
    focus(w)
    expect(tip().style.top).toBe('94px')
    expect(tip().style.left).toBe('120px')
    vi.spyOn(w.element, 'getBoundingClientRect').mockReturnValue(new DOMRect(-100, 0, 40, 20))
    focus(w)
    expect(tip().style.left).toBe('8px')
    expect(tip().style.top).toBe('26px')
    vi.spyOn(w.element, 'getBoundingClientRect').mockReturnValue(new DOMRect(window.innerWidth + 100, 100, 40, 20))
    focus(w)
    expect(tip().style.left).toBe(`${window.innerWidth - 8}px`)
  })
  it('tolerates cleanup before mounting and binding without modifiers', () => {
    const el: TooltipElement = document.createElement('button')
    requireValue(tooltip.beforeUnmount)(el, binding(null), { ...h('button'), el: null, target: null }, null)
    requireValue(tooltip.updated)(el, binding({ text: 'Standalone' }), { ...h('button'), el: null, target: null }, { ...h('button'), el: null, target: null })
    expect(el.getAttribute('aria-label')).toBe('Standalone')
  })
})
