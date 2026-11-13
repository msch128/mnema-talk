import { afterEach, describe, expect, it, vi } from 'vitest'

type Listener = (event: { matches: boolean }) => void

function stubWindow(matches: boolean) {
  let onChange: Listener = () => undefined
  const asked: string[] = []
  vi.stubGlobal('matchMedia', (query: string) => {
    asked.push(query)
    return { matches, addEventListener: (_type: string, fn: Listener) => { onChange = fn } }
  })
  return { asked, resize: (wide: boolean) => onChange({ matches: wide }) }
}

async function load() {
  vi.resetModules()
  const { useColumnRoom } = await import('./useColumnRoom')
  return useColumnRoom()
}

describe('useColumnRoom', () => {
  afterEach(() => { vi.unstubAllGlobals() })

  it('has room for every column from 1120px on and follows the window', async () => {
    const win = stubWindow(true)
    const room = await load()
    expect(win.asked).toEqual(['(min-width: 1120px)'])
    expect(room.value).toBe(true)
    win.resize(false)
    expect(room.value).toBe(false)
    win.resize(true)
    expect(room.value).toBe(true)
  })

  it('has no room on a window just above the phone layout', async () => {
    stubWindow(false)
    expect((await load()).value).toBe(false)
  })
})
