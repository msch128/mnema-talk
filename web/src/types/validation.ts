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
    super(`Invalid server response (${schema})`)
    this.name = 'ContractError'
  }
}

export type Decoder<T> = (value: unknown) => T

export function decoder<T>(schema: string, guard: (value: unknown) => value is T): Decoder<T> {
  return value => {
    if (!guard(value)) throw new ContractError(schema)
    return value
  }
}

export function arrayDecoder<T>(schema: string, guard: (value: unknown) => value is T): Decoder<T[]> {
  return decoder(schema, (value): value is T[] => isArrayOf(value, guard))
}

export function nullableDecoder<T>(decode: Decoder<T>): Decoder<T | null> {
  return value => value === null ? null : decode(value)
}
