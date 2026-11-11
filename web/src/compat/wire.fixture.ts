// A stand-in for a server of an older release: it answers what that release's
// contract answers, and records every way the app asks for more than that
// release promised (see requests.compat.test.ts).
import { ApiError, type ApiOptions } from '../shared/lib/api'
import { newEndpoints, responseGaps } from './allowlist'
import { bodyViolations, findOperation, synthesize, type CompatBaseline } from './contract.fixture'

type Options = ApiOptions & { decode?: (value: unknown) => unknown }

export interface Call { method: string, path: string, json: unknown }

type Patch = (answer: unknown) => unknown

export class Wire {
  readonly calls: Call[] = []
  readonly violations: string[] = []
  /** Calls the app made to something the release lacks, which the allowlist expects. */
  readonly tolerated: string[] = []
  /** While true every request fails like a dropped connection. */
  down = false
  private patches: Array<{ pattern: RegExp, patch: Patch, mode: 'minimal' | 'full' }> = []

  constructor(readonly baseline: CompatBaseline) {}

  /** Changes the answer of the requests whose "METHOD path" matches (and may ask for every key). */
  answer(pattern: RegExp, patch: Patch = a => a, mode: 'minimal' | 'full' = 'minimal') {
    this.patches.push({ pattern, patch, mode })
  }

  callsTo(pattern: RegExp): Call[] {
    return this.calls.filter(c => pattern.test(`${c.method} ${c.path}`))
  }

  readonly handle = async (path: string, options: Options = {}): Promise<unknown> => {
    const method = (options.method ?? 'GET').toUpperCase()
    this.calls.push({ method, path, json: options.json })
    if (this.down) throw new ApiError(0, 'NETWORK', 'Network error')
    const found = findOperation(this.baseline, method, path)
    if (!found) return this.missing(method, path)
    const { key, operation } = found
    const bare = path.split('?')[0]!
    const query = [...new URLSearchParams(path.split('?')[1] ?? '').keys()]
    for (const name of query.filter(q => !operation.query.includes(q))) this.violations.push(`${key}: query parameter ${name} is unknown`)
    if (options.json !== undefined && !operation.body) this.violations.push(`${key}: sends a body, the operation takes none`)
    if (operation.body) for (const problem of bodyViolations(this.baseline, operation.body, options.json ?? {}, `${operation.body.$ref ?? 'body'}`)) this.violations.push(`${key} ${bare}: ${problem}`)
    return this.reply(key, method, path, options, operation)
  }

  private missing(method: string, path: string): never {
    const bare = path.split('?')[0]!
    const known = newEndpoints.find(e => e.method === method && e.path === bare)
    if (!known) {
      this.violations.push(`${method} ${bare}: no such endpoint in ${this.baseline.version}, and none allowlisted`)
      throw new ApiError(404, 'NOT_FOUND', 'x')
    }
    this.tolerated.push(`${method} ${bare}`)
    throw new ApiError(known.expectOldStatus, 'INVALID_INPUT', 'x')
  }

  private reply(key: string, method: string, path: string, options: Options, operation: NonNullable<ReturnType<typeof findOperation>>['operation']): unknown {
    const patch = this.patches.find(p => p.pattern.test(`${method} ${path}`))
    let answer = operation.ok ? synthesize(this.baseline, operation.ok, patch?.mode ?? 'minimal') : null
    if (patch) answer = patch.patch(answer)
    if (operation.okStatus === 204) answer = null
    try {
      const decoded = options.decode ? options.decode(answer) : answer
      options.onStatus?.(operation.okStatus)
      return decoded
    } catch (error) {
      const schema = operation.ok?.$ref
      const excused = schema !== undefined && responseGaps.some(g => g.schema === schema && g.versions.includes(this.baseline.version))
      if (!excused) this.violations.push(`${key}: the app rejects the answer of ${this.baseline.version}: ${error instanceof Error ? error.message : String(error)}`)
      throw error
    }
  }
}
