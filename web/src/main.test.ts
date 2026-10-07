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
  boot.browserLocale.mockReturnValue('de')
  boot.app.mount.mockReturnValue(undefined)
  document.body.innerHTML = '<div id="app"></div>'
})

describe('browser application entry', () => {
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
