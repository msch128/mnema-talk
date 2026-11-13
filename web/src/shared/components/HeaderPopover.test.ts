import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import HeaderPopover from './HeaderPopover.vue'

vi.mock('../lib/keepInsideColumn', () => ({ keepInsideColumn: vi.fn() }))

describe('HeaderPopover', () => {
  it('is a labelled dialog with the title above scrolling rows', () => {
    const w = mount(HeaderPopover, { props: { title: 'Pinned messages' }, slots: { default: '<p id="row">row</p>' }, attrs: { 'data-testid': 'pinned-panel' } })
    expect(w.attributes('role')).toBe('dialog')
    expect(w.attributes('aria-label')).toBe('Pinned messages')
    expect(w.attributes('data-testid')).toBe('pinned-panel')
    expect(w.text()).toContain('Pinned messages')
    expect(w.get('#row').element.parentElement?.className).toContain('overflow-y-auto')
  })
})

describe('HeaderPopover placement', () => {
  it('is placed inside its column on mount and on every resize until it closes', async () => {
    const { keepInsideColumn } = await import('../lib/keepInsideColumn')
    const w = mount(HeaderPopover, { props: { title: 'Saved' } })
    const placed = () => vi.mocked(keepInsideColumn).mock.calls.filter(([card]) => card === w.element).length
    expect(placed()).toBe(1)
    window.dispatchEvent(new Event('resize'))
    expect(placed()).toBe(2)
    w.unmount()
    window.dispatchEvent(new Event('resize'))
    expect(placed()).toBe(2)
  })
})
