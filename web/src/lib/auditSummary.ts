// A short "what changed" line for an audit entry, read from the details the
// server keeps: `changes` ({ field: { from, to } }) for an edit, `after` (the
// audited fields) for a creation. Long texts are cut so the line stays one
// line; the full JSON stays below it.

export interface AuditSummary { kind: 'changes' | 'created'; text: string }

const MAX_FIELDS = 4
const MAX_VALUE = 40

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}

function cut(text: string): string {
  return text.length > MAX_VALUE ? `${text.slice(0, MAX_VALUE - 1)}…` : text
}

/** One value as short text; yes and no are the caller's words. */
export function shortValue(value: unknown, yes: string, no: string): string {
  if (value === null || value === undefined || value === '') return '—'
  if (typeof value === 'boolean') return value ? yes : no
  if (typeof value === 'string') return cut(value.replace(/\s+/g, ' ').trim())
  if (typeof value === 'number') return String(value)
  return cut(JSON.stringify(value))
}

function fieldName(key: string): string {
  return key.replace(/_/g, ' ')
}

function list(parts: string[], total: number): string {
  return parts.join(', ') + (total > parts.length ? ', …' : '')
}

/** The summary, or null when the entry carries no field to name. */
export function summarizeAudit(details: Record<string, unknown>, yes: string, no: string): AuditSummary | null {
  const changes = details['changes']
  if (isRecord(changes) && Object.keys(changes).length > 0) {
    const keys = Object.keys(changes)
    const parts = keys.slice(0, MAX_FIELDS).map(key => {
      const change = changes[key]
      const from = isRecord(change) ? change['from'] : undefined
      const to = isRecord(change) ? change['to'] : undefined
      return `${fieldName(key)}: ${shortValue(from, yes, no)} → ${shortValue(to, yes, no)}`
    })
    return { kind: 'changes', text: list(parts, keys.length) }
  }
  const after = details['after']
  if (isRecord(after) && !('before' in details) && Object.keys(after).length > 0) {
    const keys = Object.keys(after)
    const parts = keys.slice(0, MAX_FIELDS).map(key => `${fieldName(key)}: ${shortValue(after[key], yes, no)}`)
    return { kind: 'created', text: list(parts, keys.length) }
  }
  return null
}
