import { describe, it, expect, beforeEach } from 'vitest'
import { parseRoute, buildRoute, savePendingRoute, consumePendingRoute, navigate, popRedirectRoute, currentRoute } from './router'

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

  it('builds URL from route object', () => {
    expect(buildRoute({ view: 'chat', channelId: 'c1' })).toBe('/c/c1')
    expect(buildRoute({ view: 'chat', channelId: 'c1', messageId: 'm1' })).toBe('/c/c1/m/m1')
    expect(buildRoute({ view: 'voice', channelId: 'v1' })).toBe('/v/v1')
    expect(buildRoute({ view: 'voice', channelId: 'v1', showChat: true })).toBe('/v/v1/chat')
    expect(buildRoute({ view: 'admin', tab: 'users' })).toBe('/admin/users')
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
})
