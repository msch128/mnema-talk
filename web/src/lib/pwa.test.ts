import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const platformMock = vi.hoisted(() => ({ desktop: false, trusted: false, selector: false }))
vi.mock('./platform', () => ({
  platform: {
    isDesktop: () => platformMock.desktop,
    isTrustedDesktop: () => platformMock.trusted,
    isWebDesktopSelector: () => platformMock.selector,
  },
}))

import { captureInstallPrompt, installState, isIosDevice, isStandalone, promptInstall, registerServiceWorker, serviceWorkerAllowed } from './pwa'

function fireInstallPrompt(outcome: 'accepted' | 'dismissed' = 'accepted') {
  const event = Object.assign(new Event('beforeinstallprompt', { cancelable: true }), {
    prompt: vi.fn(async () => {}),
    userChoice: Promise.resolve({ outcome }),
  })
  window.dispatchEvent(event)
  return event
}

beforeEach(() => {
  platformMock.desktop = platformMock.trusted = platformMock.selector = false
  vi.stubGlobal('navigator', Object.assign(Object.create(navigator), { serviceWorker: { register: vi.fn(async () => ({})) } }))
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.unstubAllEnvs()
})

describe('where the service worker may run', () => {
  it('runs in a normal browser tab', () => {
    expect(serviceWorkerAllowed()).toBe(true)
  })

  it('does not run in the desktop client, its bundle or its selector', () => {
    for (const key of ['desktop', 'trusted', 'selector'] as const) {
      platformMock[key] = true
      expect(serviceWorkerAllowed(), key).toBe(false)
      platformMock[key] = false
    }
  })

  it('does not run in a browser without service workers', () => {
    vi.stubGlobal('navigator', {})
    expect(serviceWorkerAllowed()).toBe(false)
  })
})

describe('registering the worker', () => {
  it('registers /sw.js for the whole origin in a release build', () => {
    vi.stubEnv('PROD', true)
    registerServiceWorker()
    expect(navigator.serviceWorker.register).toHaveBeenCalledWith('/sw.js', { scope: '/', updateViaCache: 'none' })
  })

  it('stays out of development builds and of the desktop client', () => {
    vi.stubEnv('PROD', false)
    registerServiceWorker()
    vi.stubEnv('PROD', true)
    platformMock.desktop = true
    registerServiceWorker()
    expect(navigator.serviceWorker.register).not.toHaveBeenCalled()
  })

  it('waits for the page to finish loading', () => {
    vi.stubEnv('PROD', true)
    vi.spyOn(document, 'readyState', 'get').mockReturnValue('loading')
    registerServiceWorker()
    expect(navigator.serviceWorker.register).not.toHaveBeenCalled()
    window.dispatchEvent(new Event('load'))
    expect(navigator.serviceWorker.register).toHaveBeenCalledTimes(1)
  })

  it('keeps the app working when the registration fails', async () => {
    vi.stubEnv('PROD', true)
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.mocked(navigator.serviceWorker.register).mockRejectedValueOnce(new Error('blocked'))
    registerServiceWorker()
    await vi.waitFor(() => expect(warn).toHaveBeenCalled())
  })
})

describe('installing the app', () => {
  it('detects iPhone, iPad and a standalone window', () => {
    vi.stubGlobal('navigator', { userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)', platform: 'iPhone', maxTouchPoints: 5 })
    expect(isIosDevice()).toBe(true)
    vi.stubGlobal('navigator', { userAgent: 'Mozilla/5.0 (Macintosh)', platform: 'MacIntel', maxTouchPoints: 5 })
    expect(isIosDevice()).toBe(true)
    vi.stubGlobal('navigator', { userAgent: 'Mozilla/5.0 (Macintosh)', platform: 'MacIntel', maxTouchPoints: 0 })
    expect(isIosDevice()).toBe(false)
    vi.stubGlobal('navigator', { standalone: true })
    expect(isStandalone()).toBe(true)
    vi.stubGlobal('navigator', {})
    vi.stubGlobal('matchMedia', () => ({ matches: true }))
    expect(isStandalone()).toBe(true)
  })

  it('offers the held-back browser prompt once, then reports the app as installed', async () => {
    vi.stubGlobal('navigator', { userAgent: 'Chrome', platform: 'Linux', maxTouchPoints: 0 })
    vi.stubGlobal('matchMedia', () => ({ matches: false }))
    captureInstallPrompt()
    expect(installState()).toBe('unavailable')
    const event = fireInstallPrompt()
    expect(event.defaultPrevented).toBe(true)
    expect(installState()).toBe('available')
    expect(await promptInstall()).toBe(true)
    expect(event.prompt).toHaveBeenCalledTimes(1)
    expect(installState()).toBe('installed')
    expect(await promptInstall()).toBe(false)
  })
})
