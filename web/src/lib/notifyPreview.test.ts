import { beforeEach, describe, expect, it, vi } from 'vitest'

const reply = vi.hoisted(() => ({ pending: [] as Array<(v: unknown) => void> }))
vi.mock('./api', () => ({ api: vi.fn(() => new Promise(resolve => { reply.pending.push(resolve) })) }))
vi.mock('../types/rest', () => ({ decodePushConfig: (v: unknown) => v }))

import { applyPushConfig, loadNotifyPreview, notifyShowsText, resetNotifyPreview } from './notifyPreview'

const on = { preview: true, preview_allowed: true }

beforeEach(() => {
  reply.pending = []
  resetNotifyPreview()
})

describe('notify preview', () => {
  it('hides the text again after a reset', () => {
    applyPushConfig(on)
    expect(notifyShowsText.value).toBe(true)
    resetNotifyPreview()
    expect(notifyShowsText.value).toBe(false)
  })
  it('lets a newer choice win over an older answer still in flight', async () => {
    const loading = loadNotifyPreview()
    applyPushConfig({ preview: false, preview_allowed: true })
    reply.pending[0]?.(on)
    await loading
    expect(notifyShowsText.value).toBe(false)
  })
  it('drops an answer that arrives after sign-out', async () => {
    const loading = loadNotifyPreview()
    resetNotifyPreview()
    reply.pending[0]?.(on)
    await loading
    expect(notifyShowsText.value).toBe(false)
  })
  it('applies the latest answer', async () => {
    const loading = loadNotifyPreview()
    reply.pending[0]?.(on)
    await loading
    expect(notifyShowsText.value).toBe(true)
  })
})
