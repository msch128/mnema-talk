import { afterEach, describe, expect, it, vi } from 'vitest'
import { effectScope, nextTick } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { messageFixture } from '../../test-fixtures.fixture'

async function columns(width: number, roomForAll: boolean) {
  vi.resetModules()
  vi.stubGlobal('innerWidth', width)
  vi.stubGlobal('matchMedia', () => ({ matches: roomForAll, addEventListener: () => undefined }))
  setActivePinia(createPinia())
  // Fresh modules: the window queries are read once per module.
  const chat = (await import('../../stores/chat')).useChatStore()
  const lib = await import('../../shared/lib/sideColumns')
  const { useSideColumns } = await import('./useSideColumns')
  const scope = effectScope()
  const side = scope.run(() => useSideColumns({ leftWidth: () => 240, compact: () => false }))
  if (!side) throw new Error('no side columns')
  return { chat, side, scope, lib }
}

afterEach(() => { vi.unstubAllGlobals() })

describe('useSideColumns', () => {
  it('opens a thread over the chat when a column would leave the chat under its minimum (a tablet)', async () => {
    const { chat, side, scope, lib } = await columns(820, false)
    chat.activeThread = messageFixture()
    await nextTick()
    expect(side.threadOver.value).toBe(true)
    expect(lib.threadOverlay.value).toBe(true)
    expect(lib.membersSteppedAside.value).toBe(false)
    scope.stop()
  })

  it('lets the member list step aside for a thread column, and opens it over the chat on request', async () => {
    const { chat, side, scope, lib } = await columns(1000, false)
    chat.showMemberList = true
    chat.activeThread = messageFixture()
    await nextTick()
    expect(side.threadOver.value).toBe(false)
    expect(side.membersColumn.value).toBe(false)
    expect(side.membersOver.value).toBe(false)
    lib.toggleMemberList(chat)
    await nextTick()
    expect(side.membersOver.value).toBe(true)
    chat.activeThread = null
    await nextTick()
    expect(lib.membersOverlay.value).toBe(false)
    expect(side.membersColumn.value).toBe(true)
    scope.stop()
  })
})
