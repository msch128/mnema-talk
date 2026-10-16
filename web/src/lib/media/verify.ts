// A short code every member of one key epoch can read out to the others.
// It comes from the MLS epoch authenticator (RFC 9420, section 8.7), which
// only members of that exact group state hold: the same code on both ends
// means both hold the same keys with the same members. A server that split
// the call or slipped in a different key shows up as a different code.

const DOMAIN = new TextEncoder().encode('mnema-safety-v1')
export const SAFETY_CODE_DIGITS = 20
const GROUP_DIGITS = 5
// 80 bits reduced to 20 digits: the bias is below 1 in 10 000.
const SOURCE_BYTES = 10

export interface MediaVerification {
  epoch: number
  /** Digits in groups of five, e.g. "01234 56789 01234 56789". */
  code: string
  /** User ids holding this epoch's keys, sorted. */
  members: string[]
}

export async function safetyCode(authenticator: Uint8Array): Promise<string | null> {
  if (authenticator.byteLength === 0) return null
  const input = new Uint8Array(DOMAIN.byteLength + authenticator.byteLength)
  input.set(DOMAIN)
  input.set(authenticator, DOMAIN.byteLength)
  const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', input))
  input.fill(0)
  let value = 0n
  for (const byte of digest.subarray(0, SOURCE_BYTES)) value = (value << 8n) | BigInt(byte)
  const digits = (value % 10n ** BigInt(SAFETY_CODE_DIGITS)).toString().padStart(SAFETY_CODE_DIGITS, '0')
  const groups: string[] = []
  for (let i = 0; i < SAFETY_CODE_DIGITS; i += GROUP_DIGITS) groups.push(digits.slice(i, i + GROUP_DIGITS))
  return groups.join(' ')
}
