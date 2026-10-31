import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { errorMessage } from './api'
import { formatDetailMessage, matchSentence } from './errorDetails'
import { SENTENCES } from './errorSentences'

describe('server reasons reach the user in their language', () => {
  it.each([
    ['CONFLICT', 'a channel can keep at most 50 pinned messages; unpin one first', /50 angeheftete/],
    ['CONFLICT', 'the poll has ended', /Umfrage ist beendet/],
    ['RATE_LIMITED', 'too many requests: retry in 90 s', /etwas viel\. Versuche es in 90 s noch einmal/],
    ['CONFLICT', 'the limit of 100 custom emoji is reached', /100 eigenen Emojis/],
    ['CONFLICT', 'an account can use push on at most 10 devices; remove one first', /10 Geräten/],
    ['CONFLICT', 'an emoji with this name already exists', /Emoji mit diesem Namen/],
    ['FORBIDDEN', 'the Owner account cannot be deleted: ownership is not transferable, so the community would be left without an Owner', /Owner-Konto kann nicht gelöscht/],
    ['UNAVAILABLE', 'exports need object storage', /Dateispeicher/],
    ['NOT_FOUND', 'push notifications are turned off on this server', /Push-Benachrichtigungen sind/],
    ['INVALID_INPUT', 'this poll takes one answer', /nur eine Antwort/],
    ['INVALID_INPUT', 'options must differ', /verschieden/],
    ['INVALID_INPUT', 'an option is one line of text', /einzelne Textzeile/],
    ['INVALID_INPUT', 'a poll cannot be edited; delete it and ask again', /nicht bearbeiten/],
    ['INVALID_INPUT', 'name must be 2 to 32 characters: a-z, 0-9 and _', /2–32 Zeichen/],
    ['INVALID_INPUT', 'an emoji must be a PNG, GIF or WebP image', /PNG-, GIF- oder WebP-Bild/],
    ['INVALID_INPUT', 'only PNG, GIF and WebP images are accepted', /PNG-, GIF- oder WebP/],
    ['INVALID_INPUT', 'the image is larger than 256 KiB after processing', /256 KiB/],
    ['INVALID_INPUT', 'invalid emoji', /nicht verfügbar/],
    ['INVALID_INPUT', 'the file must be between 1 byte and 256 KiB', /256 KiB/],
    ['INVALID_INPUT', 'the image must be between 1 and 4096 pixels on each edge', /4096 Pixel/],
    ['INVALID_INPUT', 'endpoint must be an https URL of a push service on a public address', /Push-Abo/],
    ['INVALID_INPUT', 'p256dh must be a P-256 public key and auth a 16 byte secret, both base64url', /Push-Abo/],
    ['INVALID_INPUT', 'duration must be one of 1h, 1d', /Laufzeiten/],
    ['INVALID_INPUT', 'Your message was not sent: it breaks the community rule "No spam" (automod). The moderators can see this.', /Regel „No spam“ der Community/],
    ['INVALID_INPUT', 'Your edit was not saved: it breaks the community rule "Links" (automod). The moderators can see this.', /Änderung wurde nicht gespeichert.*„Links“/]
  ])('%s "%s"', (code, message, expected) => {
    expect(errorMessage(code, message)).toMatch(expected)
  })

  it('shows the generic text plus the server words for an unknown reason', () => {
    const text = errorMessage('CONFLICT', 'something brand new happened')
    expect(text).toContain('Das gibt es bereits')
    expect(text).toContain('something brand new happened')
    expect(errorMessage('UNAVAILABLE', 'brand new outage')).toContain('brand new outage')
  })

  it('keeps the plain generic text for stock-wording codes', () => {
    expect(errorMessage('NOT_FOUND', 'poll gone for unknown reasons')).not.toContain('unknown reasons')
  })
})

/** A message the server could send: the n-th placeholder is filled with 70+n so each one can be found again. */
function sample(format: string): { message: string; values: string[] } {
  const values: string[] = []
  const message = format.replace(/%[dsq]/g, (verb) => {
    const value = String(71 + values.length)
    values.push(value)
    return verb === '%q' ? `"${value}"` : value
  })
  return { message, values }
}

// Texts that leave the value out on purpose (a permission id, a list of durations, an English reason mean nothing to a member).
const WITHOUT_VALUE = new Set([
  'cannot change a permission you do not have: %s', 'cannot grant a permission you do not have: %s', 'missing permission: %s',
  'duration must be one of %s', 'password must be at most %d bytes', 'username must be at most %d characters',
  'the configured image tag cannot reach this release (%s); update manually'
])

describe('every sentence the server can answer with is localized by a row of its own', () => {
  const catalogue = JSON.parse(readFileSync(join(process.cwd(), '..', 'internal', 'repo', 'testdata', 'error_messages.golden.json'), 'utf8')) as string[]

  it('finds the sentences', () => {
    expect(catalogue.length).toBeGreaterThan(500)
  })

  it('has one row per sentence and no row for a sentence the server no longer sends', () => {
    const formats = SENTENCES.map(row => row[0])
    expect(new Set(formats).size, 'a sentence has two rows').toBe(formats.length)
    expect(formats.filter(format => !catalogue.includes(format)), 'rows without a server sentence').toEqual([])
    expect(catalogue.filter(format => !formats.includes(format)), 'server sentences without a row').toEqual([])
  })

  it.each(catalogue)('%s', (format) => {
    const { message, values } = sample(format)
    // The row must be this sentence's own, not a more general one that happens to match too.
    expect(matchSentence(message)?.row[0], `"${message}" is answered by another row`).toBe(format)
    const shown = formatDetailMessage(message)
    expect(shown).not.toMatch(/\{[a-z]+\}/)
    expect(shown).not.toBe(message)
    for (const value of WITHOUT_VALUE.has(format) ? [] : values) expect(shown, `the value ${value} is missing`).toContain(value)
  })

  it('does not translate half of a sentence it does not know', () => {
    for (const message of ['widget does not work', 'the widget is required', 'invalid widget id', 'a brand new sentence is too long', 'cannot do this', 'widget has no gadget']) {
      expect(formatDetailMessage(message), message).toBeNull()
    }
  })

  it('reads the varying parts of a sentence back out', () => {
    expect(formatDetailMessage('at most 12 favourites')).toBe('Höchstens 12 Favoriten.')
    expect(formatDetailMessage('sort_order must be between -1000 and 1000')).toContain('-1000')
    expect(formatDetailMessage('max_upload_mb must be between 1 and 500')).toContain('500')
  })

  it('shows English words the server fills in as translated words', () => {
    expect(formatDetailMessage('this event is cancelled and cannot be cancelled')).toContain('abgesagt')
    expect(formatDetailMessage('this event is active and takes no more responses')).toContain('live')
    expect(formatDetailMessage('the banner is larger than 8 MB after processing')).toContain('Banner')
    expect(formatDetailMessage('code is too easy to guess (too repetitive); leave it empty for a random code or use at least 8 hard-to-guess characters')).toContain('zu eintönig')
    expect(formatDetailMessage('code is too easy to guess (shorter than 6 characters); leave it empty for a random code or use at least 8 hard-to-guess characters')).toContain('kürzer als 6 Zeichen')
  })

  it('answers null for a sentence it does not know', () => {
    expect(formatDetailMessage('something brand new happened')).toBeNull()
  })
})
