import type { Channel } from '../types/domain'
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { parseRoute, savePendingRoute, consumePendingRoute, navigate, popRedirectRoute, currentRoute, guardRoute, resolveRoute, canGoBackInApp } from './router'

describe('router', () => {
  beforeEach(() => {
    sessionStorage.clear()
  })

  it('parses text channel route', () => {
    const route = parseRoute('/c/12345678-1234-1234-1234-123456789abc')
    expect(route).toEqual({
      view: 'chat',
      channelId: '12345678-1234-1234-1234-123456789abc'
    })
  })

  it('parses text channel with single message route', () => {
    const route = parseRoute('/c/c-id/m/m-id')
    expect(route).toEqual({
      view: 'chat',
      channelId: 'c-id',
      messageId: 'm-id'
    })
  })

  it('parses text channel with thread route', () => {
    const route = parseRoute('/c/c-id/t/t-id')
    expect(route).toEqual({
      view: 'chat',
      channelId: 'c-id',
      threadId: 't-id'
    })
  })

  it('parses voice route in watching mode', () => {
    const route = parseRoute('/v/v-id')
    expect(route).toEqual({
      view: 'voice',
      channelId: 'v-id',
      watching: true
    })
  })

  it('parses voice route with chat open', () => {
    const route = parseRoute('/v/v-id/chat')
    expect(route).toEqual({
      view: 'voice',
      channelId: 'v-id',
      showChat: true,
      watching: true
    })
  })

  it('parses admin route with tab', () => {
    const route = parseRoute('/admin/media')
    expect(route).toEqual({
      view: 'admin',
      tab: 'media'
    })
  })

  it('preserves and consumes pending route across login', () => {
    // Mock location
    window.history.pushState(null, '', '/c/secret-channel/m/msg-123')
    savePendingRoute()
    expect(consumePendingRoute()).toBe('/c/secret-channel/m/msg-123')
    expect(consumePendingRoute()).toBeNull()
  })

  it('provides popRedirectRoute alias and navigate function with opts', () => {
    window.history.pushState(null, '', '/v/ch-voice')
    savePendingRoute()
    expect(popRedirectRoute()).toBe('/v/ch-voice')
    expect(popRedirectRoute()).toBeNull()

    navigate('/c/ch-1', { replace: false })
    expect(currentRoute.value).toEqual({ view: 'chat', channelId: 'ch-1' })
    expect(window.location.pathname).toBe('/c/ch-1')

    navigate('/v/ch-2/chat', { replace: true })
    expect(currentRoute.value).toEqual({ view: 'voice', channelId: 'ch-2', showChat: true, watching: true })
    expect(window.location.pathname).toBe('/v/ch-2/chat')
  })

  it('keeps the shared link across login (pending route round trip)', () => {
    window.history.replaceState(null, '', '/c/c-id/m/m-id')
    savePendingRoute()
    window.history.replaceState(null, '', '/')
    expect(popRedirectRoute()).toBe('/c/c-id/m/m-id')
  })
})

describe('route guards', () => {
  const channels: Pick<Channel, 'id' | 'type'>[] = [{ id: 't1', type: 'text' }, { id: 'v1', type: 'voice' }]

  it('redirects non-admins away from /admin/* and lets admins in', () => {
    const route = parseRoute('/admin/media')
    expect(guardRoute(route, { isAdmin: false })).toBe('/')
    expect(guardRoute(route, { isAdmin: true })).toBeNull()
    expect(resolveRoute(route, { isAdmin: false, channels })).toEqual({ redirect: '/', reason: 'noAccess' })
    expect(resolveRoute(route, { isAdmin: true, channels })).toBeNull()
  })

  it('falls back with a reason for unknown channels', () => {
    expect(resolveRoute(parseRoute('/c/nope'), { channels })).toEqual({ redirect: '/', reason: 'channelNotFound' })
    expect(resolveRoute(parseRoute('/v/nope/chat'), { channels })).toEqual({ redirect: '/', reason: 'voiceNotFound' })
    expect(resolveRoute(parseRoute('/c/t1/m/x'), { channels })).toBeNull()
  })

  it('corrects mismatched channel types silently', () => {
    expect(resolveRoute(parseRoute('/v/t1'), { channels })).toEqual({ redirect: '/c/t1', reason: null })
    expect(resolveRoute(parseRoute('/c/v1'), { channels })).toEqual({ redirect: '/v/v1', reason: null })
  })
})

describe('in-app history', () => {
  it('tracks pushed entries and keeps the depth on replace', () => {
    window.history.replaceState(null, '', '/c/a')
    expect(canGoBackInApp()).toBe(false)
    navigate('/admin/users')
    expect(canGoBackInApp()).toBe(true)
    // admin tab switches replace the entry: still one step back
    navigate('/admin/media', { replace: true })
    expect(canGoBackInApp()).toBe(true)
    expect(window.history.state.idx).toBe(1)
  })
})

describe('router browser/environment edges', () => {
  it('defaults admin tabs, avoids root/login pending links, and follows popstate', () => {
    expect(parseRoute('/admin')).toEqual({ view: 'admin', tab: 'users' })
    for (const path of ['/', '/login']) {
      window.history.replaceState(null, '', path)
      savePendingRoute()
      expect(consumePendingRoute()).toBeNull()
    }
    window.history.replaceState({ idx: 0.5 }, '', '/c/first')
    expect(canGoBackInApp()).toBe(false)
    navigate('/c/first', {})
    expect(window.history.state.idx).toBe(0.5)
    window.history.replaceState({ idx: 2 }, '', '/v/second')
    window.dispatchEvent(new PopStateEvent('popstate'))
    expect(currentRoute.value).toEqual({ view: 'voice', channelId: 'second', watching: true })
    expect(resolveRoute({ view: 'root' })).toBeNull()
  })
  it('has safe no-browser defaults and skips history/storage/listener access', async () => {
    vi.resetModules()
    vi.stubGlobal('window', undefined)
    try {
      const router = await import('./router')
      expect(router.parseRoute()).toEqual({ view: 'root' })
      expect(router.canGoBackInApp()).toBe(false)
      expect(router.navigate('/c/ignored')).toBeUndefined()
      expect(router.savePendingRoute()).toBeUndefined()
      expect(router.consumePendingRoute()).toBeNull()
    } finally { vi.unstubAllGlobals() }
  })
})
