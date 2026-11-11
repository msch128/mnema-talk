/** A parsed object, not an unchecked domain assertion. */
export function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

export function hasOwn(value: Record<string, unknown>, key: string): boolean {
  return Object.prototype.hasOwnProperty.call(value, key)
}

// Guards bound traversal; they do not impose an arbitrary 100-person cap.
const MAX_STRING_LENGTH = 1_048_576
const MAX_COLLECTION_LENGTH = 10_000

/** True when the object has no field outside the given list (payloads are spread into stores). */
export function onlyFields(value: Record<string, unknown>, fields: readonly string[]): boolean {
  return Object.keys(value).every(key => fields.includes(key))
}

export function isString(value: unknown, maxCodePoints?: number): value is string {
  if (typeof value !== 'string' || value.length > MAX_STRING_LENGTH) return false
  return maxCodePoints === undefined || [...value].length <= maxCodePoints
}

export function isInteger(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value)
}

export function isIdentifier(value: unknown): value is string {
  return typeof value === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value)
}

export function isTimestamp(value: unknown): value is string {
  return typeof value === 'string' && value.length <= 64
    && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(value)
    && Number.isFinite(Date.parse(value))
}

export function isArrayOf<T>(value: unknown, itemGuard: (item: unknown) => item is T): value is T[]
export function isArrayOf(value: unknown, itemGuard: (item: unknown) => boolean): value is unknown[]
export function isArrayOf(value: unknown, itemGuard: (item: unknown) => boolean): value is unknown[] {
  return Array.isArray(value) && value.length <= MAX_COLLECTION_LENGTH && value.every(item => itemGuard(item))
}

export function isDictionaryOf<T>(value: unknown, itemGuard: (item: unknown) => item is T): value is Record<string, T> {
  return isRecord(value) && Object.keys(value).length <= MAX_COLLECTION_LENGTH
    && Object.entries(value).every(([key, item]) => isIdentifier(key) && itemGuard(item))
}

/** Contains a schema name only: never dumps private response content. */
export class ContractError extends Error {
  constructor(readonly schema: string) {
    super(`Invalid server response (${schema})`) // i18n-ignore: schema name for diagnostics
    this.name = 'ContractError'
  }
}

export type Decoder<T> = (value: unknown) => T

type Normaliser = (value: unknown) => unknown
const itemNormalisers = new Map<unknown, Normaliser>()
const listNormalisers = new Map<unknown, Normaliser>()

/**
 * Answers of older servers lack fields the current contract requires; shared/lib/compat/olderServers.ts registers,
 * per guard, what fills them (before the guard runs). A list normaliser sees the whole list (it may need the
 * position of an item); without one, the item normaliser runs on each item.
 */
export function registerNormaliser(guard: unknown, item: Normaliser, list?: Normaliser): void {
  itemNormalisers.set(guard, item)
  if (list) listNormalisers.set(guard, list)
}

/** The value with the documented defaults of older servers filled in; the value itself when nothing is registered. */
export function normalise(guard: unknown, value: unknown): unknown {
  return itemNormalisers.get(guard)?.(value) ?? value
}

function normaliseList(guard: unknown, value: unknown): unknown {
  const list = listNormalisers.get(guard)
  if (list) return list(value)
  return Array.isArray(value) && itemNormalisers.has(guard) ? value.map(item => normalise(guard, item)) : value
}

export function decoder<T>(schema: string, guard: (value: unknown) => value is T): Decoder<T> {
  return value => {
    const filled = normalise(guard, value)
    if (!guard(filled)) throw new ContractError(schema)
    return filled
  }
}

export function arrayDecoder<T>(schema: string, guard: (value: unknown) => value is T): Decoder<T[]> {
  return value => {
    const filled = normaliseList(guard, value)
    if (!isArrayOf(filled, guard)) throw new ContractError(schema)
    return filled
  }
}

export function nullableDecoder<T>(decode: Decoder<T>): Decoder<T | null> {
  return value => value === null ? null : decode(value)
}
