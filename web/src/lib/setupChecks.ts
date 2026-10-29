// Setup check text for the admin status tab. The server sends a stable code ("turn.none") and its
// parameters next to the English sentence; the sentence is the fallback for codes this app does not know.
import { t } from '../i18n'
import type { SetupcheckCheck } from '../types/rest'

export interface CheckText {
  detail: string
  fix: string
}

// t() answers with the key itself for a message nobody wrote.
function translated(key: string, params: Record<string, string>): string | undefined {
  const text = t(key, params)
  return text === key ? undefined : text
}

export function describeCheck(c: SetupcheckCheck): CheckText {
  const base = c.code ? `admin.operatorStatus.checkMsg.${c.code}` : ''
  const params = Object.fromEntries(Object.entries(c.params ?? {}).map(([k, v]) => [k, String(v)]))
  const detail = base ? translated(`${base}.detail`, params) : undefined
  if (detail === undefined) return { detail: c.detail, fix: c.fix ?? '' }
  return { detail, fix: c.fix ? (translated(`${base}.fix`, params) ?? c.fix) : '' }
}
