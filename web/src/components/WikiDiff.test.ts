import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import WikiDiff from './WikiDiff.vue'
import { MAX_DIFF_CELLS } from '../lib/lineDiff'

describe('WikiDiff', () => {
  it('marks added and removed lines with signs and words, not colour alone', () => {
    const w = mount(WikiDiff, { props: { before: 'a\nb\nc', after: 'a\nx\nc' } })
    const ops = w.findAll('li').map(li => li.attributes('data-op'))
    expect(ops).toEqual(['same', 'del', 'add', 'same'])
    expect(w.find('[data-op="del"]').text()).toContain('−')
    expect(w.find('[data-op="add"]').text()).toContain('+')
    expect(w.find('[data-op="add"] .sr-only').text()).not.toBe('')
    expect(w.get('[data-testid="wiki-diff-stats"]').text()).toMatch(/1.*1/)
  })

  it('says so when nothing changed', () => {
    const w = mount(WikiDiff, { props: { before: 'same', after: 'same' } })
    expect(w.find('li').exists()).toBe(false)
    expect(w.get('[data-testid="wiki-diff-stats"]').text()).not.toBe('')
  })

  it('does not run its markup: lines are text', () => {
    const w = mount(WikiDiff, { props: { before: '', after: '<img src=x onerror=alert(1)>' } })
    expect(w.find('img').exists()).toBe(false)
    expect(w.text()).toContain('<img src=x')
  })

  it('shows a notice instead of comparing texts that are too large', () => {
    const side = Math.ceil(Math.sqrt(MAX_DIFF_CELLS)) + 10
    const a = Array.from({ length: side }, (_, i) => `a${i}`).join('\n')
    const b = Array.from({ length: side }, (_, i) => `b${i}`).join('\n')
    const w = mount(WikiDiff, { props: { before: a, after: b } })
    expect(w.find('li').exists()).toBe(false)
    expect(w.text()).not.toBe('')
  })
})
