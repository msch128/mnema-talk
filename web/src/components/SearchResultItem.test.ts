import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import '../lib/customEmoji'
import SearchResultItem from './SearchResultItem.vue'
import { messageFixture } from '../test-fixtures.fixture'

const props = { index: 0, selected: false, voice: false, terms: ['party'] }

describe('SearchResultItem', () => {
  it('shows the hit as plain text with the matches highlighted', () => {
    const msg = messageFixture({ content: '**Great** party <:blob:11111111-2222-4333-8444-555555555555> ||twist||' })
    const w = mount(SearchResultItem, { props: { ...props, msg } })
    const text = w.text()
    expect(text).toContain('Great party :blob: Spoiler')
    expect(text).not.toMatch(/\*\*|<:|twist/)
    expect(w.find('mark').text()).toBe('party')
  })

  it('shows the thread root snippet as plain text, too', () => {
    const msg = messageFixture({ content: 'reply', parent_id: 'p1' })
    const root = { id: 'p1', channel_id: 'ch1', user_id: 'u1', username: 'anna', display_name: 'Anna', avatar_url: null, snippet: 'start `here` ||hidden||' }
    const w = mount(SearchResultItem, { props: { ...props, msg, root } })
    expect(w.find('[data-search-root]').text()).toContain('start here Spoiler')
    expect(w.find('[data-search-root]').text()).not.toContain('hidden')
  })
})
