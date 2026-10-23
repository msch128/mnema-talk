import { describe, it, expect } from 'vitest'
import { summarizeAudit } from './auditSummary'

const summarize = (details: Record<string, unknown>) => summarizeAudit(details, 'yes', 'no')

describe('the audit "what changed" line', () => {
  it('names each changed field with its old and new value', () => {
    expect(summarize({ changes: { name: { from: 'Mods', to: 'Moderators' }, hoist: { from: false, to: true }, max_upload_mb: { from: 10, to: null } } }))
      .toEqual({ kind: 'changes', text: 'name: Mods → Moderators, hoist: no → yes, max upload mb: 10 → —' })
  })

  it('cuts long texts and long lists so the line stays short', () => {
    const long = 'welcome '.repeat(30)
    const text = summarize({ changes: { welcome_text: { from: '', to: long }, a: { from: 1, to: 2 }, b: { from: 1, to: 2 }, c: { from: 1, to: 2 }, d: { from: 1, to: 2 } } })?.text ?? ''
    expect(text).toMatch(/^welcome text: — → welcome welcome welcome welcome welcome…, a: 1 → 2/)
    expect(text.endsWith(', …')).toBe(true)
    expect(text).not.toContain('d: 1')
  })

  it('shows the audited fields of something that was created', () => {
    expect(summarize({ after: { name: 'party', animated: false } })).toEqual({ kind: 'created', text: 'name: party, animated: no' })
  })

  it('stays quiet for an edit that changed nothing and for entries without fields', () => {
    expect(summarize({ before: { name: 'a' }, after: { name: 'a' } })).toBeNull()
    expect(summarize({ params: { id: '1' } })).toBeNull()
    expect(summarize({})).toBeNull()
  })
})
