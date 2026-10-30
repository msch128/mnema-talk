import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { errorMessage } from './api'
import { formatDetailMessage } from './errorDetails'

describe('server reasons reach the user in their language', () => {
  it.each([
    ['CONFLICT', 'a channel can keep at most 50 pinned messages; unpin one first', /50 angeheftete/],
    ['CONFLICT', 'the poll has ended', /Umfrage ist beendet/],
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
    ['INVALID_INPUT', 'Your edit was not saved: it breaks the server rule "Links" (automod). The moderators can see this.', /Änderung wurde nicht gespeichert.*„Links“/]
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

/** Plain string literals of httpx.Err*("...") calls in the files of the 1.0 features. */
const FILES = [
  'poll/input.go', 'poll/store.go', 'poll/handler.go',
  'emoji/store.go', 'emoji/handler.go', 'emoji/image.go',
  'push/store.go', 'push/handler.go', 'push/validate.go',
  'chat/pins.go', 'chat/message_types.go', 'chat/message_mutations.go',
  'auth/account_delete.go', 'auth/account_export.go', 'auth/email.go', 'auth/email_handler.go',
  'auth/two_factor_store.go', 'auth/two_factor_handler.go'
]

// Messages that the UI never shows as they are: developer-facing or covered by a generic code text.
const IGNORED = [
  /^moderation needs the permission service$/, /^edit history needs the permission service$/,
  /^user not found$/
]

function literals(file: string): string[] {
  const src = readFileSync(join(process.cwd(), '..', 'internal', file), 'utf8')
  const found: string[] = []
  for (const m of src.matchAll(/httpx\.Err(?:Conflict|Forbidden|Unavailable|InvalidInput|NotFound|UnsupportedMediaType)\(\s*"((?:[^"\\]|\\.)*)"\s*\)/g)) {
    found.push((m[1] ?? '').replace(/\\"/g, '"'))
  }
  return found
}

describe('every sentence the 1.0 features can answer with is localized', () => {
  const all = FILES.flatMap(file => literals(file).map(message => ({ file, message })))

  it('finds the sentences', () => {
    expect(all.length).toBeGreaterThan(25)
  })

  it.each(all)('$file: $message', ({ message }) => {
    if (IGNORED.some(pattern => pattern.test(message))) return
    expect(formatDetailMessage(message), `no localized text for "${message}"`).not.toBeNull()
  })
})
