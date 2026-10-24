import { describe, expect, it } from 'vitest'
import { diagnoseSearch } from './searchProblems'

describe('diagnoseSearch', () => {
  it('has nothing to report for a clean query', () => {
    expect(diagnoseSearch('hello in:general ', { in: 'general' }, [])).toEqual({ problems: [], pending: false })
  })

  it('explains a finished filter that matches no member or channel', () => {
    const d = diagnoseSearch('from:nobody in:nowhere ', { from: 'nobody', in: 'nowhere' }, ['from', 'in'])
    expect(d.problems).toEqual([
      { kind: 'member', filter: 'from', value: 'nobody' },
      { kind: 'channel', filter: 'in', value: 'nowhere' },
    ])
    expect(d.pending).toBe(false)
  })

  it('explains values that are not valid for has, is, author and dates, and empty ones', () => {
    const d = diagnoseSearch('has:foo is:x author:y before:soon from: ', {}, [])
    expect(d.problems.map(p => p.kind)).toEqual(['option', 'option', 'option', 'date', 'empty'])
    expect(d.problems[0]).toMatchObject({ filter: 'has', value: 'foo' })
    expect(d.problems[0]).toMatchObject({ options: expect.stringContaining('image') as string })
  })

  it('only waits while the last token is still being typed', () => {
    expect(diagnoseSearch('has:fo', {}, [])).toEqual({ problems: [], pending: true })
    expect(diagnoseSearch('in:gen', { in: 'gen' }, ['in'])).toEqual({ problems: [], pending: true })
    expect(diagnoseSearch('from:', {}, [])).toEqual({ problems: [], pending: true })
  })

  it('blames only the filter of a kind that finally counts', () => {
    const d = diagnoseSearch('in:general in:nope ', { in: 'nope' }, ['in'])
    expect(d.problems).toEqual([{ kind: 'channel', filter: 'in', value: 'nope' }])
  })
})
