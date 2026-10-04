import { describe, it, expect } from 'vitest'
import { buildChannelTree, loadCollapsed, saveCollapsed, COLLAPSED_KEY } from './channelTree'

function memoryStorage() {
  const data = {}
  return { getItem: k => (k in data ? data[k] : null), setItem: (k, v) => { data[k] = String(v) }, data }
}

describe('channel tree', () => {
  it('keeps uncategorized first and sorts categories and channels by sort_order', () => {
    const tree = buildChannelTree(
      [
        { id: 'voice', sort_order: 2, channels: [{ id: 'g', type: 'voice', sort_order: 1 }, { id: 'l', type: 'voice', sort_order: 0 }] },
        { id: 'text', sort_order: 1, channels: [{ id: 'gen', type: 'text', sort_order: 0 }, { id: 'v', type: 'voice', sort_order: 1 }] }
      ],
      [{ id: 'u2', sort_order: 1 }, { id: 'u1', sort_order: 0 }]
    )
    expect(tree.uncategorized.map(c => c.id)).toEqual(['u1', 'u2'])
    expect(tree.categories.map(c => c.id)).toEqual(['text', 'voice'])
    expect(tree.categories[0].channels.map(c => c.id)).toEqual(['gen', 'v'])
    expect(tree.categories[1].channels.map(c => c.id)).toEqual(['l', 'g'])
  })

  it('keeps API order when sort_order is missing or equal', () => {
    const tree = buildChannelTree([{ id: 'a', channels: [{ id: 'x' }, { id: 'y' }] }, { id: 'b' }], null)
    expect(tree.categories.map(c => c.id)).toEqual(['a', 'b'])
    expect(tree.categories[0].channels.map(c => c.id)).toEqual(['x', 'y'])
    expect(tree.categories[1].channels).toEqual([])
    expect(tree.uncategorized).toEqual([])
  })

  it('persists collapsed categories and tolerates bad storage', () => {
    const s = memoryStorage()
    expect(loadCollapsed(s)).toEqual([])
    saveCollapsed(['a', 'b'], s)
    expect(loadCollapsed(s)).toEqual(['a', 'b'])
    s.data[COLLAPSED_KEY] = '{oops'
    expect(loadCollapsed(s)).toEqual([])
    const throwing = { getItem() { throw new Error('x') }, setItem() { throw new Error('x') } }
    expect(loadCollapsed(throwing)).toEqual([])
    expect(() => saveCollapsed(['a'], throwing)).not.toThrow()
  })
})
