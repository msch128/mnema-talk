import { describe, expect, it } from 'vitest'
import openapi from '../../../api/openapi.json'
import * as contracts from './rest'
import { ContractError } from './validation'
import { fixtureId, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'

interface Schema {
  $ref?: string
  type?: string | string[]
  enum?: unknown[]
  format?: string
  maxLength?: number
  minimum?: number
  maximum?: number
  properties?: Record<string, Schema>
  required?: string[]
  items?: Schema
}
const schemas: Record<string, Schema> = openapi.components.schemas
const exports: Record<string, unknown> = contracts

function resolve(schema: Schema): Schema {
  if (!schema.$ref) return schema
  const name = schema.$ref.split('/').at(-1)
  const target = name ? schemas[name] : undefined
  if (!target) throw new Error(`Missing reference ${schema.$ref}`)
  return target
}

function wireExample(input: Schema): unknown {
  const schema = resolve(input)
  if (schema.enum) return schema.enum[0]
  const type = Array.isArray(schema.type) ? schema.type.find(type => type !== 'null') : schema.type
  switch (type) {
    case 'object': return Object.fromEntries(Object.entries(schema.properties ?? {}).map(([key, property]) => [key, wireExample(property)]))
    case 'array': return [wireExample(schema.items ?? {})]
    case 'string': return schema.format === 'uuid' ? fixtureId(1) : schema.format === 'date-time' ? FIXTURE_TIMESTAMP : 'synthetic'
    case 'integer': return 7
    case 'boolean': return false
    default: throw new Error(`Unsupported OpenAPI example type ${type}`)
  }
}

function validVariants(input: Schema): unknown[] {
  const schema = resolve(input)
  const variants: unknown[] = [wireExample(schema)]
  if (Array.isArray(schema.type) && schema.type.includes('null')) variants.push(null)
  if (schema.enum) variants.push(...schema.enum)
  if (schema.type === 'array') variants.push([])
  if (schema.type === 'boolean') variants.push(true)
  if (schema.maxLength !== undefined) variants.push('😀'.repeat(schema.maxLength))
  if (schema.minimum !== undefined) variants.push(schema.minimum)
  if (schema.maximum !== undefined) variants.push(schema.maximum)
  return variants
}

function invalidVariants(input: Schema): unknown[] {
  const schema = resolve(input)
  const variants: unknown[] = [undefined, 1.5, {}, []]
  if (!(Array.isArray(schema.type) && schema.type.includes('null'))) variants.push(null)
  if (schema.enum) variants.push('not-an-enum-value')
  if (schema.format === 'uuid') variants.push('bad-id', FIXTURE_TIMESTAMP)
  if (schema.format === 'date-time') variants.push('2026-10-07', '2026-99-99T99:99:99Z')
  if (schema.type === 'array') return [undefined, null, {}, [null], [1.5]]
  if (schema.type === 'object') return [undefined, null, [], { malformed: true }]
  if (schema.maxLength !== undefined) variants.push('😀'.repeat(schema.maxLength + 1))
  if (schema.minimum !== undefined) variants.push(schema.minimum - 1)
  if (schema.maximum !== undefined) variants.push(schema.maximum + 1)
  return variants
}

// The public OpenAPI document is an independent wire-contract oracle for
// generated guards. Every field is exercised as present, missing, nullable,
// wrongly typed, and (where declared) at its enum/string boundary.
// This catches generator regressions across the entire API, including nested
// objects and array items, without maintaining a second hand-written schema.
describe('REST runtime guards match published OpenAPI contracts', () => {
  for (const [name, schema] of Object.entries(schemas)) {
    const exportName = name.split('.').map(part => part[0]?.toUpperCase() + part.slice(1)).join('')
    const guard = exports[`is${exportName}`]
    const decode = exports[`decode${exportName}`]
    if (typeof guard !== 'function' || typeof decode !== 'function') throw new Error(`Missing generated contract ${name}`)

    it(`${name}: accepts full responses and rejects malformed roots`, () => {
      for (const valid of validVariants(schema)) {
        expect(guard(valid)).toBe(true)
        expect(decode(valid)).toBe(valid)
      }
      const roots = schema.type === 'object' ? [undefined, null, [], 1, 'synthetic'] : [undefined, null, {}, 1, 'unsupported-enum']
      for (const invalid of roots) {
        expect(guard(invalid)).toBe(false)
        expect(() => decode(invalid)).toThrow(ContractError)
      }
    })

    if (schema.type !== 'object') continue
    for (const [field, property] of Object.entries(schema.properties ?? {})) {
      it(`${name}.${field}: enforces requiredness and field constraints`, () => {
        const example = wireExample(schema)
        if (typeof example !== 'object' || example === null || Array.isArray(example)) throw new Error('Expected object example')
        const value = { ...example }
        Reflect.deleteProperty(value, field)
        const required = schema.required?.includes(field) ?? false
        expect(guard(value)).toBe(!required)
        for (const valid of validVariants(property)) expect(guard({ ...example, [field]: valid })).toBe(true)
        for (const invalid of invalidVariants(property)) expect(guard({ ...example, [field]: invalid })).toBe(false)
      })
    }
  }
})
