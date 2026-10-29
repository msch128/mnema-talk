import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ref } from 'vue'
import { applyRoute, type RoutingContext } from './appRoutingApply'
import { parseRoute } from '../lib/router'

const navigate = vi.hoisted(() => vi.fn())
const fetchForumPosts = vi.hoisted(() => vi.fn())
vi.mock('../lib/router', async importOriginal => ({ ...(await importOriginal<typeof import('../lib/router')>()), navigate }))
vi.mock('../lib/forumApi', () => ({ fetchForumPosts }))

const POST = '0f3a9c1e-1111-4222-8333-444455556666'
const OTHER = '77777777-1111-4222-8333-444455556666'
const DM = 'aaaaaaaa-0000-4000-8000-000000000001'

function makeCtx() {
  const channels = [
    { id: 'c1', type: 'text', slug: 'general', previous_slugs: ['old-name'] },
    { id: 'f1', type: 'forum', slug: 'ideas', previous_slugs: [] },
    { id: 'v1', type: 'voice', slug: 'lounge', previous_slugs: [] },
  ]
  const chatStore = {
    allChannels: channels,
    dms: [{ id: DM, group: false, members: [{ id: 'me', username: 'me' }, { id: 'u2', username: 'alice' }] }],
    activeChannel: null as { id: string } | null,
    activeThread: null as { id: string; title?: string; user_id?: string; content?: string } | null,
    dmChannel: (id: string) => (id === DM ? { id: DM, type: 'text' } : null),
    selectChannel: vi.fn(async (c: { id: string }) => { chatStore.activeChannel = c }),
    openThread: vi.fn(async (id: string) => { chatStore.activeThread = { id, title: 'Neue Küche?', user_id: 'u1', content: 'hi' } }),
    closeThread: vi.fn(),
    jumpToMessage: vi.fn(async () => true),
  }
  const toasts = { error: vi.fn() }
  const ctx = {
    alive: () => true,
    authStore: { isAuthenticated: true, user: { id: 'me' } },
    permissions: { canOpenAdmin: false },
    chatStore,
    voiceStore: { activeView: 'chat', currentChannelId: null },
    toasts,
    showAdminModal: ref(false),
    adminTab: ref('users'),
    previewVoiceChannelId: ref<string | null>(null),
    voiceShowChat: ref(false),
    currentStatePath: () => '/',
    lastVoiceRouteChannel: null,
    isSyncingFromRoute: false,
    routesApplying: 0,
    loadingFirstRoute: false,
    routeGen: 0,
  } as unknown as RoutingContext
  return { ctx, chatStore, toasts }
}

beforeEach(() => {
  navigate.mockReset()
  fetchForumPosts.mockReset()
})

describe('applying a speaking address', () => {
  it('rewrites a former slug and a legacy id to the canonical address', async () => {
    const { ctx, chatStore } = makeCtx()
    expect(await applyRoute(ctx, parseRoute('/c/old-name/t/t1'))).toBe(true)
    expect(navigate).toHaveBeenCalledWith('/c/general/t/t1', { replace: true })
    expect(await applyRoute(ctx, parseRoute('/c/c1'))).toBe(true)
    expect(navigate).toHaveBeenCalledWith('/c/general', { replace: true })
    expect(chatStore.selectChannel).not.toHaveBeenCalled()
  })

  it('shows the same message for an unknown and an invisible channel', async () => {
    const { ctx, toasts } = makeCtx()
    await applyRoute(ctx, parseRoute('/c/secret'))
    await applyRoute(ctx, parseRoute('/c/00000000-0000-4000-8000-000000000000'))
    expect(toasts.error).toHaveBeenCalledTimes(2)
    expect(toasts.error.mock.calls[0]).toEqual(toasts.error.mock.calls[1])
    expect(navigate).toHaveBeenCalledWith('/', { replace: true })
  })

  it('selects the channel behind the slug and the conversation behind the member', async () => {
    const { ctx, chatStore } = makeCtx()
    expect(await applyRoute(ctx, parseRoute('/c/general/m/m1'))).toBe(false)
    expect(chatStore.selectChannel).toHaveBeenLastCalledWith(expect.objectContaining({ id: 'c1' }))
    expect(chatStore.jumpToMessage).toHaveBeenCalledWith('m1')
    await applyRoute(ctx, parseRoute('/dm/alice'))
    expect(chatStore.selectChannel).toHaveBeenLastCalledWith(expect.objectContaining({ id: DM }))
    await applyRoute(ctx, parseRoute('/dm/nobody'))
    expect(navigate).toHaveBeenLastCalledWith('/', { replace: true })
  })

  it('opens a Talk by slug as a preview', async () => {
    const { ctx } = makeCtx()
    await applyRoute(ctx, parseRoute('/v/lounge'))
    expect(ctx.previewVoiceChannelId.value).toBe('v1')
  })
})

describe('forum post addresses', () => {
  it('opens the post whose id starts with the short id and gives the thread its speaking address', async () => {
    const { ctx, chatStore } = makeCtx()
    fetchForumPosts.mockResolvedValue([{ id: POST }, { id: OTHER }])
    await applyRoute(ctx, parseRoute('/c/ideas/p/neue-kueche-0f3a9c1e'))
    expect(fetchForumPosts).toHaveBeenCalledWith('f1', { sort: 'newest', limit: 100 })
    expect(chatStore.openThread).toHaveBeenCalledWith(POST)
    expect(navigate).toHaveBeenLastCalledWith('/c/ideas/p/neue-kueche-0f3a9c1e', { replace: true })
  })

  it('takes a full id without asking the server which posts exist', async () => {
    const { ctx, chatStore } = makeCtx()
    await applyRoute(ctx, parseRoute(`/c/ideas/p/${POST}`))
    expect(fetchForumPosts).not.toHaveBeenCalled()
    expect(chatStore.openThread).toHaveBeenCalledWith(POST)
  })

  it('pages back through older posts', async () => {
    const { ctx, chatStore } = makeCtx()
    const page = Array.from({ length: 100 }, (_, i) => ({ id: `${String(i).padStart(8, '1')}-1111-4222-8333-444455556666` }))
    fetchForumPosts.mockResolvedValueOnce(page).mockResolvedValueOnce([{ id: POST }])
    await applyRoute(ctx, parseRoute('/c/ideas/p/x-0f3a9c1e'))
    expect(fetchForumPosts).toHaveBeenLastCalledWith('f1', { sort: 'newest', limit: 100, before: page[99]?.id })
    expect(chatStore.openThread).toHaveBeenCalledWith(POST)
  })

  it('finds a post far back in a large forum', async () => {
    const { ctx, chatStore } = makeCtx()
    const page = (n: number) => Array.from({ length: 100 }, (_, i) => ({ id: `${String(n * 100 + i).padStart(8, '2')}-1111-4222-8333-444455556666` }))
    for (let n = 0; n < 8; n++) fetchForumPosts.mockResolvedValueOnce(page(n))
    fetchForumPosts.mockResolvedValueOnce([{ id: POST }])
    await applyRoute(ctx, parseRoute('/c/ideas/p/x-0f3a9c1e'))
    expect(fetchForumPosts).toHaveBeenCalledTimes(9)
    expect(chatStore.openThread).toHaveBeenCalledWith(POST)
  })

  it('stops paging for an unknown short id instead of walking the whole forum', async () => {
    const { ctx, chatStore } = makeCtx()
    let n = 0
    fetchForumPosts.mockImplementation(async () => Array.from({ length: 100 }, () => ({ id: `${String(n++).padStart(8, '2')}-1111-4222-8333-444455556666` })))
    await applyRoute(ctx, parseRoute('/c/ideas/p/x-deadbeef'))
    expect(fetchForumPosts).toHaveBeenCalledTimes(50)
    expect(chatStore.openThread).not.toHaveBeenCalled()
  })

  it.each([
    ['an unknown post', [{ id: OTHER }]],
    ['an ambiguous short id', [{ id: POST }, { id: '0f3a9c1e-2222-4222-8333-444455556666' }]],
  ])('falls back to the forum for %s', async (_name, posts) => {
    const { ctx, chatStore, toasts } = makeCtx()
    fetchForumPosts.mockResolvedValue(posts)
    await applyRoute(ctx, parseRoute('/c/ideas/p/x-0f3a9c1e'))
    expect(chatStore.openThread).not.toHaveBeenCalled()
    expect(toasts.error).toHaveBeenCalled()
    expect(navigate).toHaveBeenLastCalledWith('/c/ideas', { replace: true })
  })

  it('falls back when the posts cannot be read or the address has no id', async () => {
    const { ctx, toasts } = makeCtx()
    fetchForumPosts.mockRejectedValue(new Error('down'))
    await applyRoute(ctx, parseRoute('/c/ideas/p/x-0f3a9c1e'))
    await applyRoute(ctx, parseRoute('/c/ideas/p/no-id'))
    expect(toasts.error).toHaveBeenCalledTimes(2)
    expect(navigate).toHaveBeenLastCalledWith('/c/ideas', { replace: true })
  })
})
