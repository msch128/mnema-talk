import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ForumTagChip from './ForumTagChip.vue'
import { setLocale } from '../i18n'
import { forumTagFixture } from '../test-fixtures.fixture'
import { tagChipClass } from '../lib/forumTags'

beforeEach(() => setLocale('en'))

describe('ForumTagChip', () => {
  it('is plain text with emoji, name and colour', () => {
    const w = mount(ForumTagChip, { props: { tag: forumTagFixture({ name: 'help', emoji: '❓', color: 'blue' }) } })
    expect(w.element.tagName).toBe('SPAN')
    expect(w.text()).toBe('❓help')
    for (const cls of tagChipClass('blue').split(' ')) expect(w.classes()).toContain(cls)
  })

  it('is a toggle button when selectable and reports the tag', async () => {
    const tag = forumTagFixture()
    const w = mount(ForumTagChip, { props: { tag, selectable: true, selected: true } })
    expect(w.element.tagName).toBe('BUTTON')
    expect(w.attributes('aria-pressed')).toBe('true')
    await w.trigger('click')
    expect(w.emitted('toggle')).toEqual([[tag]])
  })

  it('does not toggle when disabled and shows a lock for moderated tags', async () => {
    const w = mount(ForumTagChip, { props: { tag: forumTagFixture(), selectable: true, disabled: true, locked: true } })
    expect(w.attributes('disabled')).toBeDefined()
    expect(w.find('svg[aria-label="Only moderators can apply this tag"]').exists()).toBe(true)
    await w.trigger('click')
    expect(w.emitted('toggle')).toBeUndefined()
  })
})
