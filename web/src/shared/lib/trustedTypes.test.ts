import { afterEach, describe, expect, it, vi } from 'vitest'
import type { TrustedTypeRules } from './trustedTypes'
import dfnWorkerUrl from '../../lib/dfn/dfnWorker.ts?worker&url'
import mediaFrameWorkerUrl from '../../lib/media/mediaFrameWorker.ts?worker&url'

/**
 * The browser's policy factory under `trusted-types <allowed>` without
 * 'allow-duplicates': values are opaque objects, a name the page's policy does
 * not list is refused, and so is a second policy of a name that is taken.
 */
function stubTrustedTypes(allowed: string[]) {
  const created: string[] = []
  const factory = {
    createPolicy(name: string, rules: TrustedTypeRules<unknown[]>) {
      if (!allowed.includes(name)) throw new TypeError(`Policy "${name}" disallowed.`)
      if (created.includes(name)) throw new TypeError(`Policy with name "${name}" already exists.`)
      created.push(name)
      return {
        createHTML: (input: string, ...args: unknown[]) => ({ policy: name, html: rules.createHTML?.(input, ...args) }),
        createScriptURL: (input: string, ...args: unknown[]) => ({ policy: name, url: rules.createScriptURL?.(input, ...args) })
      }
    }
  }
  vi.stubGlobal('trustedTypes', factory)
  return { created, factory }
}

const APP_POLICIES = ['mnema-script-url', 'mnema-emoji-picker']
const passThrough = { createHTML: (html: string) => html, createScriptURL: (url: string) => url }

async function load() {
  vi.resetModules()
  return import('./trustedTypes')
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.unstubAllEnvs()
  vi.restoreAllMocks()
})

describe('without Trusted Types in the browser', () => {
  it('passes worker script addresses and picker templates on as plain strings', async () => {
    const { ownScriptUrl, emojiPickerTemplate, createTrustedTypePolicy } = await load()
    expect(ownScriptUrl('/sw.js')).toBe('/sw.js')
    expect(emojiPickerTemplate('<div class="picker"></div>')).toBe('<div class="picker"></div>')
    expect(createTrustedTypePolicy('anything', { createHTML: html => html })).toBeNull()
  })
})

describe('the policy names of the app', () => {
  it('are taken as soon as the module has loaded, before the picker is opened', async () => {
    const { created, factory } = stubTrustedTypes(APP_POLICIES)
    await load()
    expect(created).toEqual(APP_POLICIES)
    expect(() => factory.createPolicy('mnema-emoji-picker', passThrough)).toThrow(/already exists/)
    expect(() => factory.createPolicy('mnema-script-url', passThrough)).toThrow(/already exists/)
  })

  it('hands the picker its templates through the policy created at start-up', async () => {
    stubTrustedTypes(APP_POLICIES)
    const { emojiPickerTemplate } = await load()
    expect(emojiPickerTemplate('<div class="picker"></div>')).toEqual({ policy: 'mnema-emoji-picker', html: '<div class="picker"></div>' })
  })

  it('fall back to plain strings when the page does not allow them', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined)
    stubTrustedTypes([])
    const { ownScriptUrl, emojiPickerTemplate } = await load()
    expect(ownScriptUrl('/sw.js')).toBe('/sw.js')
    expect(emojiPickerTemplate('<b></b>')).toBe('<b></b>')
    expect(warn).toHaveBeenCalledWith('Trusted Types policy mnema-script-url not created:', expect.any(TypeError))
    expect(warn).toHaveBeenCalledWith('Trusted Types policy mnema-emoji-picker not created:', expect.any(TypeError))
  })
})

describe.each([
  { build: 'the production build', dev: false, worker: '/assets/mediaFrameWorker-BhT3x9Qa.js' },
  { build: 'the dev server', dev: true, worker: mediaFrameWorkerUrl },
  { build: 'the dev server', dev: true, worker: dfnWorkerUrl }
])('worker script addresses in $build ($worker)', ({ dev, worker }) => {
  async function policy() {
    vi.stubEnv('DEV', dev)
    vi.stubEnv('PROD', !dev)
    stubTrustedTypes(APP_POLICIES)
    return (await load()).ownScriptUrl
  }
  const made = (path: string) => ({ policy: 'mnema-script-url', url: `${window.location.origin}${path}` })

  it('accepts the worker script and the service worker', async () => {
    const ownScriptUrl = await policy()
    expect(ownScriptUrl(worker)).toEqual(made(worker))
    expect(ownScriptUrl('/sw.js')).toEqual(made('/sw.js'))
    expect(ownScriptUrl(`${window.location.origin}/sw.js`)).toEqual(made('/sw.js'))
  })

  it('refuses a same-origin address that is not a script of the build', async () => {
    const ownScriptUrl = await policy()
    for (const path of ['/api/media/7c1d/evil.js', '/api/emoji/12', '/uploads/evil.js', '/', '/sw.js/x', '/sw.jsx', '/assetsx/worker.js', '/assets/../api/media/7c1d/evil.js', '/assets/%2e%2e/api/x']) {
      expect(() => ownScriptUrl(path), path).toThrow(/not one of the app's own/)
    }
  })

  it('refuses another origin, data: and blob:', async () => {
    const ownScriptUrl = await policy()
    for (const url of [
      'https://elsewhere.example/assets/worker.js',
      '//elsewhere.example/sw.js',
      'data:text/javascript,1',
      'blob:null/5c1c7a52-0f6e-4c0b-9b0a-4a4c4d0e9f11',
      `blob:${window.location.origin}/5c1c7a52-0f6e-4c0b-9b0a-4a4c4d0e9f11`
    ]) {
      expect(() => ownScriptUrl(url), url).toThrow(/another origin/)
    }
  })
})

describe('the production build', () => {
  it('refuses the source paths only the dev server serves', async () => {
    vi.stubEnv('DEV', false)
    stubTrustedTypes(APP_POLICIES)
    const { ownScriptUrl } = await load()
    expect(() => ownScriptUrl('/src/lib/media/mediaFrameWorker.ts?worker_file&type=module')).toThrow(/not one of the app's own/)
  })
})
