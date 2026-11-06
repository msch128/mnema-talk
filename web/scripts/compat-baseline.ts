// Extracts what an older server release promised: its API contract, the
// features its /api/health lists and the WebSocket events it sends. The result
// is committed under src/compat/baselines so the compatibility tests can hold
// the current web app against every release it still has to work with
// (doc/api-stability.md).
//
//   node scripts/compat-baseline.ts v1.0.0 v2.0.0 v2.9.12 v2.9.18
import { execFileSync } from 'node:child_process'
import { mkdirSync, writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

export interface CompatSchema {
  $ref?: string
  type?: string | string[]
  format?: string
  enum?: unknown[]
  minLength?: number
  maxLength?: number
  minimum?: number
  maximum?: number
  minItems?: number
  maxItems?: number
  pattern?: string
  items?: CompatSchema
  properties?: Record<string, CompatSchema>
  required?: string[]
  additionalProperties?: boolean | CompatSchema
}

export interface CompatOperation {
  /** The JSON request body, if the operation takes one. */
  body?: CompatSchema
  query: string[]
  /** Path parameters that must be UUIDs; the server answers 400 to anything else. */
  uuid: string[]
  /** The JSON answer of the success status, if it has one. */
  ok?: CompatSchema
  okStatus: number
}

export interface CompatBaseline {
  version: string
  web_client_api: number
  health: { status: string, version: string, web_client_api: number, community_name: string, features?: string[] }
  operations: Record<string, CompatOperation>
  schemas: Record<string, CompatSchema>
  eventTypes: string[]
}

const root = fileURLToPath(new URL('../../', import.meta.url))
const outDir = fileURLToPath(new URL('../src/compat/baselines/', import.meta.url))

function git(...args: string[]): string {
  return execFileSync('git', args, { cwd: root, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 })
}

export function tagExists(tag: string): boolean {
  try {
    git('rev-parse', '--verify', '--quiet', `${tag}^{commit}`)
    return true
  } catch {
    return false
  }
}

type Json = Record<string, unknown>
const isObject = (v: unknown): v is Json => v !== null && typeof v === 'object' && !Array.isArray(v)

/** Keeps what decides whether a request or answer fits; drops prose and examples. */
export function compact(raw: unknown): CompatSchema {
  if (!isObject(raw)) return {}
  const out: CompatSchema = {}
  if (typeof raw['$ref'] === 'string') out.$ref = raw['$ref'].replace('#/components/schemas/', '')
  if (typeof raw['type'] === 'string' || Array.isArray(raw['type'])) out.type = raw['type'] as string | string[]
  if (typeof raw['format'] === 'string') out.format = raw['format']
  if (Array.isArray(raw['enum'])) out.enum = raw['enum']
  for (const key of ['minLength', 'maxLength', 'minimum', 'maximum', 'minItems', 'maxItems'] as const) {
    if (typeof raw[key] === 'number') out[key] = raw[key]
  }
  if (typeof raw['pattern'] === 'string') out.pattern = raw['pattern']
  if (isObject(raw['items'])) out.items = compact(raw['items'])
  if (isObject(raw['properties'])) {
    out.properties = Object.fromEntries(Object.entries(raw['properties']).map(([k, v]) => [k, compact(v)]))
  }
  if (Array.isArray(raw['required'])) out.required = [...(raw['required'] as string[])].sort()
  const extra = raw['additionalProperties']
  if (typeof extra === 'boolean') out.additionalProperties = extra
  else if (isObject(extra)) out.additionalProperties = compact(extra)
  const parts = raw['allOf']
  if (Array.isArray(parts)) flatten(out, parts)
  return out
}

/** allOf is merged into the schema itself: properties and required add up. */
function flatten(into: CompatSchema, parts: unknown[]) {
  for (const part of parts) {
    const piece = compact(part)
    if (piece.$ref && !piece.properties) {
      into.$ref ??= piece.$ref
      continue
    }
    into.properties = { ...piece.properties, ...into.properties }
    into.required = [...new Set([...(into.required ?? []), ...(piece.required ?? [])])].sort()
    into.type ??= piece.type
  }
}

function jsonSchema(content: unknown): CompatSchema | undefined {
  if (!isObject(content)) return undefined
  const media = content['application/json']
  return isObject(media) && isObject(media['schema']) ? compact(media['schema']) : undefined
}

function operation(op: Json, shared: unknown[]): CompatOperation {
  const params = [...shared, ...(Array.isArray(op['parameters']) ? op['parameters'] : [])]
  const query = params.filter(isObject).filter(p => p['in'] === 'query').map(p => String(p['name'])).sort()
  const uuid = params.filter(isObject).filter(p => p['in'] === 'path' && isObject(p['schema']) && p['schema']['format'] === 'uuid').map(p => String(p['name'])).sort()
  const result: CompatOperation = { query, uuid, okStatus: 200 }
  const body = isObject(op['requestBody']) ? jsonSchema(op['requestBody']['content']) : undefined
  if (body) result.body = body
  const responses = isObject(op['responses']) ? op['responses'] : {}
  const code = Object.keys(responses).filter(c => c.startsWith('2')).sort()[0]
  if (code) {
    result.okStatus = Number(code)
    const response = responses[code]
    const ok = isObject(response) ? jsonSchema(response['content']) : undefined
    if (ok) result.ok = ok
  }
  return result
}

/** The string literals between `marker` and the closing brace/bracket. */
function literals(source: string, marker: RegExp): string[] {
  const start = marker.exec(source)
  if (!start) return []
  const rest = source.slice(start.index + start[0].length)
  const end = rest.search(/^\s*[}\]]/m)
  return [...rest.slice(0, end < 0 ? undefined : end).matchAll(/"([^"]+)"|'([^']+)'/g)].map(m => m[1] ?? m[2] ?? '')
}

function healthFeatures(source: string): string[] | undefined {
  return /var Features = \[\]string\{/.test(source) ? literals(source, /var Features = \[\]string\{/) : undefined
}

export function extractBaseline(tag: string): CompatBaseline {
  const doc = JSON.parse(git('show', `${tag}:api/openapi.json`)) as Json
  const healthSource = git('show', `${tag}:internal/server/health.go`)
  const events = git('show', `${tag}:web/src/types/events.ts`)
  const webClientApi = Number(/const WebClientAPI = (\d+)/.exec(healthSource)?.[1] ?? 1)
  const features = healthFeatures(healthSource)
  const operations: Record<string, CompatOperation> = {}
  const paths = isObject(doc['paths']) ? doc['paths'] : {}
  for (const [path, item] of Object.entries(paths)) {
    if (!isObject(item)) continue
    const shared = Array.isArray(item['parameters']) ? item['parameters'] : []
    for (const [method, op] of Object.entries(item)) {
      if (method !== 'parameters' && isObject(op)) operations[`${method.toUpperCase()} ${path}`] = operation(op, shared)
    }
  }
  const components = isObject(doc['components']) && isObject(doc['components']['schemas']) ? doc['components']['schemas'] : {}
  const schemas = Object.fromEntries(Object.entries(components).sort(([a], [b]) => a.localeCompare(b)).map(([name, s]) => [name, compact(s)]))
  return {
    version: tag,
    web_client_api: webClientApi,
    health: { status: 'ok', version: tag.replace(/^v/, ''), web_client_api: webClientApi, community_name: 'Compat', ...(features ? { features } : {}) },
    operations: Object.fromEntries(Object.entries(operations).sort(([a], [b]) => a.localeCompare(b))),
    schemas,
    eventTypes: literals(events, /const SERVER_EVENT_TYPES = new Set<string>\(\[/).sort()
  }
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  mkdirSync(outDir, { recursive: true })
  for (const tag of process.argv.slice(2)) {
    writeFileSync(`${outDir}${tag}.json`, JSON.stringify(extractBaseline(tag)) + '\n')
    console.log(`wrote src/compat/baselines/${tag}.json`)
  }
}
