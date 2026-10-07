import { required } from '../store-test-support.fixture'
import { channelFixture, categoryFixture } from '../test-fixtures.fixture'
import type { EffectScope } from 'vue'
import type { Category, Channel, LayoutRequest } from '../types/domain'
import type { ChannelTree } from '../lib/channelTree'
import type { ApiOptions } from '../lib/api'
type Tree = ChannelTree<Channel, Category>
interface Put { json: LayoutRequest; resolve(value: unknown): void; reject(reason: unknown): void }
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { effectScope } from 'vue'
import { setLocale } from '../i18n'

// PUTs wait until the test answers them; GET /api/channels returns `server`.
const h = vi.hoisted(() => ({ puts: [] as Put[], server: null as Tree | null }))
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: vi.fn((url: string, opts: ApiOptions = {}) => {
    if (url === '/api/channels') return Promise.resolve(JSON.parse(JSON.stringify(h.server)))
    if (url === '/api/admin/layout') {
      return new Promise<unknown>((resolve, reject) => h.puts.push({ json: opts.json as LayoutRequest, resolve, reject }))
    }
    return Promise.resolve(null)
  })
}))

import { api } from '../lib/api'
import { useChannelLayout, UNDO_MS } from './useChannelLayout'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { moveChannel, moveCategory, applyLayoutPayload } from '../lib/channelLayout'

const ch = (id: string, category_id: string | null, sort_order: number) => channelFixture({ id, name: id, type: 'text', category_id, sort_order })

function serverData() {
  return {
    uncategorized: [ch('u1', null, 0)],
    categories: [
      categoryFixture({ id: 'A', name: 'Alpha', sort_order: 0, channels: [ch('a1', 'A', 0), ch('a2', 'A', 1)] }),
      categoryFixture({ id: 'B', name: 'Beta', sort_order: 1, channels: [ch('b1', 'B', 0)] })
    ]
  }
}

// "u1 | A: a1 a2 | B: b1"
const shape = (t: Tree) => [t.uncategorized.map(c => c.id).join(' '), ...t.categories.map(c => `${c.id}: ${c.channels.map(x => x.id).join(' ')}`)].join(' | ')

const flush = async () => {
  for (let i = 0; i < 5; i++) await new Promise(r => setTimeout(r, 0))
}

// What the server does with a layout: new places and dense sort orders.
function store(payload: LayoutRequest) {
  const t = applyLayoutPayload(required(h.server), payload)
  t.categories.forEach((c, i) => {
    c.sort_order = i
    c.channels = c.channels.map((x, j) => ({ ...x, category_id: c.id, sort_order: j }))
  })
  t.uncategorized = t.uncategorized.map((x, j) => ({ ...x, category_id: null, sort_order: j }))
  h.server = JSON.parse(JSON.stringify(t))
}

// Answers the oldest PUT like the server: store it, then succeed or fail.
async function answerPut(ok = true) {
  const put = h.puts.shift()
  if (!put) throw new Error('no PUT in flight')
  if (ok) {
    store(put.json)
    put.resolve(null)
  } else {
    put.reject(new Error('Nicht gefunden'))
  }
  await flush()
  return put.json
}

let scope: EffectScope | undefined
let layoutApi: ReturnType<typeof useChannelLayout>
let chat: ReturnType<typeof useChatStore>
let toasts: ReturnType<typeof useToastStore>
function setup() {
  chat = useChatStore()
  toasts = useToastStore()
  h.server = serverData()
  chat.categories = serverData().categories
  chat.uncategorized = serverData().uncategorized
  scope = effectScope()
  layoutApi = required(scope.run(() => useChannelLayout()))
  return layoutApi
}

beforeEach(() => {
  setLocale('de')
  setActivePinia(createPinia())
  h.puts.length = 0
  vi.mocked(api).mockClear()
})
afterEach(() => {
  scope?.stop()
  vi.useRealTimers()
})

describe('immediate save', () => {
  it('supports callers without an effect scope', () => {
    setup()
    const outside = useChannelLayout()
    expect(shape(outside.layout.value)).toBe('u1 | A: a1 a2 | B: b1')
  })
  it('shows a move at once, saves the whole layout and confirms with an undo toast', async () => {
    const { layout, commit, saving } = setup()
    const done = commit(moveChannel(layout.value, 'b1', 'A', 0), { toast: 'Kanal verschoben' })
    expect(shape(layout.value)).toBe('u1 | A: b1 a1 a2 | B: ')
    expect(saving.value).toBe(true)
    expect(required(required(h.puts[0]).json.channels).filter(c => c.category_id === 'A')).toEqual([
      { id: 'b1', category_id: 'A', sort_order: 0 },
      { id: 'a1', category_id: 'A', sort_order: 1 },
      { id: 'a2', category_id: 'A', sort_order: 2 }
    ])

    await answerPut()
    await expect(done).resolves.toBe(true)
    expect(saving.value).toBe(false)
    expect(shape(layout.value)).toBe('u1 | A: b1 a1 a2 | B: ')
    // The store has the saved order itself now.
    expect(required(chat.categories[0]).channels.map(c => c.id)).toEqual(['b1', 'a1', 'a2'])

    const toast = required(toasts.toasts.at(-1))
    expect(toast).toMatchObject({ type: 'success', text: 'Kanal verschoben' })
    expect(required(toast.action).label).toBe('Rückgängig')
  })

  it('does nothing for a move that changes nothing', async () => {
    const { layout, commit } = setup()
    await expect(commit(moveChannel(layout.value, 'a1', 'A', 0))).resolves.toBe(true)
    expect(h.puts).toHaveLength(0)
    expect(toasts.toasts).toHaveLength(0)
  })

  it('saves silently without a toast text', async () => {
    const { layout, commit } = setup()
    const done = commit(moveCategory(layout.value, 'B', 0))
    await answerPut()
    await done
    expect(toasts.toasts).toHaveLength(0)
  })
})

describe('rollback', () => {
  it('reports a non-Error failure without leaking it into toast details', async () => {
    const { layout, commit } = setup()
    const done = commit(moveCategory(layout.value, 'B', 0))
    required(h.puts.shift()).reject('failed')
    await flush()
    expect(await done).toBe(false)
    expect(toasts.toasts.at(-1)?.detail).toBe('')
  })
  it('restores the previous layout and reports the error', async () => {
    const { layout, commit } = setup()
    const done = commit(moveCategory(layout.value, 'B', 0), { toast: 'x' })
    expect(shape(layout.value)).toBe('u1 | B: b1 | A: a1 a2')
    await answerPut(false)
    await expect(done).resolves.toBe(false)
    expect(shape(layout.value)).toBe('u1 | A: a1 a2 | B: b1')
    const errors = toasts.toasts.filter(t => t.type === 'error')
    expect(errors).toHaveLength(1)
    expect(required(errors[0]).text).toBe('Die neue Reihenfolge konnte nicht gespeichert werden.')
    expect(required(errors[0]).detail).toBe('Nicht gefunden')
    expect(toasts.toasts.some(t => t.action)).toBe(false)
  })
})

describe('serialized saves', () => {
  it('keeps one save in flight and sends only the latest queued layout next', async () => {
    const { layout, commit } = setup()
    const first = commit(moveChannel(layout.value, 'a2', 'A', 0), { toast: 'eins' })
    const second = commit(moveChannel(layout.value, 'u1', 'B', 1), { toast: 'zwei' })
    const third = commit(moveCategory(layout.value, 'B', 0), { toast: 'drei' })
    expect(h.puts).toHaveLength(1)
    expect(shape(layout.value)).toBe(' | B: b1 u1 | A: a2 a1')

    await answerPut()
    expect(h.puts).toHaveLength(1) // the queued one, sent only now
    expect(required(h.puts[0]).json.categories).toEqual([{ id: 'B', sort_order: 0 }, { id: 'A', sort_order: 1 }])
    expect(shape(layout.value)).toBe(' | B: b1 u1 | A: a2 a1')
    await answerPut()

    expect(await Promise.all([first, second, third])).toEqual([true, true, true])
    expect(h.puts).toHaveLength(0)
    expect(shape(layout.value)).toBe(' | B: b1 u1 | A: a2 a1')
    // One undo toast, for the latest move.
    expect(toasts.toasts.filter(t => t.action).map(t => t.text)).toEqual(['drei'])
  })

  it('a failed save drops the queued layout and returns to the last saved one', async () => {
    const { layout, commit } = setup()
    const first = commit(moveChannel(layout.value, 'a2', 'A', 0))
    await answerPut()
    await first
    const second = commit(moveCategory(layout.value, 'B', 0))
    const third = commit(moveChannel(layout.value, 'u1', 'B', 0))
    await answerPut(false)
    expect(await Promise.all([second, third])).toEqual([false, false])
    expect(h.puts).toHaveLength(0)
    expect(shape(layout.value)).toBe('u1 | A: a2 a1 | B: b1')
  })

  it('a failure after a successful save rolls back to that save', async () => {
    const { layout, commit } = setup()
    const first = commit(moveChannel(layout.value, 'a2', 'A', 0))
    const second = commit(moveCategory(layout.value, 'B', 0))
    expect(h.puts).toHaveLength(1)
    await answerPut() // first goes through …
    await answerPut(false) // … the queued second fails
    expect(await Promise.all([first, second])).toEqual([true, false])
    expect(shape(layout.value)).toBe('u1 | A: a2 a1 | B: b1')
  })

  it('a queued layout leaves out channels deleted while it waited', async () => {
    const { layout, commit } = setup()
    commit(moveChannel(layout.value, 'a2', 'A', 0))
    const queuedMove = commit(moveCategory(layout.value, 'B', 0))
    required(required(h.server).categories[0]).channels = required(required(h.server).categories[0]).channels.filter(c => c.id !== 'a1')
    await chat.fetchChannels()
    await answerPut()
    expect(required(required(h.puts[0]).json.channels).map(c => c.id)).toEqual(['u1', 'b1', 'a2'])
    await answerPut()
    await expect(queuedMove).resolves.toBe(true)
  })

  it('a refetch while saving brings new data but keeps the pending order', async () => {
    const { layout, commit } = setup()
    commit(moveCategory(layout.value, 'B', 0))
    // channels_changed from someone else: old order, a rename, a new channel.
    required(required(h.server).categories[0]).name = 'Alpha neu'
    required(required(h.server).categories[1]).channels.push(ch('b2', 'B', 1))
    await chat.fetchChannels()
    expect(shape(layout.value)).toBe('u1 | B: b1 b2 | A: a1 a2')
    expect(required(layout.value.categories[1]).name).toBe('Alpha neu')
    await answerPut()
    expect(shape(layout.value)).toBe('u1 | B: b1 b2 | A: a1 a2')
  })
})

describe('undo', () => {
  it('keeps the saved order and reports a failed undo without a restored toast', async () => {
    const { layout, commit } = setup()
    const done = commit(moveCategory(layout.value, 'B', 0), { toast: 'x' })
    await answerPut()
    await done
    const undo = required(required(toasts.toasts.find(toast => toast.action)).action).onClick()
    await answerPut(false)
    await undo
    expect(shape(layout.value)).toBe('u1 | B: b1 | A: a1 a2')
    expect(toasts.toasts.map(toast => toast.text)).not.toContain('Reihenfolge wiederhergestellt')
  })
  it('restores the layout before the move with another save', async () => {
    const { layout, commit } = setup()
    const done = commit(moveChannel(layout.value, 'b1', null, 0), { toast: 'Kanal verschoben' })
    await answerPut()
    await done
    const toast = required(toasts.toasts.find(t => t.action))
    required(toast.action).onClick()
    toasts.dismiss(toast.id)
    expect(shape(layout.value)).toBe('u1 | A: a1 a2 | B: b1')
    const sent = await answerPut()
    expect(required(sent.channels).find(c => c.id === 'b1')).toEqual({ id: 'b1', category_id: 'B', sort_order: 0 })
    expect(toasts.toasts.map(t => t.text)).toContain('Reihenfolge wiederhergestellt')
    // The undo itself offers no further undo.
    expect(toasts.toasts.some(t => t.action)).toBe(false)
  })

  it('skips channels deleted since the move', async () => {
    const { layout, commit } = setup()
    const done = commit(moveChannel(layout.value, 'a1', 'B', 1), { toast: 'x' })
    await answerPut()
    await done
    required(required(h.server).categories[0]).channels = required(required(h.server).categories[0]).channels.filter(c => c.id !== 'a2')
    await chat.fetchChannels()
    required(required(toasts.toasts.find(t => t.action)).action).onClick()
    const sent = await answerPut()
    expect(required(sent.channels).map(c => c.id)).toEqual(['u1', 'a1', 'b1'])
  })

  it('the undo toast goes away on its own and is replaced by a newer one', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    const { layout, commit } = setup()
    const save = async (next: Tree, toast: string) => {
      const done = commit(next, { toast })
      const put = required(h.puts.shift())
      store(put.json)
      put.resolve(null)
      await vi.advanceTimersByTimeAsync(10)
      await done
    }
    await save(moveCategory(layout.value, 'B', 0), 'eins')
    await save(moveCategory(layout.value, 'B', 1), 'zwei')
    expect(toasts.toasts.filter(t => t.action).map(t => t.text)).toEqual(['zwei'])
    await vi.advanceTimersByTimeAsync(UNDO_MS)
    expect(toasts.toasts.filter(t => t.action)).toHaveLength(0)
  })

  it('leaving the sidebar takes the undo toast along', async () => {
    const { layout, commit } = setup()
    const done = commit(moveCategory(layout.value, 'B', 0), { toast: 'x' })
    await answerPut()
    await done
    required(scope).stop()
    expect(toasts.toasts.some(t => t.action)).toBe(false)
  })
})
