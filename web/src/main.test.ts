import { describe, it, expect, vi, beforeEach } from 'vitest'

const boot = vi.hoisted(() => {
  const app = { use: vi.fn<(plugin: unknown) => unknown>(), directive: vi.fn<(name: string, value: unknown) => unknown>(), mount: vi.fn<(selector: string) => void>() }
  app.use.mockReturnValue(app)
  app.directive.mockReturnValue(app)
  return { app, createApp: vi.fn((root: unknown) => { void root; return app }), pinia: { kind: 'synthetic-pinia' }, plugin: { install: vi.fn() }, browserLocale: vi.fn(() => 'de'), setLocale: vi.fn() }
})
vi.mock('vue', async importOriginal => ({ ...await importOriginal<typeof import('vue')>(), createApp: boot.createApp }))
vi.mock('pinia', async importOriginal => ({ ...await importOriginal<typeof import('pinia')>(), createPinia: () => boot.pinia }))
vi.mock('./i18n', () => ({ i18nPlugin: boot.plugin, browserLocale: boot.browserLocale, setLocale: boot.setLocale, t: (key: string) => key }))

beforeEach(() => {
  vi.resetModules()
  vi.clearAllMocks()
  vi.unstubAllGlobals()
  Reflect.deleteProperty(window, '__MNEMA_GAMING_SURFACE__')
  boot.browserLocale.mockReturnValue('de')
  boot.app.mount.mockReturnValue(undefined)
  document.body.innerHTML = '<div id="app"></div>'
})

describe('browser application entry', () => {
  it('mounts the complete App at a selected HTTPS instance even inside Tauri', async () => {
    vi.stubGlobal('isTauri', true)
    vi.stubGlobal('location', new URL('https://community.example/'))
    await import('./main')
    expect(boot.createApp).toHaveBeenCalledWith((await import('./App.vue')).default)
  })
  it('mounts the local gaming surface with the canonical plugins', async () => {
    Reflect.set(window, '__MNEMA_GAMING_SURFACE__', 'gaming-overlay')
    await import('./main')
    expect(boot.createApp).toHaveBeenCalledWith((await import('./components/DesktopGamingOverlay.vue')).default)
    expect(boot.app.use.mock.calls).toEqual([[boot.pinia], [boot.plugin]])
    Reflect.deleteProperty(window, '__MNEMA_GAMING_SURFACE__')
  })
  it('mounts native discovery with the same plugins inside a real Tauri runtime', async () => {
    vi.stubGlobal('isTauri', true); vi.stubGlobal('location', new URL('http://tauri.localhost/'))
    await import('./main')
    expect(boot.createApp).toHaveBeenCalledWith((await import('./NativePreviewApp.vue')).default)
    expect(boot.app.use.mock.calls).toEqual([[boot.pinia], [boot.plugin]])
    expect(boot.app.mount).toHaveBeenCalledWith('#app')
  })
  it('mounts the instance selector for the verified bundle in the trusted desktop client', async () => {
    vi.stubGlobal('isTauri', true); vi.stubGlobal('location', new URL('mnema://localhost/'))
    vi.stubGlobal('__MNEMA_TRUSTED_APP__', { version: '0.8.0', source: 'builtin', webClientApi: 1, keyId: '1069c76ff2e11272', role: 'selector' })
    await import('./main')
    expect(boot.createApp).toHaveBeenCalledWith((await import('./components/DesktopSelector.vue')).default)
  })
  it('mounts the instance selector on the legacy Tauri selector page', async () => {
    vi.stubGlobal('isTauri', true); vi.stubGlobal('location', new URL('http://tauri.localhost/'))
    vi.stubGlobal('__MNEMA_WEB_DESKTOP__', true)
    await import('./main')
    expect(boot.createApp).toHaveBeenCalledWith((await import('./components/DesktopSelector.vue')).default)
  })
  it('keeps the gaming surface ahead of the instance selector', async () => {
    Reflect.set(window, '__MNEMA_GAMING_SURFACE__', 'gaming-overlay')
    vi.stubGlobal('isTauri', true); vi.stubGlobal('location', new URL('mnema://localhost/'))
    vi.stubGlobal('__MNEMA_TRUSTED_APP__', { version: '0.8.0', source: 'builtin', webClientApi: 1, keyId: '1069c76ff2e11272', role: 'selector' })
    await import('./main')
    expect(boot.createApp).toHaveBeenCalledWith((await import('./components/DesktopGamingOverlay.vue')).default)
  })
  it('mounts the complete App for the verified bundle once an instance is selected', async () => {
    vi.stubGlobal('isTauri', true); vi.stubGlobal('location', new URL('mnema://localhost/'))
    vi.stubGlobal('__MNEMA_TRUSTED_APP__', { version: '0.8.0', source: 'builtin', webClientApi: 1, keyId: '1069c76ff2e11272', role: 'instance' })
    await import('./main')
    expect(boot.createApp).toHaveBeenCalledWith((await import('./App.vue')).default)
  })
  it('selects browser language and mounts App with Pinia, translations and accessible tooltips', async () => {
    await import('./main')
    expect(boot.setLocale).toHaveBeenCalledWith('de')
    expect(boot.createApp).toHaveBeenCalledWith((await import('./App.vue')).default)
    expect(boot.app.use.mock.calls).toEqual([[boot.pinia], [boot.plugin]])
    expect(boot.app.directive).toHaveBeenCalledWith('tooltip', (await import('./directives/tooltip')).tooltip)
    expect(boot.app.mount).toHaveBeenCalledWith('#app')
    expect(boot.setLocale.mock.invocationCallOrder[0]).toBeLessThan(boot.createApp.mock.invocationCallOrder[0] ?? 0)
  })
  it('uses an English browser and surfaces mount failure instead of silently succeeding', async () => {
    boot.browserLocale.mockReturnValue('en')
    boot.app.mount.mockImplementationOnce(() => { throw new Error('missing application root') })
    await expect(import('./main')).rejects.toThrow('missing application root')
    expect(boot.setLocale).toHaveBeenCalledWith('en')
    expect(boot.app.mount).toHaveBeenCalledOnce()
  })
})
