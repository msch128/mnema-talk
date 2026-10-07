import { describe, expect, it } from 'vitest'
import { arrayDecoder, ContractError, decoder, hasOwn, isArrayOf, isDictionaryOf, isIdentifier, isInteger, isRecord, isString, isTimestamp, nullableDecoder } from './validation'
import { fixtureId, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'

describe('untrusted JSON validation primitives', () => {
  it('distinguishes records and own fields from arrays, null and inherited values', () => {
    expect(isRecord({})).toBe(true)
    for (const value of [null, undefined, [], false, 1, 'value']) expect(isRecord(value)).toBe(false)
    expect(hasOwn({ name: null }, 'name')).toBe(true)
    expect(hasOwn({}, 'toString')).toBe(false)
  })
  it('bounds strings by code points and raw transport length', () => {
    expect(isString('😀', 1)).toBe(true)
    expect(isString('😀😀', 1)).toBe(false)
    expect(isString('', 0)).toBe(true)
    expect(isString('synthetic')).toBe(true)
    expect(isString('x'.repeat(1048576))).toBe(true)
    expect(isString('x'.repeat(1048577))).toBe(false)
    expect(isString(1)).toBe(false)
  })
  it('requires safe integers, UUIDs and valid ISO datetimes', () => {
    for (const number of [0, -1, Number.MAX_SAFE_INTEGER]) expect(isInteger(number)).toBe(true)
    for (const number of [1.5, NaN, Infinity, Number.MAX_SAFE_INTEGER + 1, '1', null]) expect(isInteger(number)).toBe(false)
    expect(isIdentifier(fixtureId(1).toUpperCase())).toBe(true)
    for (const id of ['a', null, 1]) expect(isIdentifier(id)).toBe(false)
    for (const date of [FIXTURE_TIMESTAMP, '2026-10-07T18:00:00.000+02:00']) expect(isTimestamp(date)).toBe(true)
    for (const date of [null, 1, 'x'.repeat(65), '2026-10-07', '2026-99-99T99:99:99Z']) expect(isTimestamp(date)).toBe(false)
  })
  it('bounds array and dictionary traversal and validates every nested value/key', () => {
    expect(isArrayOf([], isString)).toBe(true)
    expect(isArrayOf(['x'], isString)).toBe(true)
    expect(isArrayOf([1], isString)).toBe(false)
    expect(isArrayOf({}, isString)).toBe(false)
    expect(isArrayOf(Array.from({ length: 10000 }, () => 'x'), isString)).toBe(true)
    expect(isArrayOf(Array.from({ length: 10001 }, () => 'x'), isString)).toBe(false)
    expect(isDictionaryOf({}, isString)).toBe(true)
    expect(isDictionaryOf({ [fixtureId(1)]: 'x' }, isString)).toBe(true)
    expect(isDictionaryOf({ [fixtureId(1)]: 1 }, isString)).toBe(false)
    expect(isDictionaryOf({ invalid: 'x' }, isString)).toBe(false)
    expect(isDictionaryOf([], isString)).toBe(false)
    const large = Object.fromEntries(Array.from({ length: 10001 }, (_, i) => [fixtureId(i + 1), 'x']))
    expect(isDictionaryOf(large, isString)).toBe(false)
  })
  it('returns the original decoded values, supports nullable arrays, and never includes response content in errors', () => {
    const decode = decoder('Synthetic', isString)
    expect(decode('value')).toBe('value')
    const list = ['value']
    expect(arrayDecoder('Synthetic[]', isString)(list)).toBe(list)
    expect(nullableDecoder(decode)(null)).toBeNull()
    expect(nullableDecoder(decode)('value')).toBe('value')
    const marker = { private: 'SYNTHETIC_PRIVATE_PAYLOAD' }
    try {
      decode(marker)
      throw new Error('Expected contract rejection')
    } catch (error: unknown) {
      expect(error).toBeInstanceOf(ContractError)
      if (!(error instanceof ContractError)) throw error
      expect(error.name).toBe('ContractError')
      expect(error.schema).toBe('Synthetic')
      expect(error.message).toBe('Invalid server response (Synthetic)')
      expect(error.message).not.toContain(marker.private)
    }
  })
})
