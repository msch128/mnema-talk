// Generate frontend schema types AND runtime guards from the checked-in Go API
// contract. A declaration alone cannot establish the shape of network JSON.
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

interface Schema {
  $ref?: string
  type?: string | string[]
  enum?: Array<string | boolean | number | null>
  required?: string[]
  properties?: Record<string, Schema>
  items?: Schema
  format?: string
  maxLength?: number
  minimum?: number
  maximum?: number
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

function isSchema(value: unknown): value is Schema {
  if (!isRecord(value)) return false
  return (value['$ref'] === undefined || typeof value['$ref'] === 'string')
    && (value['type'] === undefined || typeof value['type'] === 'string' || (Array.isArray(value['type']) && value['type'].every(item => typeof item === 'string')))
    && (value['enum'] === undefined || (Array.isArray(value['enum']) && value['enum'].every(item => item === null || ['string', 'boolean', 'number'].includes(typeof item))))
    && (value['required'] === undefined || (Array.isArray(value['required']) && value['required'].every(item => typeof item === 'string')))
    && (value['properties'] === undefined || (isRecord(value['properties']) && Object.values(value['properties']).every(isSchema)))
    && (value['items'] === undefined || isSchema(value['items']))
    && (value['format'] === undefined || typeof value['format'] === 'string')
    && (value['maxLength'] === undefined || typeof value['maxLength'] === 'number')
    && (value['minimum'] === undefined || typeof value['minimum'] === 'number')
    && (value['maximum'] === undefined || typeof value['maximum'] === 'number')
}

const specPath = new URL('../../api/openapi.json', import.meta.url)
const outputPath = new URL('../src/types/rest.ts', import.meta.url)
const document: unknown = JSON.parse(readFileSync(specPath, 'utf8'))
if (!isRecord(document) || !isRecord(document['components']) || !isRecord(document['components']['schemas'])) throw new Error('Invalid OpenAPI schema document')
const schemas: Record<string, Schema> = {}
for (const [name, value] of Object.entries(document['components']['schemas'])) {
  if (!isSchema(value)) throw new Error(`Invalid schema: ${name}`)
  schemas[name] = value
}
const identifier = (name: string): string => name.split('.').map(part => part.charAt(0).toUpperCase() + part.slice(1)).join('')
const referenced = (ref: string): string => {
  const name = ref.replace('#/components/schemas/', '')
  if (!Object.hasOwn(schemas, name)) throw new Error(`Unknown schema reference: ${ref}`)
  return identifier(name)
}

function typeOf(schema: Schema): string {
  if (schema.$ref) return referenced(schema.$ref)
  if (schema.enum) return schema.enum.map(value => JSON.stringify(value)).join(' | ')
  if (Array.isArray(schema.type)) return schema.type.map(type => typeOf({ ...schema, type })).join(' | ')
  switch (schema.type) {
    case 'null': return 'null'
    case 'integer':
    case 'number': return 'number'
    case 'string': return 'string'
    case 'boolean': return 'boolean'
    case 'array':
      if (!schema.items) throw new Error('Array is missing items')
      return `Array<${typeOf(schema.items)}>`
    case 'object': {
      const required = new Set(schema.required || [])
      return `{\n${Object.entries(schema.properties || {}).map(([key, property]) => `  ${key}${required.has(key) ? '' : '?'}: ${typeOf(property)}`).join('\n')}\n}`
    }
    default: throw new Error(`Unsupported schema: ${JSON.stringify(schema)}`)
  }
}

function guard(schema: Schema, expression: string): string {
  if (schema.$ref) return `is${referenced(schema.$ref)}(${expression})`
  if (schema.enum) return `(${schema.enum.map(value => `${expression} === ${JSON.stringify(value)}`).join(' || ')})`
  if (Array.isArray(schema.type)) return `(${schema.type.map(type => guard({ ...schema, type }, expression)).join(' || ')})`
  switch (schema.type) {
    case 'null': return `${expression} === null`
    case 'integer':
    case 'number': {
      const checks = [schema.type === 'integer' ? `isInteger(${expression})` : `(typeof ${expression} === 'number' && Number.isFinite(${expression}))`]
      if (schema.minimum !== undefined) checks.push(`${expression} >= ${schema.minimum}`)
      if (schema.maximum !== undefined) checks.push(`${expression} <= ${schema.maximum}`)
      return checks.length === 1 ? checks[0]! : `(${checks.join(' && ')})`
    }
    case 'boolean': return `typeof ${expression} === 'boolean'`
    case 'string': {
      if (schema.format === 'uuid') return `isIdentifier(${expression})`
      if (schema.format === 'date-time') return `isTimestamp(${expression})`
      return `isString(${expression}${schema.maxLength ? `, ${schema.maxLength}` : ''})`
    }
    case 'array':
      if (!schema.items) throw new Error('Array is missing items')
      return `isArrayOf(${expression}, item => ${guard(schema.items, 'item')})`
    case 'object': {
      const required = new Set(schema.required || [])
      return [`isRecord(${expression})`, ...Object.entries(schema.properties || {}).map(([key, property]) => {
        const field = `${expression}[${JSON.stringify(key)}]`
        const check = guard(property, field)
        return required.has(key) ? `(hasOwn(${expression}, ${JSON.stringify(key)}) && ${check})` : `(!hasOwn(${expression}, ${JSON.stringify(key)}) || ${check})`
      })].join('\n    && ')
    }
    default: throw new Error(`Unsupported guard: ${JSON.stringify(schema)}`)
  }
}

const lines = [
  '// GENERATED by web/scripts/generate-contracts.ts from api/openapi.json.',
  '// Do not edit this file; fix the Go/OpenAPI source and regenerate instead.',
  "import { isRecord, hasOwn, isString, isInteger, isIdentifier, isTimestamp, isArrayOf, decoder } from './validation'",
  ''
]
for (const [name, schema] of Object.entries(schemas)) {
  const id = identifier(name)
  lines.push(`export type ${id} = ${typeOf(schema)}`, '', `export function is${id}(value: unknown): value is ${id} {`, `  return ${guard(schema, 'value')}`, '}', '', `export const decode${id} = decoder(${JSON.stringify(name)}, is${id})`, '')
}
const result = lines.join('\n')
if (process.argv.includes('--check')) {
  if (readFileSync(outputPath, 'utf8') !== result) {
    process.stderr.write('Frontend REST contracts are stale. Run node web/scripts/generate-contracts.ts.\n')
    process.exitCode = 1
  }
} else {
  mkdirSync(fileURLToPath(new URL('../src/types', import.meta.url)), { recursive: true })
  writeFileSync(outputPath, result)
}
