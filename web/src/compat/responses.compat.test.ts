import { describe, it, expect } from 'vitest'
import * as rest from '../types/rest'
import { decodeServerHealth } from '../types/rest'
import { allBaselines } from './baselines'
import { synthesize } from './contract'
import { responseGaps } from './allowlist'

type Decode = (value: unknown) => unknown

/** chat.Message -> decodeChatMessage, the way types/rest.ts names its guards. */
function decoderFor(schema: string): Decode | undefined {
  const name = `decode${schema.split('.').map(part => part.charAt(0).toUpperCase() + part.slice(1)).join('')}`
  const found = (rest as Record<string, unknown>)[name]
  return typeof found === 'function' ? found as Decode : undefined
}

const gapsOf = (version: string) => new Map(responseGaps.filter(gap => gap.versions.includes(version)).map(gap => [gap.schema, gap.reason]))

describe.each(allBaselines)('answers of server $version', baseline => {
  const gaps = gapsOf(baseline.version)

  it('the health answer passes', () => {
    expect(() => decodeServerHealth(baseline.health)).not.toThrow()
  })

  const names = Object.keys(baseline.schemas).filter(name => decoderFor(name))

  it.each(names.filter(name => !gaps.has(name)))('%s decodes as sent by that release', name => {
    const decode = decoderFor(name)!
    expect(() => decode(synthesize(baseline, name, 'minimal')), 'only the required keys').not.toThrow()
    expect(() => decode(synthesize(baseline, name, 'full')), 'every key').not.toThrow()
  })

  it.each([...gaps.keys()])('%s is still a known gap (remove its entry once it decodes)', name => {
    const decode = decoderFor(name)!
    const fails = (mode: 'minimal' | 'full') => { try { decode(synthesize(baseline, name, mode)); return false } catch { return true } }
    expect(fails('minimal') || fails('full')).toBe(true)
  })
})
