// A block folds the blocked member's messages: "You don't see messages from
// members you block" (settings, blocks.intro), "Their messages are collapsed in
// every list" (stores/blocks.ts), and doc/moderation.md "Blocking a member".
// The message list and the thread fold them (ChatAreaDm.test.ts). These are
// the other places that show a blocked member's words. Finding H-L3-10 of the
// 2026-11 hunt: the it.fails cases turn red the day they fold.
import { beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import '../../shared/lib/customEmoji'
import ReplyPreview from './ReplyPreview.vue'
import SearchResultItem from './SearchResultItem.vue'
import { messageFixture } from '../../test-fixtures.fixture'
import { useBlocksStore } from '../../stores/blocks'
import { useServerFeaturesStore } from '../../stores/serverFeatures'

const BLOCKED = '00000000-0000-4000-8000-0000000000b1'
const WORDS = 'words of the blocked member'

beforeEach(() => {
  setActivePinia(createPinia())
  useServerFeaturesStore().list = ['blocks']
  useBlocksStore().blocked = [{ user_id: BLOCKED, username: 'troll', display_name: 'Troll', blocked_at: '2026-01-01T00:00:00Z' }]
})

describe('a blocked member is folded beyond the message list', () => {
  it('the block is in force (control)', () => {
    expect(useBlocksStore().hides({ id: 'm1', user_id: BLOCKED })).toBe(true)
  })

  it.fails('the "replied to" line above a reply to the blocked member', () => {
    const reply = { id: 'm1', user_id: BLOCKED, username: 'troll', display_name: 'Troll', content: WORDS }
    const w = mount(ReplyPreview, { props: { reply } as never })
    expect(w.text()).not.toContain(WORDS)
  })

  it.fails('a search result written by the blocked member', () => {
    const msg = messageFixture({ id: 'm2', user_id: BLOCKED, username: 'troll', content: WORDS })
    const w = mount(SearchResultItem, { props: { index: 0, selected: false, voice: false, terms: ['words'], msg } })
    expect(w.text()).not.toContain(WORDS)
  })

  it.fails('the thread root of a search result, written by the blocked member', () => {
    const msg = messageFixture({ id: 'm3', content: 'a reply', parent_id: 'p1' })
    const root = { id: 'p1', channel_id: 'ch1', user_id: BLOCKED, username: 'troll', display_name: 'Troll', avatar_url: null, snippet: WORDS }
    const w = mount(SearchResultItem, { props: { index: 0, selected: false, voice: false, terms: ['reply'], msg, root } })
    expect(w.text()).not.toContain(WORDS)
  })
})
