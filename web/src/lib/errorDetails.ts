// The server's error sentences (English, stable) mapped to the localized text
// the user should see. Every sentence has its own row in errorSentences.ts;
// one that is not listed there falls back to the generic text of its error
// code plus the server message (see api.ts).
import { t } from '../i18n'
import { SENTENCES, type SentenceRow } from './errorSentences'
import { durationLabel } from './moderation'

// A field name the server echoes (`bio`, `option description`) shown as its label.
function fieldLabel(field: string): string {
  const key = `errors.field.${field.replace(/ /g, '_')}`
  const label = t(key)
  return label === key ? field : label
}

// Values the server fills in as English words (an event status, "avatar", why an invite code is weak).
const WORDS: Readonly<Record<string, string>> = {
  'too repetitive': 'repetitive', 'needs letters and digits, or 16 or more characters': 'lettersDigits',
  'contains a common word or sequence': 'commonWord', avatar: 'avatar', banner: 'banner',
  scheduled: 'scheduled', active: 'active', completed: 'completed', cancelled: 'cancelled'
}
const SHORTER = /^shorter than (\d+) characters$/

function word(value: string): string {
  if (Object.prototype.hasOwnProperty.call(WORDS, value)) return t(`errors.word.${WORDS[value]}`)
  const shorter = SHORTER.exec(value)
  return shorter ? t('errors.word.shorter', { a: shorter[1] }) : value
}

const DEFAULT_NAMES = ['a', 'b', 'c', 'd']
const LABELLED = new Set(['field', 'what'])

function param(name: string, value: string): string {
  if (name === 'time') return durationLabel(Number(value))
  return LABELLED.has(name) ? fieldLabel(value) : word(value)
}

// A varying part at the start of a sentence, or after "invalid" or "option", is
// a name: one token, so prose cannot be swallowed by it.
function wildcard(before: string): string {
  return before === '' || /(?:^|\s)(?:invalid|option)\s$/.test(before) ? '(\\S+)' : '(.+?)'
}

interface Compiled { row: SentenceRow; pattern: RegExp; literal: number; quoted: number[] }

// A format such as "at most %d favourites" becomes a regex that reads the
// varying parts back out of the finished sentence, anchored at both ends.
function compile(row: SentenceRow): Compiled {
  let before = ''
  const quoted: number[] = []
  let group = 0
  const source = row[0].split(/(%[dsq])/).map(part => {
    let piece: string
    if (part === '%d') piece = '(-?\\d+)'
    else if (part === '%q') piece = '"(.*)"'
    else if (part === '%s') piece = wildcard(before)
    else piece = part.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    if (/^%[dsq]$/.test(part)) { if (part === '%q') quoted.push(group); group++ }
    before += part
    return piece
  }).join('')
  return { row, pattern: new RegExp(`^${source}$`), literal: row[0].replace(/%[dsq]/g, '').length, quoted }
}

const EXACT = new Map(SENTENCES.filter(row => !row[0].includes('%')).map(row => [row[0], row]))
// The most specific format (the most fixed text) wins.
const FORMATS = SENTENCES.filter(row => row[0].includes('%')).map(compile).sort((a, b) => b.literal - a.literal)

function render(row: SentenceRow, values: readonly string[]): string {
  const names = row[2] ?? DEFAULT_NAMES
  const params: Record<string, string> = {}
  for (const [name, value] of Object.entries(row[3] ?? {})) params[name] = param(name, value)
  values.forEach((value, i) => { params[names[i] ?? `p${i}`] = param(names[i] ?? '', value) })
  return t(`errors.detail.${row[1]}`, params)
}

// Go's %q writes a name with backslash escapes (a quote in it becomes \").
function unquote(escaped: string): string {
  try {
    return JSON.parse(`"${escaped}"`) as string
  } catch {
    return escaped
  }
}

/** The row that answers a server message, with the parts of the message that vary. */
export function matchSentence(message: string): { row: SentenceRow; values: string[] } | null {
  const exact = EXACT.get(message)
  if (exact) return { row: exact, values: [] }
  for (const { row, pattern, quoted } of FORMATS) {
    const match = message.match(pattern)
    if (match) return { row, values: match.slice(1).map((value, i) => quoted.includes(i) ? unquote(value) : value) }
  }
  return null
}

/** The localized sentence for a known server message, or null. */
export function formatDetailMessage(message: string): string | null {
  const found = matchSentence(message)
  return found ? render(found.row, found.values) : null
}
