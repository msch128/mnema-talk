import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import WikiDiff from './WikiDiff.vue'
import { MAX_DIFF_CELLS } from '../lib/lineDiff'

afterEach(() => vi.useRealTimers())

describe('WikiDiff', () => {
  it('compares once the writer pauses, not on every key', async () => {
    vi.useFakeTimers()
    const w = mount(WikiDiff, { props: { before: 'a\nb', after: 'a\nb' } })
    await w.setProps({ after: 'a\nb1' })
    await w.setProps({ after: 'a\nb12' })
    expect(w.find('[data-op="add"]').exists()).toBe(false)
    vi.advanceTimersByTime(300)
    await w.vm.$nextTick()
    expect(w.get('[data-op="add"]').text()).toContain('b12')
  })

  it('shows at once what a different text to compare against changes', async () => {
    vi.useFakeTimers()
    const w = mount(WikiDiff, { props: { before: 'a', after: 'a' } })
    await w.setProps({ before: 'x', after: 'y' })
    expect(w.findAll('[data-op]').map(o => o.attributes('data-op'))).toEqual(['del', 'add'])
  })

  it('marks added and removed lines with signs and words, not colour alone', () => {
    const w = mount(WikiDiff, { props: { before: 'a\nb\nc', after: 'a\nx\nc' } })
    const ops = w.findAll('li').map(li => li.attributes('data-op'))
    expect(ops).toEqual(['same', 'del', 'add', 'same'])
    expect(w.find('[data-op="del"]').text()).toContain('−')
    expect(w.find('[data-op="add"]').text()).toContain('+')
    expect(w.find('[data-op="add"] .sr-only').text()).not.toBe('')
    expect(w.get('[data-testid="wiki-diff-stats"]').text()).toMatch(/1.*1/)
  })

  it('does not claim "no changes" when only the end of the text differs in blank space', () => {
    const same = mount(WikiDiff, { props: { before: 'same', after: 'same' } }).get('[data-testid="wiki-diff-stats"]').text()
    const spaced = mount(WikiDiff, { props: { before: 'same', after: 'same\n' } }).get('[data-testid="wiki-diff-stats"]').text()
    expect(spaced).not.toBe(same)
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
