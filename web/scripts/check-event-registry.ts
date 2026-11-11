// The server's WebSocket event registry (internal/events/types.go, pinned by
// internal/events/testdata/events.golden.json) and web/src/types/events.ts must
// name the same events, and every server event must have a shape generated
// from its Go type (api/events.json, web/src/types/eventShapes.ts). Event names
// and payloads are public API (doc/api-stability.md).
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

interface Golden { server: Record<string, string>; client: Record<string, string> }

function read(path: string): string {
  return readFileSync(fileURLToPath(new URL(path, import.meta.url)), 'utf8')
}

/** The keys of `export interface <name> { ... }` in events.ts. */
export function interfaceKeys(source: string, name: string): string[] {
  const start = source.indexOf(`export interface ${name} {`)
  if (start < 0) throw new Error(`events.ts has no interface ${name}`)
  const end = source.indexOf('\n}', start)
  return [...source.slice(start, end).matchAll(/^ {2}([a-z_]+): /gm)].map(match => match[1]!)
}

export function differences(direction: string, goEvents: string[], tsEvents: string[]): string[] {
  const go = new Set(goEvents)
  const ts = new Set(tsEvents)
  return [
    ...goEvents.filter(name => !ts.has(name)).map(name => `${direction} event ${name} is in the Go registry but not in events.ts`),
    ...tsEvents.filter(name => !go.has(name)).map(name => `${direction} event ${name} is in events.ts but not in the Go registry`)
  ]
}

/** Server events without a generated shape, and shapes of events the registry does not know. */
export function shapeDifferences(goEvents: string[], shaped: string[]): string[] {
  const go = new Set(goEvents)
  const have = new Set(shaped)
  return [
    ...goEvents.filter(name => !have.has(name)).map(name => `server event ${name} has no shape in eventShapes.ts (no Go event type is registered for it)`),
    ...shaped.filter(name => !go.has(name)).map(name => `eventShapes.ts has a shape for ${name}, which is not in the Go registry`)
  ]
}

function main(): void {
  const golden = JSON.parse(read('../../internal/events/testdata/events.golden.json')) as Golden
  const source = read('../src/types/events.ts')
  const server = Object.values(golden.server)
  const problems = [
    ...differences('server', server, interfaceKeys(source, 'ServerEventPayloads')),
    ...shapeDifferences(server, interfaceKeys(read('../src/types/eventShapes.ts'), 'ServerEventShapes')),
    ...differences('client', Object.values(golden.client), interfaceKeys(source, 'ClientEventPayloads'))
  ]
  if (problems.length > 0) {
    process.stderr.write(`WebSocket events disagree between Go and the web app:\n  ${problems.join('\n  ')}\n`)
    process.exitCode = 1
  }
}

if (process.argv[1] === fileURLToPath(import.meta.url)) main()
