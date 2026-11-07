// What an older server release promised, read from its committed baseline
// (scripts/compat-baseline.ts): finding an operation, checking a request body
// against its schema and building sample answers. Test support only.
import type { CompatBaseline, CompatOperation, CompatSchema } from './types'

export type { CompatBaseline, CompatOperation, CompatSchema }

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
const SAMPLE_UUID = '00000000-0000-4000-8000-000000000001'
const SAMPLE_TIME = '2026-01-01T00:00:00Z'
const MAX_DEPTH = 4

/** The operation a request reaches on that release: a literal segment wins over a {parameter}; a non-UUID in a UUID parameter is no match (the server answers 400). */
export function findOperation(baseline: CompatBaseline, method: string, path: string): { key: string, operation: CompatOperation } | undefined {
  const segments = path.split('?')[0]!.split('/')
  let best: { key: string, literals: number } | undefined
  for (const key of Object.keys(baseline.operations)) {
    const [m, template] = key.split(' ') as [string, string]
    if (m !== method.toUpperCase()) continue
    const parts = template.split('/')
    if (parts.length !== segments.length) continue
    let literals = 0
    const uuid = baseline.operations[key]!.uuid
    const fits = parts.every((part, i) => {
      if (!part.startsWith('{')) {
        if (part === segments[i]) literals++
        return part === segments[i]
      }
      return !uuid.includes(part.slice(1, -1)) || UUID.test(segments[i]!)
    })
    if (fits && (!best || literals > best.literals)) best = { key, literals }
  }
  return best && { key: best.key, operation: baseline.operations[best.key]! }
}

function resolve(baseline: CompatBaseline, schema: CompatSchema | string | undefined): CompatSchema {
  if (typeof schema === 'string') return baseline.schemas[schema] ?? {}
  if (!schema) return {}
  const { $ref, ...own } = schema
  return $ref ? { ...resolve(baseline, $ref), ...own } : schema
}

function typesOf(schema: CompatSchema): string[] {
  return schema.type === undefined ? [] : Array.isArray(schema.type) ? schema.type : [schema.type]
}

function kindOf(value: unknown): string {
  if (value === null) return 'null'
  if (Array.isArray(value)) return 'array'
  if (Number.isInteger(value)) return 'integer'
  return typeof value
}

function typeFits(types: string[], value: unknown): boolean {
  const kind = kindOf(value)
  return types.length === 0 || types.includes(kind) || (kind === 'integer' && types.includes('number'))
}

/** Where a body breaks the rules of that release: unknown keys at any depth (the server rejects unknown JSON fields), missing required keys, wrong types. Each entry reads "path: problem". */
export function bodyViolations(baseline: CompatBaseline, schema: CompatSchema | string, body: unknown, at = typeof schema === 'string' ? schema : 'body'): string[] {
  const resolved = resolve(baseline, schema)
  const types = typesOf(resolved)
  if (!typeFits(types, body)) return [`${at}: expected ${types.join('|')}, got ${kindOf(body)}`]
  if (Array.isArray(body)) return body.flatMap((item, i) => bodyViolations(baseline, resolved.items ?? {}, item, `${at}[${i}]`))
  if (body === null || typeof body !== 'object') return enumViolation(resolved, body, at)
  return objectViolations(baseline, resolved, body as Record<string, unknown>, at)
}

function enumViolation(schema: CompatSchema, value: unknown, at: string): string[] {
  return schema.enum && !schema.enum.includes(value) ? [`${at}: ${JSON.stringify(value)} is not one of ${JSON.stringify(schema.enum)}`] : []
}

function objectViolations(baseline: CompatBaseline, schema: CompatSchema, body: Record<string, unknown>, at: string): string[] {
  const out: string[] = []
  const known = schema.properties ?? {}
  const open = schema.additionalProperties
  for (const [key, value] of Object.entries(body)) {
    if (value === undefined) continue
    const property = known[key]
    if (property) out.push(...bodyViolations(baseline, property, value, `${at}.${key}`))
    else if (open && typeof open === 'object') out.push(...bodyViolations(baseline, open, value, `${at}.${key}`))
    else if (open !== true && schema.properties) out.push(`${at}.${key}: unknown field`)
  }
  for (const key of schema.required ?? []) {
    if (body[key] === undefined) out.push(`${at}.${key}: required field is missing`)
  }
  return out
}

/** A sample value for a schema: 'minimal' has the required keys only (null where allowed), 'full' every key with one list item (below the depth cap only the required keys, so recursive schemas end). */
export function synthesize(baseline: CompatBaseline, schema: CompatSchema | string, mode: 'minimal' | 'full', depth = 0): unknown {
  const resolved = resolve(baseline, schema)
  const types = typesOf(resolved)
  if (resolved.enum?.length) return resolved.enum[0]
  if (mode === 'minimal' && types.includes('null') && depth > 0) return null
  const type = types.find(t => t !== 'null') ?? (resolved.properties ? 'object' : undefined)
  const here = mode === 'full' && depth >= MAX_DEPTH ? 'minimal' : mode
  switch (type) {
    case 'string': return sampleString(resolved)
    case 'integer': case 'number': return sampleNumber(resolved)
    case 'boolean': return false
    case 'array': return sampleArray(baseline, resolved, here, depth)
    case 'object': return sampleObject(baseline, resolved, here, depth)
    default: return depth === 0 ? {} : null
  }
}

function sampleString(schema: CompatSchema): string {
  if (schema.format === 'uuid') return SAMPLE_UUID
  if (schema.format === 'date-time') return SAMPLE_TIME
  if (schema.format === 'date') return '2026-01-01'
  return 'x'.repeat(Math.min(Math.max(schema.minLength ?? 1, 1), schema.maxLength ?? Infinity))
}

function sampleNumber(schema: CompatSchema): number {
  return Math.min(Math.max(0, schema.minimum ?? 0), schema.maximum ?? Infinity)
}

function sampleArray(baseline: CompatBaseline, schema: CompatSchema, mode: 'minimal' | 'full', depth: number): unknown[] {
  const count = Math.max(schema.minItems ?? 0, mode === 'full' ? 1 : 0)
  return Array.from({ length: count }, () => synthesize(baseline, schema.items ?? {}, mode, depth + 1))
}

function sampleObject(baseline: CompatBaseline, schema: CompatSchema, mode: 'minimal' | 'full', depth: number): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  const keys = mode === 'full' ? Object.keys(schema.properties ?? {}) : (schema.required ?? [])
  for (const key of keys) out[key] = synthesize(baseline, schema.properties?.[key] ?? {}, mode, depth + 1)
  return out
}
