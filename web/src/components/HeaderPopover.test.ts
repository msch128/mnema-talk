import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import HeaderPopover from './HeaderPopover.vue'

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
