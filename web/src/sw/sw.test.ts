import { afterEach, describe, expect, it, vi } from 'vitest'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.resetModules()
})

describe('the service worker entry', () => {
  it('installs the worker on its own scope with what the build baked in', async () => {
    const added: string[] = []
    vi.stubGlobal('__SHELL_VERSION__', 'abc')
    vi.stubGlobal('__SHELL_FILES__', ['/'])
    vi.stubGlobal('self', { addEventListener: (type: string) => added.push(type), location: { origin: 'https://app.test' } })
    await import('./sw')
    expect(added).toEqual(['install', 'activate', 'fetch'])
  })
})
