import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

type Listener = (event: { matches: boolean }) => void

function stubWindow(matches: boolean, viewportHeight = 800, innerHeight = 800) {
  let onChange: Listener = () => undefined
  const viewportListeners: Record<string, () => void> = {}
  const viewport = {
    height: viewportHeight,
    addEventListener: (type: string, fn: () => void) => { viewportListeners[type] = fn },
  }
  vi.stubGlobal('matchMedia', () => ({
    matches,
    addEventListener: (_type: string, fn: Listener) => { onChange = fn },
  }))
  vi.stubGlobal('visualViewport', viewport)
  vi.stubGlobal('innerHeight', innerHeight)
  vi.stubGlobal('scrollTo', vi.fn())
  return {
    switchTo: (next: boolean) => onChange({ matches: next }),
    resizeViewport: (height: number) => { viewport.height = height; viewportListeners['resize']?.() },
  }
}

async function load() {
  vi.resetModules()
  const { useCompactLayout } = await import('./useCompactLayout')
  return useCompactLayout()
}

describe('useCompactLayout', () => {
  beforeEach(() => { document.documentElement.style.removeProperty('--app-h') })
  afterEach(() => { vi.unstubAllGlobals() })

  it('is compact on a phone-sized window and follows the window when it grows', async () => {
    const win = stubWindow(true)
    const layout = await load()
    expect(layout.isCompact.value).toBe(true)

    layout.openNav()
    expect(layout.navOpen.value).toBe(true)
    layout.closeNav()
    expect(layout.navOpen.value).toBe(false)

    layout.openNav()
    win.switchTo(false)
    expect(layout.isCompact.value).toBe(false)
    expect(layout.navOpen.value).toBe(false)
  })

  it('is not compact on a desktop-sized window', async () => {
    stubWindow(false)
    expect((await load()).isCompact.value).toBe(false)
  })

  it('shrinks the page to the visible area while a keyboard covers part of it, and releases it after', async () => {
    const win = stubWindow(true, 800, 800)
    await load()
    win.resizeViewport(420)
    expect(document.documentElement.style.getPropertyValue('--app-h')).toBe('420px')
    win.resizeViewport(800)
    expect(document.documentElement.style.getPropertyValue('--app-h')).toBe('')
  })

  it('leaves the page height alone on a desktop window', async () => {
    const win = stubWindow(false, 800, 800)
    await load()
    win.resizeViewport(420)
    expect(document.documentElement.style.getPropertyValue('--app-h')).toBe('')
  })
})
