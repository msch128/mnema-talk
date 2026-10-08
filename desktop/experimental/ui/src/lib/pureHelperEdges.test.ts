import { afterEach, describe, expect, it, vi } from 'vitest'
import { menuPointFromEvent } from './contextMenuPoint'
import { confirm, pendingConfirm } from './confirm'
import { buildChannelTree, loadCollapsed, saveCollapsed } from './channelTree'
import { secondsSince, formatClock, totalParts } from './clock'
import { extractPreviewUrls, firstUnreadId, typingLine, createKeyedThrottle } from './chatLogic'
import { renderMarkdown } from './markdown'
import { stripMarkdown, markPreviewEdited } from './replies'
import { findMentionQuery, suggestMentions, applyMention } from './mentionQuery'
import { snapshotToMap, applyPresenceUpdate } from './presence'
import { fromLatest, fromAround, prependOlder, appendNewer, appendLive, emptyWindow, dedupe } from './messageWindow'
import { MIN_PASSWORD_LENGTH } from './passwordPolicy'
import { fixtureId, userFixture, messageFixture } from '../test-fixtures.fixture'

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); vi.useRealTimers() })

describe('menu and confirmation edge cases', () => {
  it('uses partial mouse coordinates, keyboard anchors, and safe empty fallbacks', () => {
    expect(menuPointFromEvent({ clientX: 4 })).toEqual({ x: 4, y: 0, anchor: null })
    expect(menuPointFromEvent({ clientY: 6 })).toEqual({ x: 0, y: 6, anchor: null })
    const element = document.createElement('button')
    vi.spyOn(element, 'getBoundingClientRect').mockReturnValue(new DOMRect(12, 13, 30, 20))
    expect(menuPointFromEvent({ clientX: 0, clientY: 0, currentTarget: element })).toEqual({ x: 12, y: 33, anchor: element })
    expect(menuPointFromEvent({ currentTarget: window, target: element })).toEqual({ x: 12, y: 33, anchor: element })
    for (const event of [null, undefined, {}, { target: window }, { currentTarget: null, target: null }]) expect(menuPointFromEvent(event)).toEqual({ x: 0, y: 0, anchor: null })
  })
  it('cancels an earlier request and prevents stale resolution from clearing the replacement', async () => {
    const first = confirm({ title: 'First' })
    const stale = pendingConfirm.value
    expect(stale).not.toBeNull()
    const second = confirm({ title: 'Second', danger: true })
    await expect(first).resolves.toBe(false)
    stale?.resolve(true)
    expect(pendingConfirm.value?.title).toBe('Second')
    pendingConfirm.value?.resolve(true)
    await expect(second).resolves.toBe(true)
    expect(pendingConfirm.value).toBeNull()
  })
})

describe('runtime edges of pure chat helpers', () => {
  it('remains safe when collapse persistence is unavailable', () => {
    expect(loadCollapsed(null)).toEqual([])
    saveCollapsed([], null)
    expect(buildChannelTree(undefined, undefined)).toEqual({ categories: [], uncategorized: [] })
  })
  it('formats unknown durations and falls back for missing history', () => {
    expect(formatClock(undefined)).toBe('0:00')
    expect(totalParts(null)).toEqual({ key: 'activity.underMinute', params: {} })
    expect(secondsSince(undefined)).toBe(0)
    expect(extractPreviewUrls(null)).toEqual([])
    expect(firstUnreadId(undefined, 'bad', 'me')).toBeNull()
    expect(firstUnreadId([messageFixture()], 'bad', 'me')).toBeNull()
    expect(typingLine(undefined)).toBeNull()
    const throttle = createKeyedThrottle(vi.fn(), 100)
    throttle.cancel('never-scheduled')
  })
  it('removes orphaned internal markdown placeholders and preserves short mention-like strings', () => {
    expect(renderMarkdown(null)).toBe('')
    expect(renderMarkdown('before\u00009999\u0000after')).toBe('beforeafter')
    expect(stripMarkdown('before\u00009999\u0000after')).toBe('beforeafter')
    expect(renderMarkdown('@a..')).toBe('@a..')
  })
  it('supports edits that update only attachments without clearing the cached text', () => {
    const reply = messageFixture({ reply_to_id: fixtureId(5), reply_to: { id: fixtureId(5), content: 'Keep', deleted: false, has_attachments: false } })
    expect(markPreviewEdited([[reply]], { id: fixtureId(5), attachments: [] })).toBe(1)
    expect(reply.reply_to?.has_attachments).toBe(false)
    expect(reply.reply_to?.content).toBe('Keep')
    const attachment = { id: fixtureId(8), is_deleted: false, mime_type: 'image/png', original_filename: 'synthetic.png', size_bytes: 1, url: '/api/media/synthetic' }
    markPreviewEdited([[reply]], { id: fixtureId(5), attachments: [attachment] })
    expect(reply.reply_to?.has_attachments).toBe(true)
  })
  it('limits mention length, ignores unknown caret and ranks internal matches after word prefixes', () => {
    expect(findMentionQuery(null, 1)).toBeNull()
    expect(findMentionQuery('@abc', null)).toBeNull()
    expect(findMentionQuery(`@${'a'.repeat(33)}`, 34)).toBeNull()
    expect(findMentionQuery('@@abc', 5)).toBeNull()
    expect(findMentionQuery(' @abc', 5)).toEqual({ start: 1, query: 'abc' })
    expect(suggestMentions('', undefined).map(item => item.username)).toEqual(['here', 'all'])
    const one = userFixture({ id: fixtureId(1), username: 'first', display_name: 'A Word' })
    const two = userFixture({ id: fixtureId(2), username: 'middleword', display_name: '' })
    const three = { ...userFixture({ id: fixtureId(3), username: 'noword', display_name: 'No match' }), disabled: true }
    expect(suggestMentions('word', [two, three, one]).map(item => item.username)).toEqual(['first', 'middleword'])
    expect(suggestMentions('missing', [one, two])).toEqual([])
    expect(suggestMentions('', [two, userFixture({ id: fixtureId(4), username: 'alpha', display_name: '' })], { limit: 1 }).map(item => item.username)).toEqual(['alpha', 'here', 'all'])
    expect(applyMention('@abc.', 0, 2, 'first')).toEqual({ text: '@first ', caret: 7 })
  })
  it('handles malformed legacy presence snapshots and absent updates without modifying the map', () => {
    expect(snapshotToMap(null)).toEqual({})
    expect(snapshotToMap([fixtureId(1), null, 4])).toEqual({ [fixtureId(1)]: 'online' })
    const current = { [fixtureId(1)]: 'online' as const }
    expect(applyPresenceUpdate(current, null)).toBe(current)
    expect(applyPresenceUpdate(current, undefined)).toBe(current)
    expect(applyPresenceUpdate(current, { user_id: '' })).toBe(current)
  })
  it('treats missing pages conservatively and ignores invalid live messages', () => {
    const win = emptyWindow<{ id: string }>()
    expect(dedupe(null)).toEqual([])
    expect(dedupe([null, undefined, { id: 'a' }])).toEqual([{ id: 'a' }])
    expect(fromLatest(null)).toEqual(win)
    expect(fromAround(null, 'missing')).toEqual(win)
    expect(fromAround([{ id: 'a' }], 'missing')).toEqual({ messages: [{ id: 'a' }], hasMoreBefore: true, hasMoreAfter: true })
    expect(prependOlder(win, null)).toEqual(win)
    expect(appendNewer(win, null)).toEqual(win)
    expect(appendLive(win, null)).toEqual({ window: win, status: 'duplicate' })
  })
  it('shares the documented minimum password length', () => { expect(MIN_PASSWORD_LENGTH).toBe(10) })
})
