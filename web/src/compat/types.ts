// The shape of a committed baseline (scripts/compat-baseline.ts writes it).
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
