import { beforeEach, describe, expect, it } from 'vitest'
import { setLocale } from '../i18n'
import { localizeHostError } from './hostError'

// The sentences the Rust and Electron desktop clients send. A change to one of
// them in the desktop repository needs the same change here.
const HINT =
  'If the server is in your own network, your router may not support reaching it through its public address (NAT loopback/hairpin); try from outside or use the local address.'
const SENT = [
  'Enter an HTTPS instance address.',
  'Invalid instance address.',
  'Use an HTTPS origin without credentials, a path or query.',
  'Unable to initialize HTTPS.',
  'The instance refused the connection. Check the address and the port.',
  'The instance did not answer in time. Check the address, the port and your network.',
  'The instance address could not be resolved. Check the address and your network.',
  "This computer does not trust the instance's HTTPS certificate. Check the system clock, and that no antivirus or network filter intercepts HTTPS.",
  'Unable to reach the instance using trusted HTTPS.',
  'The instance is unavailable or redirects to a different address.',
  'Instance check failed.',
  'Invalid instance response.',
  'This instance needs an update for the desktop client.',
  'This instance is not compatible with this desktop client.',
  'This client has no verified app. Reinstall the desktop client.',
  'This server needs a newer Mnema app than this client has. Update the desktop client.',
  'This server is too old for this desktop client (needs 0.9.0 or newer). Ask the administrator to update it.',
  'This server speaks an older contract than any app in this client. Ask the administrator to update it.',
  'The app in this client was signed with a key that is no longer trusted. Update the desktop client; it fetches a newly signed app at its next check.',
  'The newest verified app this client has used is no longer available, and older apps are refused. Update or reinstall the desktop client.',
  'This server is already in the list.',
  'At most 8 servers fit in one window.',
  'The web app of this server was withdrawn. Restart the client to get a current one.',
  'Several servers at once need macOS 14 or newer.',
  'Only the instance selector may open an instance.'
]

describe.each(['de', 'en'] as const)('host errors in %s', (locale) => {
  beforeEach(() => setLocale(locale))

  it('translates every sentence the desktop clients send', () => {
    for (const sent of SENT) {
      const text = localizeHostError(sent)
      if (locale === 'de') expect(text, sent).not.toBe(sent)
      expect(text, sent).not.toContain('nativeDesktop.')
      expect(text, sent).not.toMatch(/\{\w+\}/)
    }
  })

  it('adds the hint for a server in the own network to a refusal and a timeout', () => {
    for (const sent of [SENT[4], SENT[5]]) {
      const text = localizeHostError(`${sent} ${HINT}`)
      expect(text.startsWith(localizeHostError(sent))).toBe(true)
      expect(text.length).toBeGreaterThan(localizeHostError(sent).length + 40)
      expect(text).not.toContain('NAT loopback/hairpin);')
    }
  })

  it('keeps the system detail of a certificate refusal', () => {
    const text = localizeHostError(`${SENT[7]} (The certificate chain was issued by an untrusted authority. (0x800B0109))`)
    expect(text).toContain('0x800B0109')
    expect(text).toContain('(The certificate chain was issued')
  })

  it('shows what it does not know as sent', () => {
    expect(localizeHostError('Something new.')).toBe('Something new.')
  })
})

it('speaks German by default and English on request', () => {
  setLocale('de')
  expect(localizeHostError(SENT[4])).toBe('Der Server hat die Verbindung abgelehnt. Prüfe Adresse und Port.')
  expect(localizeHostError('At most 1 servers fit in one window.')).toBe('In einem Fenster haben höchstens 1 Server Platz.')
  setLocale('en')
  expect(localizeHostError(SENT[16])).toContain('needs 0.9.0 or newer')
})
