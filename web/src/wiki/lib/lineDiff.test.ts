import { describe, expect, it } from 'vitest'
import { diffStats, lineDiff, MAX_DIFF_CELLS } from './lineDiff'

describe('lineDiff', () => {
  it('marks added, removed and unchanged lines', () => {
    const ops = lineDiff('a\nb\nc\n', 'a\nx\nc\nd\n')
    expect(ops).toEqual([
      { kind: 'same', text: 'a' },
      { kind: 'del', text: 'b' },
      { kind: 'add', text: 'x' },
      { kind: 'same', text: 'c' },
      { kind: 'add', text: 'd' }
    ])
    expect(diffStats(ops ?? [])).toEqual({ added: 2, removed: 1 })
  })

  it('treats a trailing newline as no extra line and equal texts as unchanged', () => {
    expect(lineDiff('a\nb', 'a\nb\n')?.every(op => op.kind === 'same')).toBe(true)
    expect(lineDiff('', '')).toEqual([])
  })

  it('handles one side empty', () => {
    expect(lineDiff('', 'x\ny')).toEqual([{ kind: 'add', text: 'x' }, { kind: 'add', text: 'y' }])
    expect(lineDiff('x\ny', '')).toEqual([{ kind: 'del', text: 'x' }, { kind: 'del', text: 'y' }])
  })

  it('finds the common lines in the middle', () => {
    const ops = lineDiff('1\nA\n2\nB\n3', '1\nB\n2\nA\n3') ?? []
    expect(ops.filter(op => op.kind === 'same').length).toBeGreaterThanOrEqual(3)
    const rebuilt = ops.filter(op => op.kind !== 'del').map(op => op.text).join('\n')
    expect(rebuilt).toBe('1\nB\n2\nA\n3')
    expect(ops.filter(op => op.kind !== 'add').map(op => op.text).join('\n')).toBe('1\nA\n2\nB\n3')
  })

  it('gives up on texts too large to compare line by line', () => {
    const side = Math.ceil(Math.sqrt(MAX_DIFF_CELLS)) + 10
    const a = Array.from({ length: side }, (_, i) => `a${i}`).join('\n')
    const b = Array.from({ length: side }, (_, i) => `b${i}`).join('\n')
    expect(lineDiff(a, b)).toBeNull()
  })

  it('keeps a long text with a small change cheap', () => {
    const base = Array.from({ length: 5000 }, (_, i) => `line ${i}`)
    const changed = [...base]
    changed[2500] = 'changed'
    const ops = lineDiff(base.join('\n'), changed.join('\n'))
    expect(ops && diffStats(ops)).toEqual({ added: 1, removed: 1 })
  })
})

describe('lineDiff when lines repeat', () => {
  it('keeps the longest common lines and reproduces both texts', () => {
    const cases: [string, string][] = [
      ['a\nb\na\nb', 'b\na\nb\na'],
      ['x\nx\nx', 'x'],
      ['x', 'x\nx\nx'],
      ['1\n2\n3\n4\n5', '5\n4\n3\n2\n1'],
      ['a\n\nb', 'a\nb\n\n']
    ]
    for (const [before, after] of cases) {
      const ops = lineDiff(before, after) ?? []
      const keep = (s: string) => (s === '' ? [] : s.replace(/\n$/, '').split('\n'))
      expect(ops.filter(o => o.kind !== 'add').map(o => o.text)).toEqual(keep(before))
      expect(ops.filter(o => o.kind !== 'del').map(o => o.text)).toEqual(keep(after))
    }
  })
})
