import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import MessageRow from './MessageRow.vue'
import MessageEditor from './MessageEditor.vue'
import MessageAttachments from './MessageAttachments.vue'
import ReactionBar from './ReactionBar.vue'
import ReactionPalette from './ReactionPalette.vue'
import ReplyPreview from './ReplyPreview.vue'
import ReplyComposerBar from './ReplyComposerBar.vue'
import MentionSuggestions from './MentionSuggestions.vue'
import EmojiPicker from './EmojiPicker.vue'
import UserAvatar from './UserAvatar.vue'
import { fixtureId, messageFixture, requireValue, userFixture } from '../test-fixtures.fixture'
import type { ReplyPreview as Reply, MediaAttachment } from '../types/domain'
import type { MentionSuggestion } from '../lib/mentionQuery'
import { t } from '../i18n'

enableAutoUnmount(afterEach)
beforeEach(() => setActivePinia(createPinia()))
const reply: Reply = { id: fixtureId(5), deleted: false, username: 'original', content: 'Original text' }
const image: MediaAttachment = { id: fixtureId(6), is_deleted: false, mime_type: 'image/png', original_filename: 'image.png', size_bytes: 1, url: '/m/image' }
const emojiStub = defineComponent({ name: 'EmojiPicker', emits: ['pick', 'close'], template: '<div data-testid="emoji-picker"></div>' })

describe('MessageRow presentation and event contract', () => {
  it('forwards every quick action and exposes its article element', async () => {
    const msg = messageFixture({ display_name: 'Author', is_edited: true, reply_count: 2, reply_to: reply, attachments: [image] })
    const w = mount(MessageRow, { props: { msg, isOwn: true, highlighted: true, mentionsMe: true, pickerId: msg.id }, global: { stubs: { EmojiPicker: emojiStub } } })
    expect(w.attributes('role')).toBe('article')
    expect(w.classes()).toContain('msg-flash')
    expect(w.classes()).toContain('msg-mentions-me')
    expect(w.vm.el).toBe(w.element)
    expect(w.find('[data-testid="author-name"]').text()).toBe('Author')
    const buttons = w.findAll('button')
    await requireValue(buttons[0]).trigger('click')
    expect(w.emitted('toggle-picker')).toEqual([[msg.id]])
    const quick = w.findAll('button').filter(b => b.find('svg').exists() && b.element.parentElement?.classList.contains('z-20'))
    expect(quick).toHaveLength(3)
    await requireValue(quick[0]).trigger('click')
    await requireValue(quick[1]).trigger('click')
    await requireValue(quick[2]).trigger('click')
    expect(w.emitted('reply')).toHaveLength(1)
    expect(w.emitted('edit')).toHaveLength(1)
    expect(w.emitted('more')?.[0]?.[0]).toBeInstanceOf(MouseEvent)
    await w.find('[data-testid="author-name"]').trigger('click')
    await w.findComponent(UserAvatar).trigger('click')
    expect(w.emitted('open-profile')).toHaveLength(2)
    w.findComponent(ReplyPreview).vm.$emit('jump')
    w.findComponent(MessageAttachments).vm.$emit('open-image', image.url)
    w.findComponent(ReactionPalette).vm.$emit('pick', '🎉')
    w.findComponent(ReactionPalette).vm.$emit('close')
    expect(w.emitted('jump')).toHaveLength(1)
    expect(w.emitted('open-image')).toEqual([[image.url]])
    expect(w.emitted('react')).toEqual([['🎉']])
    expect(w.emitted('close-picker')).toHaveLength(1)
    const thread = w.findAll('button').find(b => b.text().includes(t('chat.openThread')))
    await requireValue(thread).trigger('click')
    expect(w.emitted('open-thread')).toHaveLength(1)
  })

  it('forwards inline editing and lower reaction controls', async () => {
    const msg = messageFixture()
    const w = mount(MessageRow, { props: { msg, editing: true, saving: true, editText: 'before', pickerId: `bottom-${msg.id}` } })
    await w.find('textarea').setValue('after')
    expect(w.emitted('update:editText')).toEqual([['after']])
    w.findComponent(MessageEditor).vm.$emit('save')
    w.findComponent(MessageEditor).vm.$emit('cancel')
    expect(w.emitted('save')).toHaveLength(1)
    expect(w.emitted('cancel-edit')).toHaveLength(1)
    const bar = w.findComponent(ReactionBar)
    expect(bar.props('pickerOpen')).toBe(true)
    bar.vm.$emit('toggle', '🔥')
    bar.vm.$emit('toggle-picker')
    bar.vm.$emit('close-picker')
    expect(w.emitted('react')).toEqual([['🔥']])
    expect(w.emitted('toggle-picker')).toEqual([[`bottom-${msg.id}`]])
    expect(w.emitted('close-picker')).toHaveLength(1)
  })

  it('renders grouped edits, absent content and username fallback', async () => {
    const w = mount(MessageRow, { props: { msg: messageFixture({ content: '', is_edited: true }), grouped: true } })
    expect(w.findComponent(UserAvatar).exists()).toBe(false)
    expect(w.find('.markdown-body').exists()).toBe(false)
    expect(w.text()).toContain(t('chat.edited'))
    await w.setProps({ grouped: false, msg: messageFixture() })
    expect(w.find('[data-testid="author-name"]').text()).toBe('member')
    expect(w.find('.markdown-body').text()).toBe('Synthetic message')
  })
})

describe('MentionSuggestions', () => {
  it('renders member and group options and preserves composer focus when picking', async () => {
    const member = userFixture({ display_name: 'Member Name' })
    const items: MentionSuggestion[] = [{ username: member.username, display_name: member.display_name, user: member }, { username: 'all', display_name: 'all', group: true }, { username: 'here', display_name: 'here', group: true }]
    const w = mount(MentionSuggestions, { props: { items, active: 1, id: 'composer-options' } })
    const options = w.findAll('[role="option"]')
    expect(options.map(o => o.attributes('aria-selected'))).toEqual(['false', 'true', 'false'])
    expect(requireValue(options[0]).text()).toContain('Member Name')
    expect(w.findComponent(UserAvatar).props('status')).toBe('offline')
    const click = new MouseEvent('mousedown', { bubbles: true, cancelable: true })
    requireValue(options[0]).element.dispatchEvent(click)
    expect(click.defaultPrevented).toBe(true)
    expect(w.emitted('pick')).toEqual([[items[0]]])
    await requireValue(options[2]).trigger('mousemove')
    expect(w.emitted('hover')).toEqual([[2]])
    await w.setProps({ active: 2 })
    expect(requireValue(options[2]).attributes('aria-selected')).toBe('true')
  })

  it('provides its default list identity for an empty list', () => {
    const w = mount(MentionSuggestions, { props: { items: [] } })
    expect(w.attributes('id')).toBe('mention-suggestions')
    expect(w.findAll('[role="option"]')).toHaveLength(0)
  })
})

describe('ReplyComposerBar and ReplyPreview', () => {
  it('shows reply target display name and cancellation, falling back to username', async () => {
    const w = mount(ReplyComposerBar, { props: { target: { display_name: 'Name', username: 'member' } } })
    expect(w.text()).toContain('Name')
    await w.find('button').trigger('click')
    expect(w.emitted('cancel')).toHaveLength(1)
    await w.setProps({ target: { username: 'fallback' } })
    expect(w.text()).toContain('fallback')
  })

  it.each([
    { data: { ...reply, display_name: 'Display' }, text: 'Display' },
    { data: reply, text: 'original' },
    { data: { id: reply.id, deleted: false }, text: t('chat.unknownUser') },
    { data: { ...reply, content: '', has_attachments: true }, text: t('chat.seeAttachment') },
    { data: { ...reply, content: '', has_attachments: false }, text: t('chat.message') },
    { data: { ...reply, deleted: true }, text: t('chat.originalDeleted') }
  ] satisfies { data: Reply; text: string }[])('renders reply variant $text and allows a jump', async ({ data, text }) => {
    const w = mount(ReplyPreview, { props: { reply: data, spineClass: 'custom-spine' } })
    expect(w.text()).toContain(text)
    expect(w.find('.reply-spine').classes()).toContain('custom-spine')
    await w.find('button').trigger('click')
    expect(w.emitted('jump')).toHaveLength(1)
  })
})

describe('ReactionPalette', () => {
  it.each(['left', 'right'] as const)('offers quick reactions and the full picker aligned %s', async align => {
    const w = mount(ReactionPalette, { props: { align }, global: { stubs: { EmojiPicker: emojiStub } } })
    expect(w.classes()).toContain(`${align}-0`)
    await w.find('button').trigger('click')
    expect(w.emitted('pick')).toEqual([['👍']])
    await w.find('[data-testid="reaction-more"]').trigger('click')
    const picker = w.findComponent(EmojiPicker)
    expect(picker.classes()).toContain(`${align}-0`)
    picker.vm.$emit('pick', '🐱')
    picker.vm.$emit('close')
    expect(w.emitted('pick')).toEqual([['👍'], ['🐱']])
    expect(w.emitted('close')).toHaveLength(1)
  })
})
