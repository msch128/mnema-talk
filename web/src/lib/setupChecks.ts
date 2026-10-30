// Setup check text for the admin status tab. The server sends a stable code ("turn.none") and its
// parameters next to the English sentence; the sentence is the fallback for codes this app does not know.
import { t, locale } from '../i18n'
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

// The server names a moment twice: "at" (ISO) and "time" (UTC, for the English fallback). The app shows it in the viewer's zone and language.
function localizedTime(iso: string | undefined): string | undefined {
  const d = iso ? new Date(iso) : null
  if (!d || Number.isNaN(d.getTime())) return undefined
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(d)
}

export function describeCheck(c: SetupcheckCheck): CheckText {
  const base = c.code ? `admin.operatorStatus.checkMsg.${c.code}` : ''
  const params: Record<string, string> = Object.fromEntries(Object.entries(c.params ?? {}).map(([k, v]) => [k, String(v)]))
  const when = localizedTime(params.at)
  if (when) params.time = when
  const detail = base ? translated(`${base}.detail`, params) : undefined
  if (detail === undefined) return { detail: c.detail, fix: c.fix ?? '' }
  return { detail, fix: c.fix ? (translated(`${base}.fix`, params) ?? c.fix) : '' }
}
