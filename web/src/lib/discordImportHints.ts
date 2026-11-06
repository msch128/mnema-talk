// The server sends every import note and skip as a code with raw params next
// to its English text. Known codes are translated here; an unknown code (a
// newer server) shows the server's text.
import type { DiscordimportHint } from '../types/rest'
import { t } from '../i18n'

export const IMPORT_NOTE_CODES = [
  'announcement_to_text', 'stage_to_talk', 'forum_to_text', 'topic_shortened',
  'slow_mode_capped', 'age_restriction_not_imported'
] as const
export const IMPORT_SKIP_CODES = ['channel_type_unsupported'] as const

type Kind = 'note' | 'skipped'

export function importHintText(kind: Kind, hint: DiscordimportHint | undefined, fallback: string): string {
  const known: readonly string[] = kind === 'note' ? IMPORT_NOTE_CODES : IMPORT_SKIP_CODES
  if (!hint || !known.includes(hint.code)) return fallback
  const params: Record<string, string | number> = {}
  for (const [k, v] of Object.entries(hint.params)) {
    if (typeof v === 'string' || typeof v === 'number') params[k] = v
  }
  return t(`discordImport.${kind}.${hint.code}`, params)
}
