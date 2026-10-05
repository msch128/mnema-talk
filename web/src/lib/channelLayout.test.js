import { describe, it, expect } from 'vitest'
import {
  containers, channelsIn, locateChannel, locateCategory, moveChannel, moveCategory, gapToIndex,
  moveChannelToGap, moveCategoryToGap, channelStep, stepChannel, stepCategory, removeChannel,
  removeCategory, addCategory, nextSortOrder, toLayoutPayload, samePayload, sameLayout,
  applyLayoutPayload, resolveChannelDrop, resolveCategoryDrop
} from './channelLayout'

const ch = (id, category_id = null, type = 'text') => ({ id, name: id, type, category_id })

// u1 u2 | A: a1 a2 a3 | B: b1 | C: (empty)
function tree() {
  return {
    uncategorized: [ch('u1'), ch('u2')],
    categories: [
      { id: 'A', name: 'Alpha', channels: [ch('a1', 'A'), ch('a2', 'A', 'voice'), ch('a3', 'A')] },
      { id: 'B', name: 'Beta', channels: [ch('b1', 'B', 'voice')] },
      { id: 'C', name: 'Gamma', channels: [] }
    ]
  }
}

// Compact view: "u1 u2 | A: a1 a2 a3 | B: b1 | C:"
function shape(t) {
  return [
    t.uncategorized.map(c => c.id).join(' '),
    ...t.categories.map(c => `${c.id}:${c.channels.length ? ' ' : ''}${c.channels.map(x => x.id).join(' ')}`)
  ].join(' | ')
}

const INITIAL = 'u1 u2 | A: a1 a2 a3 | B: b1 | C:'

describe('lookups', () => {
  it('lists the uncategorized group first, then the categories', () => {
    expect(containers(tree()).map(c => c.id)).toEqual([null, 'A', 'B', 'C'])
  })

  it('finds channel lists, channels and categories', () => {
    const t = tree()
    expect(channelsIn(t, null).map(c => c.id)).toEqual(['u1', 'u2'])
    expect(channelsIn(t, 'C')).toEqual([])
    expect(channelsIn(t, 'nope')).toBeNull()
    expect(locateChannel(t, 'a2')).toMatchObject({ categoryId: 'A', index: 1, total: 3 })
    expect(locateChannel(t, 'u2')).toMatchObject({ categoryId: null, index: 1, total: 2 })
    expect(locateChannel(t, 'zz')).toBeNull()
    expect(locateCategory(t, 'B')).toMatchObject({ index: 1, total: 3 })
    expect(locateCategory(t, 'zz')).toBeNull()
  })

  it('tolerates categories without a channel list', () => {
    const t = { uncategorized: [], categories: [{ id: 'X' }] }
    expect(channelsIn(t, 'X')).toEqual([])
    expect(shape(moveChannel({ ...t, uncategorized: [ch('u')] }, 'u', 'X', 0))).toBe(' | X: u')
  })
})

describe('moveChannel', () => {
  it('reorders within a category', () => {
    expect(shape(moveChannel(tree(), 'a1', 'A', 2))).toBe('u1 u2 | A: a2 a3 a1 | B: b1 | C:')
    expect(shape(moveChannel(tree(), 'a3', 'A', 0))).toBe('u1 u2 | A: a3 a1 a2 | B: b1 | C:')
  })

  it('reorders within the uncategorized group', () => {
    expect(shape(moveChannel(tree(), 'u2', null, 0))).toBe('u2 u1 | A: a1 a2 a3 | B: b1 | C:')
  })

  it('moves between categories and sets the new category_id on a copy', () => {
    const t = tree()
    const next = moveChannel(t, 'a2', 'B', 0)
    expect(shape(next)).toBe('u1 u2 | A: a1 a3 | B: a2 b1 | C:')
    expect(locateChannel(next, 'a2').channel.category_id).toBe('B')
    expect(locateChannel(t, 'a2').channel.category_id).toBe('A')
  })

  it('moves into and out of the uncategorized group', () => {
    const out = moveChannel(tree(), 'u1', 'C', 0)
    expect(shape(out)).toBe('u2 | A: a1 a2 a3 | B: b1 | C: u1')
    expect(locateChannel(out, 'u1').channel.category_id).toBe('C')
    const back = moveChannel(tree(), 'b1', null, 1)
    expect(shape(back)).toBe('u1 b1 u2 | A: a1 a2 a3 | B: | C:')
    expect(locateChannel(back, 'b1').channel.category_id).toBeNull()
    expect(shape(moveChannel(tree(), 'b1', undefined, 0))).toBe('b1 u1 u2 | A: a1 a2 a3 | B: | C:')
  })

  it('clamps the index at both ends', () => {
    expect(shape(moveChannel(tree(), 'u1', 'A', 99))).toBe('u2 | A: a1 a2 a3 u1 | B: b1 | C:')
    expect(shape(moveChannel(tree(), 'a3', 'A', -5))).toBe('u1 u2 | A: a3 a1 a2 | B: b1 | C:')
    expect(shape(moveChannel(tree(), 'a1', 'A', 99))).toBe('u1 u2 | A: a2 a3 a1 | B: b1 | C:')
  })

  it('returns the same tree when nothing changes or ids are unknown', () => {
    const t = tree()
    expect(moveChannel(t, 'a2', 'A', 1)).toBe(t)
    expect(moveChannel(t, 'a3', 'A', 99)).toBe(t)
    expect(moveChannel(t, 'zz', 'A', 0)).toBe(t)
    expect(moveChannel(t, 'a1', 'zz', 0)).toBe(t)
  })

  it('never mutates the input', () => {
    const t = tree()
    const before = JSON.stringify(t)
    moveChannel(t, 'a1', 'B', 1)
    moveChannel(t, 'a1', 'A', 2)
    expect(JSON.stringify(t)).toBe(before)
  })

  it('keeps untouched categories and channel objects as they are', () => {
    const t = tree()
    const next = moveChannel(t, 'a1', 'A', 1)
    expect(next.categories[1]).toBe(t.categories[1])
    expect(next.uncategorized).toBe(t.uncategorized)
    expect(locateChannel(next, 'a1').channel).toBe(t.categories[0].channels[0])
  })
})

describe('moveCategory', () => {
  it('reorders categories and clamps', () => {
    expect(shape(moveCategory(tree(), 'A', 2))).toBe('u1 u2 | B: b1 | C: | A: a1 a2 a3')
    expect(shape(moveCategory(tree(), 'C', 0))).toBe('u1 u2 | C: | A: a1 a2 a3 | B: b1')
    expect(shape(moveCategory(tree(), 'B', 42))).toBe('u1 u2 | A: a1 a2 a3 | C: | B: b1')
    expect(shape(moveCategory(tree(), 'B', -1))).toBe('u1 u2 | B: b1 | A: a1 a2 a3 | C:')
  })

  it('returns the same tree for a no-op or an unknown id', () => {
    const t = tree()
    expect(moveCategory(t, 'B', 1)).toBe(t)
    expect(moveCategory(t, 'zz', 0)).toBe(t)
  })
})

describe('gaps', () => {
  it('converts a gap in the shown list into the final index', () => {
    expect(gapToIndex(0, true, 3)).toBe(2)
    expect(gapToIndex(2, true, 0)).toBe(0)
    expect(gapToIndex(1, true, 1)).toBe(1)
    expect(gapToIndex(0, false, 3)).toBe(3)
  })

  it('moves to a gap, accounting for the slot the item leaves', () => {
    expect(shape(moveChannelToGap(tree(), 'a1', 'A', 3))).toBe('u1 u2 | A: a2 a3 a1 | B: b1 | C:')
    expect(shape(moveChannelToGap(tree(), 'a1', 'A', 2))).toBe('u1 u2 | A: a2 a1 a3 | B: b1 | C:')
    expect(shape(moveChannelToGap(tree(), 'a3', 'A', 0))).toBe('u1 u2 | A: a3 a1 a2 | B: b1 | C:')
    expect(shape(moveChannelToGap(tree(), 'a1', 'B', 1))).toBe('u1 u2 | A: a2 a3 | B: b1 a1 | C:')
    expect(shape(moveCategoryToGap(tree(), 'A', 2))).toBe('u1 u2 | B: b1 | A: a1 a2 a3 | C:')
    expect(shape(moveCategoryToGap(tree(), 'C', 0))).toBe('u1 u2 | C: | A: a1 a2 a3 | B: b1')
    const t = tree()
    expect(moveChannelToGap(t, 'a1', 'A', 1)).toBe(t)
    expect(moveChannelToGap(t, 'zz', 'A', 1)).toBe(t)
    expect(moveCategoryToGap(t, 'A', 1)).toBe(t)
    expect(moveCategoryToGap(t, 'zz', 1)).toBe(t)
  })
})

describe('keyboard steps', () => {
  it('swaps with the neighbour inside a container', () => {
    expect(shape(stepChannel(tree(), 'a2', -1))).toBe('u1 u2 | A: a2 a1 a3 | B: b1 | C:')
    expect(shape(stepChannel(tree(), 'a2', 1))).toBe('u1 u2 | A: a1 a3 a2 | B: b1 | C:')
    expect(shape(stepChannel(tree(), 'u1', 1))).toBe('u2 u1 | A: a1 a2 a3 | B: b1 | C:')
  })

  it('crosses into the end of the container above', () => {
    expect(channelStep(tree(), 'b1', -1)).toEqual({ categoryId: 'A', index: 3 })
    expect(shape(stepChannel(tree(), 'b1', -1))).toBe('u1 u2 | A: a1 a2 a3 b1 | B: | C:')
    // The first channel of the first category becomes the last uncategorized one.
    expect(shape(stepChannel(tree(), 'a1', -1))).toBe('u1 u2 a1 | A: a2 a3 | B: b1 | C:')
  })

  it('crosses into the start of the container below, empty ones included', () => {
    expect(shape(stepChannel(tree(), 'u2', 1))).toBe('u1 | A: u2 a1 a2 a3 | B: b1 | C:')
    expect(shape(stepChannel(tree(), 'a3', 1))).toBe('u1 u2 | A: a1 a2 | B: a3 b1 | C:')
    expect(shape(stepChannel(tree(), 'b1', 1))).toBe('u1 u2 | A: a1 a2 a3 | B: | C: b1')
  })

  it('reaches an empty uncategorized group from the first category', () => {
    const t = { uncategorized: [], categories: [{ id: 'A', channels: [ch('a1', 'A')] }] }
    expect(shape(stepChannel(t, 'a1', -1))).toBe('a1 | A:')
    expect(shape(stepChannel(stepChannel(t, 'a1', -1), 'a1', 1))).toBe(' | A: a1')
  })

  it('stops at the very top and bottom', () => {
    const t = tree()
    expect(channelStep(t, 'u1', -1)).toBeNull()
    expect(stepChannel(t, 'u1', -1)).toBe(t)
    const last = moveChannel(t, 'b1', 'C', 0)
    expect(stepChannel(last, 'b1', 1)).toBe(last)
    expect(channelStep(t, 'zz', 1)).toBeNull()
    expect(channelStep(t, 'a1', 0)).toBeNull()
  })

  it('steps categories and stops at the ends', () => {
    const t = tree()
    expect(shape(stepCategory(t, 'B', -1))).toBe('u1 u2 | B: b1 | A: a1 a2 a3 | C:')
    expect(shape(stepCategory(t, 'B', 1))).toBe('u1 u2 | A: a1 a2 a3 | C: | B: b1')
    expect(stepCategory(t, 'A', -1)).toBe(t)
    expect(stepCategory(t, 'C', 1)).toBe(t)
    expect(stepCategory(t, 'zz', 1)).toBe(t)
    expect(stepCategory(t, 'A', 0)).toBe(t)
  })
})

describe('adding and removing', () => {
  it('removes channels and categories; orphans become uncategorized', () => {
    expect(shape(removeChannel(tree(), 'a2'))).toBe('u1 u2 | A: a1 a3 | B: b1 | C:')
    const t = removeCategory(tree(), 'A')
    expect(shape(t)).toBe('u1 u2 a1 a2 a3 | B: b1 | C:')
    expect(locateChannel(t, 'a1').channel.category_id).toBeNull()
    const same = tree()
    expect(removeChannel(same, 'zz')).toBe(same)
    expect(removeCategory(same, 'zz')).toBe(same)
  })

  it('adds a category once, at the end or at an index', () => {
    expect(shape(addCategory(tree(), { id: 'D' }))).toBe(`${INITIAL} | D:`)
    expect(shape(addCategory(tree(), { id: 'D' }, 1))).toBe('u1 u2 | A: a1 a2 a3 | D: | B: b1 | C:')
    const t = tree()
    expect(addCategory(t, { id: 'A' })).toBe(t)
    expect(addCategory(t, null)).toBe(t)
  })

  it('picks the next sort order after the highest one', () => {
    expect(nextSortOrder([])).toBe(0)
    expect(nextSortOrder(null)).toBe(0)
    expect(nextSortOrder([{ sort_order: 0 }, { sort_order: 5 }, { sort_order: 2 }])).toBe(6)
    expect(nextSortOrder([{}, {}])).toBe(0)
  })
})

describe('layout payload', () => {
  it('lists dense sort orders, uncategorized channels first', () => {
    expect(toLayoutPayload(tree())).toEqual({
      categories: [{ id: 'A', sort_order: 0 }, { id: 'B', sort_order: 1 }, { id: 'C', sort_order: 2 }],
      channels: [
        { id: 'u1', category_id: null, sort_order: 0 },
        { id: 'u2', category_id: null, sort_order: 1 },
        { id: 'a1', category_id: 'A', sort_order: 0 },
        { id: 'a2', category_id: 'A', sort_order: 1 },
        { id: 'a3', category_id: 'A', sort_order: 2 },
        { id: 'b1', category_id: 'B', sort_order: 0 }
      ]
    })
  })

  it('reflects moves', () => {
    const p = toLayoutPayload(moveChannel(moveCategory(tree(), 'B', 0), 'u1', 'B', 1))
    expect(p.categories.map(c => c.id)).toEqual(['B', 'A', 'C'])
    expect(p.channels.filter(c => c.category_id === 'B')).toEqual([
      { id: 'b1', category_id: 'B', sort_order: 0 },
      { id: 'u1', category_id: 'B', sort_order: 1 }
    ])
  })

  it('compares layouts by order only', () => {
    const t = tree()
    const renamed = { ...t, categories: t.categories.map(c => ({ ...c, name: 'x' })) }
    expect(sameLayout(t, renamed)).toBe(true)
    expect(sameLayout(t, moveChannel(t, 'a1', 'A', 1))).toBe(false)
    expect(samePayload(toLayoutPayload(t), toLayoutPayload(tree()))).toBe(true)
  })

  it('applies a payload to fresh data', () => {
    const wanted = toLayoutPayload(moveCategory(moveChannel(tree(), 'a1', 'B', 1), 'C', 0))
    const fresh = tree()
    fresh.categories[0].channels[0] = { ...fresh.categories[0].channels[0], name: 'renamed' }
    const t = applyLayoutPayload(fresh, wanted)
    expect(shape(t)).toBe('u1 u2 | C: | A: a2 a3 | B: b1 a1')
    expect(locateChannel(t, 'a1').channel).toMatchObject({ name: 'renamed', category_id: 'B' })
    // Unmoved channels keep their objects.
    expect(locateChannel(t, 'b1').channel).toBe(fresh.categories[1].channels[0])
    expect(applyLayoutPayload(fresh, null)).toBe(fresh)
  })

  it('keeps items the payload does not know after the placed ones', () => {
    const wanted = toLayoutPayload(moveChannel(tree(), 'a3', 'A', 0))
    const fresh = tree()
    fresh.categories[0].channels.splice(1, 0, ch('new', 'A'))
    fresh.categories.push({ id: 'D', channels: [ch('d1', 'D')] })
    const t = applyLayoutPayload(fresh, wanted)
    expect(shape(t)).toBe('u1 u2 | A: a3 a1 a2 new | B: b1 | C: | D: d1')
  })

  it('ignores channels and categories that are gone, and placements into unknown categories', () => {
    const wanted = toLayoutPayload(moveChannel(tree(), 'u1', 'C', 0))
    const fresh = tree()
    fresh.categories = fresh.categories.filter(c => c.id !== 'C')
    fresh.categories[0].channels = fresh.categories[0].channels.filter(c => c.id !== 'a2')
    expect(shape(applyLayoutPayload(fresh, wanted))).toBe('u2 u1 | A: a1 a3 | B: b1')
  })
})

describe('resolveChannelDrop', () => {
  const open = { isCollapsed: () => false }
  const collapsed = ids => ({ isCollapsed: id => ids.includes(id) })

  it('drops above or below a channel row depending on the half', () => {
    expect(resolveChannelDrop(tree(), 'a1', { zone: 'channel', id: 'a3', ratio: 0.2 }, open))
      .toEqual({ categoryId: 'A', gap: 2, index: 1, indicator: { key: 'channel:a3', edge: 'top' } })
    expect(resolveChannelDrop(tree(), 'a1', { zone: 'channel', id: 'a3', ratio: 0.8 }, open))
      .toEqual({ categoryId: 'A', gap: 3, index: 2, indicator: { key: 'channel:a3', edge: 'bottom' } })
    expect(resolveChannelDrop(tree(), 'u1', { zone: 'channel', id: 'b1', ratio: 0.9 }, open))
      .toEqual({ categoryId: 'B', gap: 1, index: 1, indicator: { key: 'channel:b1', edge: 'bottom' } })
  })

  it('treats voice users under a row as its lower half (ratio 1)', () => {
    expect(resolveChannelDrop(tree(), 'u1', { zone: 'channel', id: 'a2', ratio: 1 }, open))
      .toMatchObject({ categoryId: 'A', index: 2 })
  })

  it('returns null where nothing would change', () => {
    const t = tree()
    expect(resolveChannelDrop(t, 'a2', { zone: 'channel', id: 'a2', ratio: 0.1 }, open)).toBeNull()
    expect(resolveChannelDrop(t, 'a2', { zone: 'channel', id: 'a1', ratio: 0.9 }, open)).toBeNull()
    expect(resolveChannelDrop(t, 'a2', { zone: 'channel', id: 'a3', ratio: 0.1 }, open)).toBeNull()
    expect(resolveChannelDrop(t, 'a3', { zone: 'end', id: 'A' }, open)).toBeNull()
    expect(resolveChannelDrop(t, 'u1', { zone: 'start' }, open)).toBeNull()
  })

  it('upper half of an open header: end of the container above', () => {
    expect(resolveChannelDrop(tree(), 'u1', { zone: 'header', id: 'B', ratio: 0.3 }, open))
      .toEqual({ categoryId: 'A', gap: 3, index: 3, indicator: { key: 'header:B', edge: 'top' } })
    // Above the first category: the uncategorized group (Discord-like way out of a category).
    expect(resolveChannelDrop(tree(), 'a2', { zone: 'header', id: 'A', ratio: 0.3 }, open))
      .toEqual({ categoryId: null, gap: 2, index: 2, indicator: { key: 'header:A', edge: 'top' } })
  })

  it('lower half of an open header: first in that category', () => {
    expect(resolveChannelDrop(tree(), 'u1', { zone: 'header', id: 'B', ratio: 0.7 }, open))
      .toEqual({ categoryId: 'B', gap: 0, index: 0, indicator: { key: 'header:B', edge: 'bottom' } })
  })

  it('an open header below a collapsed category only means "first in it"', () => {
    expect(resolveChannelDrop(tree(), 'u1', { zone: 'header', id: 'B', ratio: 0.1 }, collapsed(['A'])))
      .toMatchObject({ categoryId: 'B', index: 0, indicator: { key: 'header:B', edge: 'bottom' } })
  })

  it('a collapsed header appends into it; its top edge goes above', () => {
    expect(resolveChannelDrop(tree(), 'u1', { zone: 'header', id: 'A', ratio: 0.5 }, collapsed(['A'])))
      .toEqual({ categoryId: 'A', gap: 3, index: 3, indicator: { key: 'header:A', edge: 'inside' } })
    expect(resolveChannelDrop(tree(), 'b1', { zone: 'header', id: 'A', ratio: 0.1 }, collapsed(['A'])))
      .toEqual({ categoryId: null, gap: 2, index: 2, indicator: { key: 'header:A', edge: 'top' } })
    // Below another collapsed one the whole header means "into".
    expect(resolveChannelDrop(tree(), 'u1', { zone: 'header', id: 'B', ratio: 0.1 }, collapsed(['A', 'B'])))
      .toMatchObject({ categoryId: 'B', index: 1, indicator: { edge: 'inside' } })
  })

  it('dropping on the collapsed category a channel already sits in changes nothing', () => {
    expect(resolveChannelDrop(tree(), 'a1', { zone: 'header', id: 'A', ratio: 0.6 }, collapsed(['A']))).toBeNull()
    expect(resolveChannelDrop(tree(), 'a1', { zone: 'end', id: 'A' }, collapsed(['A']))).toBeNull()
  })

  it('the "no channels" line of an empty category drops into it', () => {
    expect(resolveChannelDrop(tree(), 'a1', { zone: 'empty', id: 'C' }, open))
      .toEqual({ categoryId: 'C', gap: 0, index: 0, indicator: { key: 'header:C', edge: 'bottom' } })
  })

  it('free space after a container appends to it', () => {
    expect(resolveChannelDrop(tree(), 'u1', { zone: 'end', id: 'A' }, open))
      .toEqual({ categoryId: 'A', gap: 3, index: 3, indicator: { key: 'channel:a3', edge: 'bottom' } })
    expect(resolveChannelDrop(tree(), 'a1', { zone: 'end', id: null }, open))
      .toEqual({ categoryId: null, gap: 2, index: 2, indicator: { key: 'channel:u2', edge: 'bottom' } })
    expect(resolveChannelDrop(tree(), 'a1', { zone: 'end', id: 'C' }, open))
      .toEqual({ categoryId: 'C', gap: 0, index: 0, indicator: { key: 'header:C', edge: 'bottom' } })
    expect(resolveChannelDrop(tree(), 'u1', { zone: 'end', id: 'B' }, collapsed(['B'])))
      .toMatchObject({ categoryId: 'B', index: 1, indicator: { key: 'header:B', edge: 'inside' } })
  })

  it('above everything: first uncategorized channel', () => {
    expect(resolveChannelDrop(tree(), 'b1', { zone: 'start' }, open))
      .toEqual({ categoryId: null, gap: 0, index: 0, indicator: { key: 'channel:u1', edge: 'top' } })
    const noUncat = { uncategorized: [], categories: [{ id: 'A', channels: [ch('a1', 'A')] }] }
    expect(resolveChannelDrop(noUncat, 'a1', { zone: 'start' }, open))
      .toEqual({ categoryId: null, gap: 0, index: 0, indicator: { key: 'header:A', edge: 'top' } })
    expect(resolveChannelDrop(noUncat, 'a1', { zone: 'end', id: null }, open))
      .toMatchObject({ categoryId: null, indicator: { key: 'header:A', edge: 'top' } })
  })

  it('ignores unknown targets and zones', () => {
    const t = tree()
    expect(resolveChannelDrop(t, 'zz', { zone: 'start' }, open)).toBeNull()
    expect(resolveChannelDrop(t, 'a1', null, open)).toBeNull()
    expect(resolveChannelDrop(t, 'a1', { zone: 'channel', id: 'zz' }, open)).toBeNull()
    expect(resolveChannelDrop(t, 'a1', { zone: 'header', id: 'zz' }, open)).toBeNull()
    expect(resolveChannelDrop(t, 'a1', { zone: 'empty', id: 'zz' }, open)).toBeNull()
    expect(resolveChannelDrop(t, 'a1', { zone: 'end', id: 'zz' }, open)).toBeNull()
    expect(resolveChannelDrop(t, 'a1', { zone: 'section', id: 'B' }, open)).toBeNull()
  })

  it('applies to the tree through moveChannel', () => {
    const t = tree()
    const drop = resolveChannelDrop(t, 'a1', { zone: 'channel', id: 'b1', ratio: 0.1 })
    expect(shape(moveChannel(t, 'a1', drop.categoryId, drop.index))).toBe('u1 u2 | A: a2 a3 | B: a1 b1 | C:')
    expect(shape(moveChannelToGap(t, 'a1', drop.categoryId, drop.gap))).toBe('u1 u2 | A: a2 a3 | B: a1 b1 | C:')
  })
})

describe('resolveCategoryDrop', () => {
  it('drops before or after a section by its half', () => {
    expect(resolveCategoryDrop(tree(), 'A', { zone: 'section', id: 'B', ratio: 0.8 }))
      .toEqual({ categoryId: null, gap: 2, index: 1, indicator: { key: 'header:C', edge: 'top' } })
    expect(resolveCategoryDrop(tree(), 'A', { zone: 'section', id: 'C', ratio: 0.9 }))
      .toEqual({ categoryId: null, gap: 3, index: 2, indicator: { key: 'section:C', edge: 'bottom' } })
    expect(resolveCategoryDrop(tree(), 'C', { zone: 'section', id: 'A', ratio: 0.1 }))
      .toEqual({ categoryId: null, gap: 0, index: 0, indicator: { key: 'header:A', edge: 'top' } })
  })

  it('the uncategorized group and the top mean "first"; the bottom "last"', () => {
    expect(resolveCategoryDrop(tree(), 'B', { zone: 'section', id: null, ratio: 0.9 })).toMatchObject({ index: 0 })
    expect(resolveCategoryDrop(tree(), 'B', { zone: 'start' })).toMatchObject({ index: 0 })
    expect(resolveCategoryDrop(tree(), 'A', { zone: 'end' })).toMatchObject({ index: 2, indicator: { key: 'section:C', edge: 'bottom' } })
  })

  it('returns null where nothing would change or the target is unknown', () => {
    const t = tree()
    expect(resolveCategoryDrop(t, 'B', { zone: 'section', id: 'B', ratio: 0.1 })).toBeNull()
    expect(resolveCategoryDrop(t, 'B', { zone: 'section', id: 'A', ratio: 0.9 })).toBeNull()
    expect(resolveCategoryDrop(t, 'B', { zone: 'section', id: 'C', ratio: 0.1 })).toBeNull()
    expect(resolveCategoryDrop(t, 'A', { zone: 'start' })).toBeNull()
    expect(resolveCategoryDrop(t, 'A', { zone: 'section', id: 'zz' })).toBeNull()
    expect(resolveCategoryDrop(t, 'zz', { zone: 'start' })).toBeNull()
    expect(resolveCategoryDrop(t, 'A', { zone: 'channel', id: 'b1' })).toBeNull()
    expect(resolveCategoryDrop(t, 'A', null)).toBeNull()
  })
})
