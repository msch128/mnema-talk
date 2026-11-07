import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { baselineFrom } from '../../scripts/compat-baseline.ts'
import type { CompatBaseline } from './contract.fixture'

// Vitest runs from web/, the repository root is its parent.
const read = (path: string) => readFileSync(resolve(process.cwd(), '..', path), 'utf8')

/** The server of this very checkout: what the web app that ships with it must work against. */
export function headBaseline(): CompatBaseline {
  return baselineFrom('head', {
    openapi: read('api/openapi.json'),
    healthGo: read('internal/server/health.go'),
    eventsTs: read('web/src/types/events.ts')
  })
}
