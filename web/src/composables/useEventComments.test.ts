import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
import { applyChange, useEventComments } from './useEventComments'
import { useEventCommentsStore } from '../stores/eventComments'
import { useToastStore } from '../stores/toast'
import * as api from '../lib/eventCommentsApi'
import { setLocale } from '../i18n'
import { commentFixture } from '../eventRoles.fixture'
import { fixtureId } from '../test-fixtures.fixture'

vi.mock('../lib/eventCommentsApi', () => ({
  fetchComments: vi.fn(), postComment: vi.fn(), editComment: vi.fn(), deleteComment: vi.fn(), setCommentsMuted: vi.fn()
}))
vi.mock('../lib/eventsDialog', () => ({ openEvents: vi.fn() }))

const [c1, c2, c3] = [commentFixture(1), commentFixture(2), commentFixture(3)]
const page = (comments = [c2, c3], has_more = true, muted = false) => ({ comments, has_more, muted })

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  vi.clearAllMocks()
})

describe('applyChange', () => {
  it('adds a created comment once, replaces an edited one and drops a deleted one', () => {
    const base = { event_id: fixtureId(1), comment_id: c1.id }
    expect(applyChange([c2], { ...base, action: 'created', comment: c1 })).toEqual([c2, c1])
    expect(applyChange([c1], { ...base, action: 'created', comment: c1 })).toEqual([c1])
    const edited = { ...c1, content: 'new' }
    expect(applyChange([c1, c2], { ...base, action: 'updated', comment: edited })).toEqual([edited, c2])
    expect(applyChange([c1, c2], { ...base, action: 'deleted' })).toEqual([c2])
    expect(applyChange([c1], { ...base, action: 'created' })).toEqual([c1])
  })
})

describe('useEventComments', () => {
  it('loads the newest page with the mute state, then older pages without repeating one', async () => {
    vi.mocked(api.fetchComments).mockResolvedValueOnce(page([c2, c3], true, true))
    const list = useEventComments(ref(fixtureId(1)))
    await list.load()
    expect(list.comments.value).toEqual([c2, c3])
    expect(list.hasMore.value && list.muted.value).toBe(true)

    vi.mocked(api.fetchComments).mockResolvedValueOnce(page([c1, c2], false))
    await list.loadOlder()
    expect(api.fetchComments).toHaveBeenLastCalledWith(fixtureId(1), c2.id)
    expect(list.comments.value).toEqual([c1, c2, c3])
    expect(list.hasMore.value).toBe(false)
  })

  it('says so when the comments cannot be loaded, and toasts a failed older page', async () => {
    vi.mocked(api.fetchComments).mockRejectedValueOnce(new Error('down'))
    const list = useEventComments(ref(fixtureId(1)))
    await list.load()
    expect(list.failed.value).toBe(true)
    expect(list.loading.value).toBe(false)

    await list.loadOlder() // nothing loaded: nothing to page from
    expect(api.fetchComments).toHaveBeenCalledTimes(1)

    vi.mocked(api.fetchComments).mockResolvedValueOnce(page())
    await list.load()
    vi.mocked(api.fetchComments).mockRejectedValueOnce(new Error('older failed'))
    await list.loadOlder()
    expect(useToastStore().toasts.map(t => t.text).join()).toContain('older failed')
    expect(list.loadingMore.value).toBe(false)
  })

  it('drops the answer of a load that was overtaken', async () => {
    let release!: (v: ReturnType<typeof page>) => void
    vi.mocked(api.fetchComments).mockImplementationOnce(() => new Promise(r => { release = r }))
    const list = useEventComments(ref(fixtureId(1)))
    const slow = list.load()
    vi.mocked(api.fetchComments).mockResolvedValueOnce(page([c3], false))
    await list.load()
    release(page([c1]))
    await slow
    expect(list.comments.value).toEqual([c3])
  })

  it('sends, edits, deletes and mutes, each once at a time, and shows a failure calmly', async () => {
    const list = useEventComments(ref(fixtureId(1)))
    vi.mocked(api.postComment).mockResolvedValueOnce(c1)
    expect(await list.send('hi')).toBe(true)
    expect(list.comments.value).toEqual([c1])

    vi.mocked(api.editComment).mockResolvedValueOnce({ ...c1, content: 'changed' })
    expect(await list.edit(c1.id, 'changed')).toBe(true)
    expect(list.comments.value[0]!.content).toBe('changed')

    vi.mocked(api.setCommentsMuted).mockResolvedValueOnce(true)
    await list.toggleMute()
    expect(api.setCommentsMuted).toHaveBeenLastCalledWith(fixtureId(1), true)
    expect(list.muted.value).toBe(true)

    vi.mocked(api.deleteComment).mockRejectedValueOnce(new Error('no way'))
    expect(await list.remove(c1.id)).toBe(false)
    expect(useToastStore().toasts.map(t => t.text).join()).toContain('no way')
    vi.mocked(api.deleteComment).mockResolvedValueOnce()
    expect(await list.remove(c1.id)).toBe(true)
    expect(list.comments.value).toEqual([])

    let finish!: (c: typeof c1) => void
    vi.mocked(api.postComment).mockImplementationOnce(() => new Promise(r => { finish = r }))
    const first = list.send('one')
    expect(await list.send('two')).toBe(false)
    finish(c2)
    await first
    expect(api.postComment).toHaveBeenCalledTimes(2)
  })

  it('does not add a sent comment that the live update brought already', async () => {
    const list = useEventComments(ref(fixtureId(1)))
    useEventCommentsStore().handleChange({ action: 'created', event_id: fixtureId(1), comment_id: c1.id, comment: c1 })
    await flushPromises()
    vi.mocked(api.postComment).mockResolvedValueOnce(c1)
    await list.send('x')
    expect(list.comments.value).toEqual([c1])
  })

  it('follows live changes of its own event only, and starts over for another event', async () => {
    const id = ref(fixtureId(1))
    vi.mocked(api.fetchComments).mockResolvedValue(page([c2], false))
    const list = useEventComments(id)
    const live = useEventCommentsStore()
    live.handleChange({ action: 'created', event_id: fixtureId(9), comment_id: c1.id, comment: c1 })
    await flushPromises()
    expect(list.comments.value).toEqual([])
    live.handleChange({ action: 'created', event_id: fixtureId(1), comment_id: c1.id, comment: c1 })
    await flushPromises()
    expect(list.comments.value).toEqual([c1])
    id.value = fixtureId(5)
    await flushPromises()
    expect(api.fetchComments).toHaveBeenLastCalledWith(fixtureId(5))
    expect(list.comments.value).toEqual([c2])
  })
})
