import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import PresenceDot from './PresenceDot.vue'

describe('PresenceDot', () => {
  it('gives every away dot its own mask id, even at the same size', () => {
    const host = mount({
      components: { PresenceDot },
      template: '<div><PresenceDot status="away" /><PresenceDot status="away" /></div>'
    })
    const ids = host.findAll('mask').map(m => m.attributes('id'))
    expect(ids).toHaveLength(2)
    expect(new Set(ids).size).toBe(2)
    host.findAll('svg').forEach((svg, i) => {
      expect(svg.get('circle[mask]').attributes('mask')).toBe(`url(#${ids[i]})`)
    })
  })

  it('paints the dark marks with a theme token, not a fixed colour', () => {
    const dnd = mount(PresenceDot, { props: { status: 'dnd' } })
    expect(dnd.get('rect').attributes('fill')).toBe('var(--color-mnema-accent-ink)')
  })
})
