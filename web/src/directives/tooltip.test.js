import { describe, it, expect, afterEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, h, withDirectives, defineComponent } from 'vue'
import { tooltip } from './tooltip'

afterEach(() => {
  vi.useRealTimers()
  document.body.innerHTML = ''
})

const tip = () => document.getElementById('mnema-tooltip')

function Btn(value, mods) {
  return defineComponent({
    props: { v: { type: [String, Object], default: value } },
    setup(props) {
      return () => withDirectives(h('button', { type: 'button' }, 'x'), [[tooltip, props.v, '', mods || {}]])
    }
  })
}

describe('v-tooltip', () => {
  it('shows after 400 ms hover, hides on leave', async () => {
    vi.useFakeTimers()
    const w = mount(Btn('Antworten'), { attachTo: document.body })
    w.element.dispatchEvent(new Event('pointerenter'))
    vi.advanceTimersByTime(399)
    expect(tip()?.hidden ?? true).toBe(true)
    vi.advanceTimersByTime(2)
    expect(tip().hidden).toBe(false)
    expect(tip().getAttribute('role')).toBe('tooltip')
    expect(tip().textContent).toBe('Antworten')
    w.element.dispatchEvent(new Event('pointerleave'))
    expect(tip().hidden).toBe(true)
  })

  it('sets the aria-label from the same text, unless .visual', () => {
    const a = mount(Btn('Stummschalten'), { attachTo: document.body })
    expect(a.attributes('aria-label')).toBe('Stummschalten')
    const b = mount(Btn('Langer Name', { visual: true }), { attachTo: document.body })
    expect(b.attributes('aria-label')).toBeUndefined()
  })

  it('appends a shortcut hint in mono', () => {
    vi.useFakeTimers()
    const w = mount(Btn({ text: 'Stummschalten', shortcut: 'M' }), { attachTo: document.body })
    w.element.dispatchEvent(new Event('pointerenter'))
    vi.advanceTimersByTime(401)
    expect(tip().querySelector('.mnema-tooltip-kbd').textContent).toBe('M')
    expect(w.attributes('aria-label')).toBe('Stummschalten')
  })

  it('updates the label when the text changes (toggle buttons)', async () => {
    const w = mount(Btn('Stummschalten'), { attachTo: document.body })
    await w.setProps({ v: 'Stummschaltung aufheben' })
    await nextTick()
    expect(w.attributes('aria-label')).toBe('Stummschaltung aufheben')
  })

  it('does nothing for an empty value', () => {
    vi.useFakeTimers()
    const w = mount(Btn(''), { attachTo: document.body })
    w.element.dispatchEvent(new Event('pointerenter'))
    vi.advanceTimersByTime(500)
    expect(tip()?.hidden ?? true).toBe(true)
    expect(w.attributes('aria-label')).toBeUndefined()
  })
})
