import { afterEach, describe, expect, it, vi } from 'vitest'
import type { TrustedTypeRules } from './trustedTypes'

/** The browser's policy factory: values are opaque objects, and a name the page's policy does not list is refused. */
function stubTrustedTypes(allowed: string[]) {
  const created: string[] = []
  vi.stubGlobal('trustedTypes', {
    createPolicy(name: string, rules: TrustedTypeRules<unknown[]>) {
      if (!allowed.includes(name)) throw new TypeError(`Policy "${name}" disallowed.`)
      created.push(name)
      return {
        createHTML: (input: string, ...args: unknown[]) => ({ policy: name, html: rules.createHTML?.(input, ...args) }),
        createScriptURL: (input: string, ...args: unknown[]) => ({ policy: name, url: rules.createScriptURL?.(input, ...args) })
      }
    }
  })
  return created
}

async function load() {
  vi.resetModules()
  return import('./trustedTypes')
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('without Trusted Types in the browser', () => {
  it('passes a worker script address on as the plain string', async () => {
    const { ownScriptUrl, createTrustedTypePolicy } = await load()
    expect(ownScriptUrl('/sw.js')).toBe('/sw.js')
    expect(createTrustedTypePolicy('anything', { createHTML: html => html })).toBeNull()
  })
})

describe('with Trusted Types', () => {
  it('makes worker script addresses through the policy mnema-script-url', async () => {
    const created = stubTrustedTypes(['mnema-script-url'])
    const { ownScriptUrl } = await load()
    expect(created).toEqual(['mnema-script-url'])
    expect(ownScriptUrl('/assets/worker.js')).toEqual({ policy: 'mnema-script-url', url: `${window.location.origin}/assets/worker.js` })
  })

  it('refuses a script from another origin', async () => {
    stubTrustedTypes(['mnema-script-url'])
    const { ownScriptUrl } = await load()
    expect(() => ownScriptUrl('https://elsewhere.example/worker.js')).toThrow(/another origin/)
    expect(() => ownScriptUrl('//elsewhere.example/worker.js')).toThrow(/another origin/)
    expect(() => ownScriptUrl('data:text/javascript,1')).toThrow(/another origin/)
    expect(() => ownScriptUrl('blob:null/5c1c7a52-0f6e-4c0b-9b0a-4a4c4d0e9f11')).toThrow(/another origin/)
  })

  it('falls back to plain strings when the page does not allow the policy', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined)
    stubTrustedTypes([])
    const { ownScriptUrl } = await load()
    expect(ownScriptUrl('/sw.js')).toBe('/sw.js')
    expect(warn).toHaveBeenCalledWith('Trusted Types policy mnema-script-url not created:', expect.any(TypeError))
  })
})
