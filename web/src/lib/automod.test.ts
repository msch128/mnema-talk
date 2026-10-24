import { beforeEach, describe, expect, it } from 'vitest'
import type { AutomodRule } from '../types/rest'
import { emptyForm, formFromRule, formProblem, inputFromForm, ruleSummary, toggled, withCurrent } from './automod'
import { setLocale } from '../i18n'

const rule = (extra: Partial<AutomodRule>): AutomodRule => ({
  id: '00000000-0000-4000-8000-000000000001', kind: 'words', name: 'Words', enabled: true, action: 'block', timeout_seconds: 0,
  config: {}, exempt_channels: [], exempt_roles: [], updated_at: '2026-01-01T00:00:00Z', ...extra
})

beforeEach(() => setLocale('en'))

describe('automod rule forms', () => {
  it('turns the lines of a list box into entries and back', () => {
    const form = formFromRule(rule({ config: { words: ['spam', 'buy now'] } }))
    expect(form.words).toBe('spam\nbuy now')
    expect(inputFromForm({ ...form, words: ' spam \n\n  cheat*\n' }).config).toEqual({ words: ['spam', 'cheat*'] })
  })

  it('sends only the settings of the rule kind', () => {
    const links = { ...emptyForm('links'), mode: 'block_all' as const, domains: 'a.com' }
    expect(inputFromForm(links).config).toEqual({ mode: 'block_all' })
    expect(inputFromForm({ ...links, mode: 'allow_list' }).config).toEqual({ mode: 'allow_list', domains: ['a.com'] })
    expect(inputFromForm(emptyForm('mentions')).config).toEqual({ max_mentions: 10 })
    expect(inputFromForm(emptyForm('new_account')).config).toEqual({ max_age_seconds: 600, restrict: 'links' })
  })

  it('lets raid protection only flag, whatever the form says', () => {
    const raid = { ...emptyForm('raid'), action: 'block' as const, timeoutSeconds: 600 }
    expect(inputFromForm(raid)).toMatchObject({ kind: 'raid', action: 'flag', timeout_seconds: 0, config: { joins: 10, window_seconds: 10, restrict_seconds: 600 } })
  })

  it('flips only the on/off state of a rule', () => {
    const r = rule({ kind: 'mentions', config: { max_mentions: 4 }, exempt_roles: ['r1'], timeout_seconds: 300 })
    expect(toggled(r)).toEqual({
      kind: 'mentions', name: 'Words', enabled: false, action: 'block', timeout_seconds: 300,
      config: { max_mentions: 4 }, exempt_roles: ['r1'], exempt_channels: []
    })
  })

  it('names what is missing before anything is sent', () => {
    expect(formProblem({ ...emptyForm('words'), name: '  ' })).toBe('Give the rule a name.')
    expect(formProblem({ ...emptyForm('mentions'), maxMentions: 0 })).toBe('Enter a number from 1 to 50.')
    expect(formProblem({ ...emptyForm('raid'), joins: 1 })).toBe('Enter a number from 2 to 200.')
    expect(formProblem({ ...emptyForm('words'), words: Array.from({ length: 201 }, (_, i) => `w${i}`).join('\n') })).toBe('At most 200 words.')
    expect(formProblem(emptyForm('words'))).toBe('')
  })

  it('keeps a value outside the ready-made choices selectable', () => {
    expect(withCurrent([60, 300], 300)).toEqual([60, 300])
    expect(withCurrent([60, 300], 120)).toEqual([60, 120, 300])
  })
})

describe('automod rule summaries', () => {
  it('says what each kind does', () => {
    expect(ruleSummary(rule({ config: { words: ['a', 'b'] } }))).toBe('Words and phrases: 2')
    expect(ruleSummary(rule({ kind: 'links', config: { mode: 'block_all' } }))).toBe('Every link')
    expect(ruleSummary(rule({ kind: 'links', config: { mode: 'allow_list', domains: ['a.com'] } }))).toBe('Only links to 1 listed domains')
    expect(ruleSummary(rule({ kind: 'mentions', config: { max_mentions: 10 } }))).toBe('More than 10 different mentions')
    expect(ruleSummary(rule({ kind: 'new_account', config: { max_age_seconds: 600, restrict: 'links' } }))).toBe('Accounts under 10 min may not post links')
    expect(ruleSummary(rule({ kind: 'raid', config: { joins: 10, window_seconds: 10, restrict_seconds: 600 } }))).toBe('More than 10 joins within 10 s: newcomers restricted for 10 min')
  })
})
