import type { ApiOptions } from './api'

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { deleteReminder, deleteScheduled, fetchReminders, fetchScheduled, patchScheduled, postReminder, postScheduled } from './laterApi'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('./api', async importOriginal => ({
  ...(await importOriginal<typeof import('./api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const at = new Date('2026-10-25T08:00:00Z')

beforeEach(() => {
  apiMock.mockReset()
  apiMock.mockResolvedValue({ items: [] })
})

describe('reminder and scheduled message requests', () => {
  it('lists both kinds of entries', async () => {
    expect(await fetchReminders()).toEqual([])
    expect(await fetchScheduled()).toEqual([])
    expect(apiMock.mock.calls.map(c => c[0])).toEqual(['/api/me/reminders', '/api/me/scheduled'])
  })

  it('posts a reminder and deletes one', async () => {
    await postReminder('m1', at)
    expect(apiMock).toHaveBeenLastCalledWith('/api/me/reminders', expect.objectContaining({ method: 'POST', json: { message_id: 'm1', remind_at: at.toISOString() } }))
    await deleteReminder('r1')
    expect(apiMock).toHaveBeenLastCalledWith('/api/me/reminders/r1', { method: 'DELETE' })
  })

  it('sends only the thread and reply targets that are set', async () => {
    await postScheduled('c1', 'hi', at)
    expect(apiMock.mock.lastCall?.[1]?.json).toEqual({ content: 'hi', send_at: at.toISOString() })
    await postScheduled('c1', 'hi', at, { parentId: 'p1', replyToId: null })
    expect(apiMock.mock.lastCall?.[1]?.json).toEqual({ content: 'hi', send_at: at.toISOString(), parent_id: 'p1' })
    await postScheduled('c1', 'hi', at, { parentId: null, replyToId: 'r1' })
    expect(apiMock.mock.lastCall?.[1]?.json).toEqual({ content: 'hi', send_at: at.toISOString(), reply_to_id: 'r1' })
    expect(apiMock).toHaveBeenLastCalledWith('/api/channels/c1/scheduled', expect.anything())
  })

  it('patches only what changed, including an emptied text', async () => {
    await patchScheduled('s1', { content: '' })
    expect(apiMock.mock.lastCall?.[1]?.json).toEqual({ content: '' })
    await patchScheduled('s1', { at })
    expect(apiMock.mock.lastCall?.[1]?.json).toEqual({ send_at: at.toISOString() })
    await patchScheduled('s1', {})
    expect(apiMock.mock.lastCall?.[1]?.json).toEqual({})
    await deleteScheduled('s1')
    expect(apiMock).toHaveBeenLastCalledWith('/api/me/scheduled/s1', { method: 'DELETE' })
  })
})
