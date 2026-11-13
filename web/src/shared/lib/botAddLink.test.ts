import { beforeEach, describe, expect, it } from 'vitest'
import { captureBotAddLink, dropPendingBotManifest, hasPendingBotManifest, manifestParam, takePendingBotManifest } from './botAddLink'

beforeEach(() => {
  sessionStorage.clear()
  window.history.replaceState(null, '', '/')
})

describe('a bot add link', () => {
  it('keeps the manifest for the Bots tab and leaves it out of the address', () => {
    window.history.replaceState(null, '', '/admin/bots/add#manifest=eyJuYW1lIjoiTW5lbW8ifQ')
    expect(captureBotAddLink()).toBe(true)
    expect(window.location.pathname).toBe('/admin/bots')
    expect(window.location.hash).toBe('')
    expect(hasPendingBotManifest()).toBe(true)
    expect(takePendingBotManifest()).toBe('eyJuYW1lIjoiTW5lbW8ifQ')
    expect(takePendingBotManifest()).toBeNull()
  })

  it('leaves every other address alone', () => {
    window.history.replaceState(null, '', '/c/general#manifest=abc')
    expect(captureBotAddLink()).toBe(false)
    expect(window.location.hash).toBe('#manifest=abc')
    expect(hasPendingBotManifest()).toBe(false)
  })

  it('opens the Bots tab without a manifest when the fragment holds none', () => {
    window.history.replaceState(null, '', '/admin/bots/add#manifest=not%20base64')
    expect(captureBotAddLink()).toBe(true)
    expect(hasPendingBotManifest()).toBe(false)
  })

  it('reads only base64url', () => {
    expect(manifestParam('#manifest=ab-_')).toBe('ab-_')
    expect(manifestParam('#other=1')).toBeNull()
    expect(manifestParam('#manifest=a+b')).toBeNull()
  })

  it('can be dropped', () => {
    sessionStorage.setItem('mnema_pending_bot_manifest', 'x')
    dropPendingBotManifest()
    expect(hasPendingBotManifest()).toBe(false)
  })
})
