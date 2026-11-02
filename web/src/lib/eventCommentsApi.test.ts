import { describe, it, expect, beforeEach, vi } from 'vitest'
import { api } from './api'
import { fetchComments, postComment, editComment, deleteComment, setCommentsMuted } from './eventCommentsApi'

vi.mock('./api', () => ({ api: vi.fn() }))
const apiMock = vi.mocked(api) as unknown as ReturnType<typeof vi.fn>

type Opts = { method?: string; json?: unknown; decode?: (v: unknown) => unknown }
function last(): [string, Opts] {
  const call = apiMock.mock.calls.at(-1)
  if (!call) throw new Error('api was not called')
  return [call[0] as string, (call[1] ?? {}) as Opts]
}

beforeEach(() => apiMock.mockReset())

describe('event comments api', () => {
  it('asks for the newest page, or the page before a comment', async () => {
    await fetchComments('e/1')
    expect(last()[0]).toBe('/api/events/e%2F1/comments?limit=50')
    await fetchComments('e1', 'c 1', 20)
    expect(last()[0]).toBe('/api/events/e1/comments?limit=20&before=c+1')
    expect(() => last()[1].decode?.({ nope: true })).toThrow()
    expect(last()[1].decode?.({ comments: [], has_more: false, muted: false })).toEqual({ comments: [], has_more: false, muted: false })
  })

  it('writes, edits and deletes a comment', async () => {
    await postComment('e1', 'hello')
    expect(last()[0]).toBe('/api/events/e1/comments')
    expect(last()[1]).toMatchObject({ method: 'POST', json: { content: 'hello' } })
    await editComment('e1', 'c1', 'edited')
    expect(last()[0]).toBe('/api/events/e1/comments/c1')
    expect(last()[1]).toMatchObject({ method: 'PATCH', json: { content: 'edited' } })
    await deleteComment('e1', 'c/1')
    expect(last()[0]).toBe('/api/events/e1/comments/c%2F1')
    expect(last()[1].method).toBe('DELETE')
  })

  it('mutes and unmutes the notices and answers the state the server kept', async () => {
    apiMock.mockResolvedValue({ muted: true })
    expect(await setCommentsMuted('e1', true)).toBe(true)
    expect(last()[0]).toBe('/api/events/e1/comments/mute')
    expect(last()[1]).toMatchObject({ method: 'PUT', json: { muted: true } })
  })
})
