import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { SAFETY_CODE_DIGITS, safetyCode } from './verify'

const previous = globalThis.crypto
beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

describe('safety code', () => {
  it('is twenty digits in groups of five, the same for the same authenticator', async () => {
    const authenticator = new Uint8Array(32).fill(7)
    const code = await safetyCode(authenticator)
    expect(code).toMatch(/^\d{5} \d{5} \d{5} \d{5}$/)
    expect(code?.replace(/ /g, '')).toHaveLength(SAFETY_CODE_DIGITS)
    expect(await safetyCode(new Uint8Array(32).fill(7))).toBe(code)
    expect(authenticator).toEqual(new Uint8Array(32).fill(7))
  })

  it('differs when a single bit of the authenticator differs', async () => {
    const flipped = new Uint8Array(32).fill(7)
    flipped[31] = 6
    expect(await safetyCode(flipped)).not.toBe(await safetyCode(new Uint8Array(32).fill(7)))
  })

  it('keeps leading zeros and pins the derivation', async () => {
    // SHA-256("mnema-safety-v1" || 32 zero bytes), first 80 bits mod 10^20.
    const expected = await (async () => {
      const input = new Uint8Array(15 + 32)
      input.set(new TextEncoder().encode('mnema-safety-v1'))
      const digest = new Uint8Array(await webcrypto.subtle.digest('SHA-256', input))
      let value = 0n
      for (const byte of digest.subarray(0, 10)) value = (value << 8n) | BigInt(byte)
      return (value % 10n ** 20n).toString().padStart(20, '0').replace(/(\d{5})(?=\d)/g, '$1 ')
    })()
    expect(await safetyCode(new Uint8Array(32))).toBe(expected)
  })

  it('has no code without an authenticator', async () => {
    expect(await safetyCode(new Uint8Array())).toBeNull()
  })
})
