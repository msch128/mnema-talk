import type { Channel } from '../types/domain'
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { parseRoute, savePendingRoute, consumePendingRoute, navigate, popRedirectRoute, currentRoute, guardRoute, resolveRoute, canGoBackInApp, targetOf } from './router'

describe('router', () => {
  beforeEach(() => {
    sessionStorage.clear()
  })

  it('parses text channel route', () => {
    const route = parseRoute('/c/12345678-1234-1234-1234-123456789abc')
    expect(route).toEqual({
      view: 'chat',
      channelRef: '12345678-1234-1234-1234-123456789abc'
    })
  })

  it('parses text channel with single message route', () => {
    const route = parseRoute('/c/c-id/m/m-id')
    expect(route).toEqual({
      view: 'chat',
      channelRef: 'c-id',
      messageId: 'm-id'
    })
  })

  it('parses text channel with thread route', () => {
    const route = parseRoute('/c/c-id/t/t-id')
    expect(route).toEqual({
      view: 'chat',
      channelRef: 'c-id',
      threadId: 't-id'
    })
  })

  it('parses voice route in watching mode', () => {
    const route = parseRoute('/v/v-id')
    expect(route).toEqual({
      view: 'voice',
      channelRef: 'v-id',
      watching: true
    })
  })

  it('parses voice route with chat open', () => {
    const route = parseRoute('/v/v-id/chat')
    expect(route).toEqual({
      view: 'voice',
      channelRef: 'v-id',
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
    expect(currentRoute.value).toEqual({ view: 'chat', channelRef: 'ch-1' })
    expect(window.location.pathname).toBe('/c/ch-1')

    navigate('/v/ch-2/chat', { replace: true })
    expect(currentRoute.value).toEqual({ view: 'voice', channelRef: 'ch-2', showChat: true, watching: true })
    expect(window.location.pathname).toBe('/v/ch-2/chat')
  })

  it('never saves an invite link as the route to return to after login', () => {
    window.history.replaceState(null, '', '/invite/abc234xyz789')
    savePendingRoute()
    expect(consumePendingRoute()).toBeNull()
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
    expect(currentRoute.value).toEqual({ view: 'voice', channelRef: 'second', watching: true })
    expect(resolveRoute({ view: 'root' })).toBeNull()
  })
  it('never keeps or follows a protocol-relative path', () => {
    window.history.replaceState(null, '', '/')
    sessionStorage.setItem('mnema_pending_route', '//evil.example/x')
    expect(consumePendingRoute()).toBeNull()
    expect(() => navigate('//evil.example/x', { replace: true })).not.toThrow()
    expect(window.location.pathname).toBe('/')
    expect(currentRoute.value).toEqual({ view: 'root' })
  })
  it('survives blocked session storage', () => {
    window.history.replaceState(null, '', '/c/first')
    const blocked = () => { throw new DOMException('blocked') }
    vi.stubGlobal('sessionStorage', { setItem: blocked, getItem: blocked, removeItem: blocked })
    try {
      expect(() => savePendingRoute()).not.toThrow()
      expect(consumePendingRoute()).toBeNull()
    } finally { vi.unstubAllGlobals() }
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

describe('speaking addresses', () => {
  const channels = [
    { id: 'c1', type: 'text', slug: 'general', previous_slugs: ['old-name', 'main'] },
    { id: 'v1', type: 'voice', slug: 'lounge', previous_slugs: [] },
    { id: 'f1', type: 'forum', slug: 'ideas' },
    { id: 'old1', type: 'text' },
  ]
  const dms = [
    { id: 'aaaaaaaa-0000-4000-8000-000000000001', group: false, peer: 'Alice' },
    { id: 'bbbbbbbb-0000-4000-8000-000000000002', group: true, peer: null },
  ]
  const route = (path: string) => parseRoute(path)

  it('parses slugs, forum posts and conversations', () => {
    expect(route('/c/general')).toEqual({ view: 'chat', channelRef: 'general' })
    expect(route('/c/ideas/p/neue-kueche-0f3a9c1e')).toEqual({ view: 'chat', channelRef: 'ideas', postRef: 'neue-kueche-0f3a9c1e' })
    expect(route('/dm/alice')).toEqual({ view: 'dm', dmRef: 'alice', group: false })
    expect(route('/dm/a%20b/m/m1')).toEqual({ view: 'dm', dmRef: 'a b', group: false, messageId: 'm1' })
    expect(route('/dm/g/bbbbbbbb/t/t1')).toEqual({ view: 'dm', dmRef: 'bbbbbbbb', group: true, threadId: 't1' })
    expect(route('/c/%E0%A4%A')).toEqual({ view: 'chat', channelRef: '%E0%A4%A' })
  })

  it('shows the current slug as it is and the id of a channel without one', () => {
    expect(resolveRoute(route('/c/general/m/m1'), { channels })).toBeNull()
    expect(resolveRoute(route('/c/General'), { channels })).toBeNull()
    expect(resolveRoute(route('/c/old1'), { channels })).toBeNull()
    expect(resolveRoute(route('/v/lounge/chat'), { channels })).toBeNull()
  })

  it('rewrites a former slug to the current one, keeping the rest of the address', () => {
    expect(resolveRoute(route('/c/old-name'), { channels })).toEqual({ redirect: '/c/general', reason: null })
    expect(resolveRoute(route('/c/main/t/t1'), { channels })).toEqual({ redirect: '/c/general/t/t1', reason: null })
    expect(resolveRoute(route('/c/main/m/m1'), { channels })).toEqual({ redirect: '/c/general/m/m1', reason: null })
  })

  it('rewrites a legacy id link to the slug', () => {
    expect(resolveRoute(route('/c/c1/m/m1'), { channels })).toEqual({ redirect: '/c/general/m/m1', reason: null })
    expect(resolveRoute(route('/v/v1/chat/m/m9'), { channels })).toEqual({ redirect: '/v/lounge/chat/m/m9', reason: null })
    expect(resolveRoute(route('/v/v1'), { channels })).toEqual({ redirect: '/v/lounge', reason: null })
    expect(resolveRoute(route('/c/f1/p/x-0f3a9c1e'), { channels })).toEqual({ redirect: '/c/ideas/p/x-0f3a9c1e', reason: null })
  })

  it('corrects the kind of channel', () => {
    expect(resolveRoute(route('/v/general'), { channels })).toEqual({ redirect: '/c/general', reason: null })
    expect(resolveRoute(route('/c/lounge'), { channels })).toEqual({ redirect: '/v/lounge', reason: null })
    expect(resolveRoute(route('/c/lounge/m/m1'), { channels })).toEqual({ redirect: '/v/lounge/chat/m/m1', reason: null })
  })

  it('treats an unknown and an invisible channel alike', () => {
    expect(resolveRoute(route('/c/secret'), { channels })).toEqual({ redirect: '/', reason: 'channelNotFound' })
    expect(resolveRoute(route('/v/secret'), { channels })).toEqual({ redirect: '/', reason: 'voiceNotFound' })
    expect(resolveRoute(route('/c/secret/p/a-0f3a9c1e'), { channels: [] })).toEqual({ redirect: '/', reason: 'channelNotFound' })
  })

  it('opens conversations by member and by short id, and rewrites a legacy id', () => {
    expect(resolveRoute(route('/dm/alice'), { dms })).toBeNull()
    expect(resolveRoute(route('/dm/g/bbbbbbbb'), { dms })).toBeNull()
    expect(resolveRoute(route('/dm/bob'), { dms })).toEqual({ redirect: '/', reason: 'channelNotFound' })
    expect(resolveRoute(route('/dm/g/bbbb'), { dms })).toEqual({ redirect: '/', reason: 'channelNotFound' })
    expect(resolveRoute(route('/c/aaaaaaaa-0000-4000-8000-000000000001/m/m1'), { channels, dms })).toEqual({ redirect: '/dm/Alice/m/m1', reason: null })
    expect(resolveRoute(route('/c/bbbbbbbb-0000-4000-8000-000000000002'), { channels, dms })).toEqual({ redirect: '/dm/g/bbbbbbbb', reason: null })
  })

  it('does not take a short id that two conversations share', () => {
    const twins = [...dms, { id: 'bbbbbbbb-1111-4000-8000-000000000009', group: true, peer: null }]
    expect(resolveRoute(route('/dm/g/bbbbbbbb'), { dms: twins })).toEqual({ redirect: '/', reason: 'channelNotFound' })
    expect(resolveRoute(route('/dm/g/bbbbbbbb-0000-4000-8000-000000000002'), { dms: twins })).toBeNull()
  })

  it('resolves a route to the ids the app works with', () => {
    expect(targetOf(route('/c/general/t/t1'), { channels })).toEqual({ view: 'chat', channelId: 'c1', threadId: 't1' })
    expect(targetOf(route('/c/main'), { channels })).toEqual({ view: 'chat', channelId: 'c1' })
    expect(targetOf(route('/v/lounge/chat'), { channels })).toEqual({ view: 'voice', channelId: 'v1', showChat: true, watching: true })
    expect(targetOf(route('/dm/alice/m/m1'), { dms })).toEqual({ view: 'chat', channelId: dms[0]?.id, messageId: 'm1' })
    expect(targetOf(route('/dm/g/bbbbbbbb'), { dms })).toEqual({ view: 'chat', channelId: dms[1]?.id })
    expect(targetOf(route('/c/ideas/p/x-0f3a9c1e'), { channels })).toEqual({ view: 'chat', channelId: 'f1', postRef: 'x-0f3a9c1e' })
    expect(targetOf(route('/c/c1'), { channels })).toEqual({ view: 'chat', channelId: 'c1' })
    expect(targetOf(route('/c/aaaaaaaa-0000-4000-8000-000000000001'), { channels, dms })).toEqual({ view: 'chat', channelId: dms[0]?.id })
    expect(targetOf(route('/c/nope'), { channels })).toBeNull()
    expect(targetOf(route('/dm/nobody'), { dms })).toBeNull()
    expect(targetOf(route('/admin/users'))).toEqual({ view: 'admin', tab: 'users' })
  })
})
