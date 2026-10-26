import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import Badge from './Badge.vue'

describe('Badge', () => {
  it('is the neutral unread pill by default', () => {
    const w = mount(Badge, { slots: { default: '3' } })
    expect(w.text()).toBe('3')
    expect(w.classes()).toContain('bg-mnema-hover')
    expect(w.classes()).toContain('rounded-full')
  })

  it.each([
    ['mention', 'bg-mnema-danger'],
    ['unread', 'bg-mnema-hover'],
    ['info', 'bg-mnema-accent']
  ] as const)('paints the %s variant', (variant, background) => {
    expect(mount(Badge, { props: { variant }, slots: { default: '1' } }).classes()).toContain(background)
  })

  it('lets the caller place it and label it', () => {
    const w = mount(Badge, { props: { variant: 'mention' }, attrs: { class: 'ml-auto', 'data-testid': 'dm-mentions', 'aria-label': '2 mentions' }, slots: { default: '@2' } })
    expect(w.classes()).toContain('ml-auto')
    expect(w.attributes('data-testid')).toBe('dm-mentions')
    expect(w.attributes('aria-label')).toBe('2 mentions')
  })
})
