import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, reactive } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import SlashSuggestions from './SlashSuggestions.vue'
import SlashCommandBar from './SlashCommandBar.vue'
import EphemeralNotes from './EphemeralNotes.vue'
import { setLocale } from '../i18n'
import { useChatStore } from '../stores/chat'
import { userFixture, channelFixture } from '../test-fixtures.fixture'
import type { BotChannelCommand } from '../types/rest'

const command = (name: string, options: BotChannelCommand['options'] = [], extra: Partial<BotChannelCommand> = {}): BotChannelCommand => ({
  id: `00000000-0000-4000-8000-00000000000${name.length}`, bot_id: '00000000-0000-4000-8000-000000000009', bot_name: 'helper',
  created_at: '2026-01-01T00:00:00Z', description: `does ${name}`, disabled: false, name, options, ...extra
})

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
})

describe('SlashSuggestions', () => {
  it('lists commands with their bot and reports picks and hovers', async () => {
    const items = [command('ping', [], { bot_avatar_url: '/api/media/x' }), command('help')]
    const w = mount(SlashSuggestions, { props: { items, active: 1 } })
    const options = w.findAll('[role="option"]')
    expect(options.map(o => o.text())).toEqual([expect.stringContaining('/ping'), expect.stringContaining('/help')])
    expect(options[0]!.text()).toContain('does ping')
    expect(w.findAll('[data-testid="slash-group"]').map(g => g.text())).toEqual(['helper'])
    expect(options.map(o => o.attributes('aria-selected'))).toEqual(['false', 'true'])
    expect(w.get('[role="listbox"]').attributes('aria-label')).toBe('Slash commands')
    await options[1]!.trigger('mousemove')
    expect(w.emitted('hover')).toEqual([[1]])
    await options[0]!.trigger('mousedown')
    expect(w.emitted('pick')).toEqual([[items[0]]])
  })
})

describe('SlashSuggestions groups and scrolling', () => {
  it('names each source once, built-ins first', () => {
    const builtin = command('poll', [], { builtin: true })
    const items = [builtin, command('me', [], { builtin: true }), command('ping'), command('help', [], { bot_name: 'other' })]
    const w = mount(SlashSuggestions, { props: { items } })
    expect(w.findAll('[data-testid="slash-group"]').map(g => g.text())).toEqual(['Built-in', 'helper', 'other'])
    expect(w.findAll('[role="option"]')).toHaveLength(4)
  })

  it('shows a fade while there is more below and keeps the active row in view', async () => {
    const items = Array.from({ length: 8 }, (_, i) => command(`c${i}`, [], { id: `00000000-0000-4000-8000-00000000010${i}` }))
    const w = mount(SlashSuggestions, { props: { items, active: 0 }, attachTo: document.body })
    const list = w.get('[role="listbox"]').element
    Object.defineProperty(list, 'scrollHeight', { value: 500, configurable: true })
    Object.defineProperty(list, 'clientHeight', { value: 300, configurable: true })
    await w.get('[role="listbox"]').trigger('scroll')
    expect(w.get('[role="listbox"]').attributes('data-more')).toBeDefined()
    const scrolled = vi.fn()
    const target = w.get('[id="slash-suggestions-5"]').element
    target.scrollIntoView = scrolled
    await w.setProps({ active: 5 })
    await nextTick()
    expect(scrolled).toHaveBeenCalledWith({ block: 'nearest' })
    list.scrollTop = 200
    await w.get('[role="listbox"]').trigger('scroll')
    expect(w.get('[role="listbox"]').attributes('data-more')).toBeUndefined()
    w.unmount()
  })
})

describe('SlashCommandBar', () => {
  const full = command('say', [
    { name: 'text', description: 'What', type: 3, required: true },
    { name: 'times', description: 'How often', type: 4, required: false },
    { name: 'loud', description: 'Shout', type: 5, required: false },
    { name: 'who', description: 'Member', type: 6, required: false },
    { name: 'where', description: 'Channel', type: 7, required: false }
  ])
  function mountBar(extra: { running?: boolean; command?: BotChannelCommand } = {}) {
    const chat = useChatStore()
    chat.members = [userFixture({ id: 'u1', username: 'max', display_name: 'Max' }), userFixture({ id: 'u2', username: 'eva', display_name: '' })]
    chat.uncategorized = [channelFixture({ id: 'c1', name: 'general', type: 'text' }), channelFixture({ id: 'c2', name: 'talk', type: 'voice' })]
    const values = reactive<Record<string, string | boolean>>({ text: '', times: '', loud: false, who: '', where: '' })
    const w = mount(SlashCommandBar, {
      props: { command: extra.command ?? full, values, running: extra.running ?? false, onSet: (name: string, value: string | boolean) => { values[name] = value } },
      attachTo: document.body
    })
    return { w, values }
  }

  it('shows one field per option and writes what is typed into the values', async () => {
    const { w, values } = mountBar()
    expect(w.get('[role="group"]').attributes('aria-label')).toBe('Options of /say')
    expect(w.text()).toContain('/say')
    expect(w.text()).toContain('helper')
    await w.get('[data-option="text"]').setValue('hello')
    await w.get('[data-option="times"]').setValue('3')
    await w.get('[data-option="loud"]').setValue(true)
    await w.get('[data-option="who"]').setValue('u2')
    await w.get('[data-option="where"]').setValue('c1')
    expect(values).toEqual({ text: 'hello', times: '3', loud: true, who: 'u2', where: 'c1' })
    expect(w.get('[data-option="who"]').text()).toContain('Max')
    expect(w.get('[data-option="who"]').text()).toContain('eva')
    expect(w.get('[data-option="where"]').text()).toContain('# general')
    expect(w.get('[data-option="where"]').text()).not.toContain('talk')
    expect(w.get('[data-option="times"]').attributes('type')).toBe('number')
    expect(w.get('[data-option="text"]').attributes('type')).toBe('text')
    expect(document.activeElement).toBe(w.get('[data-option="text"]').element)
    w.unmount()
  })

  it('runs with the button or Enter in a field, and cancels with the button or Escape', async () => {
    const { w } = mountBar()
    await w.get('[data-testid="slash-run"]').trigger('click')
    await w.get('[data-option="text"]').trigger('keydown', { key: 'Enter' })
    await w.get('[data-option="text"]').trigger('keydown', { key: 'Enter', shiftKey: true })
    await w.get('[data-option="who"]').trigger('keydown', { key: 'Enter' })
    expect(w.emitted('run')).toHaveLength(2)
    await w.get('[data-testid="slash-cancel"]').trigger('click')
    await w.get('[data-option="text"]').trigger('keydown', { key: 'Escape' })
    await w.get('[data-option="text"]').trigger('keydown', { key: 'a' })
    expect(w.emitted('cancel')).toHaveLength(2)
    w.unmount()
  })

  it('disables Run while a command runs and copes with a command without options', async () => {
    const { w } = mountBar({ running: true, command: command('hi') })
    expect(w.get('[data-testid="slash-run"]').attributes('disabled')).toBeDefined()
    expect(w.findAll('label')).toHaveLength(0)
    w.unmount()
  })
})

describe('EphemeralNotes', () => {
  const note = (channel: string, content: string, embeds: { title?: string; description?: string }[] | null = null) => ({
    channel_id: channel, bot_id: 'b', bot_name: 'helper', content, embeds, created_at: '2026-01-01T00:00:00Z'
  })

  it('shows the private replies of one channel and dismisses them', async () => {
    const chat = useChatStore()
    const w = mount(EphemeralNotes, { props: { channelId: 'c1' } })
    expect(w.find('[data-testid="ephemeral-notes"]').exists()).toBe(false)
    chat.handleWSEvent({ type: 'interaction_response', payload: { ...note('c1', 'psst', [{ title: 'T', description: 'D' }, {}]), bot_avatar_url: '/api/media/x' } })
    chat.handleWSEvent({ type: 'interaction_response', payload: note('c2', 'elsewhere') })
    chat.handleWSEvent({ type: 'interaction_response', payload: note('c1', '') })
    await w.vm.$nextTick()
    const articles = w.findAll('article')
    expect(articles).toHaveLength(2)
    expect(articles[0]!.text()).toContain('psst')
    expect(articles[0]!.text()).toContain('T')
    expect(articles[0]!.text()).toContain('D')
    expect(articles[0]!.text()).toContain('Only you can see this')
    expect(articles[0]!.attributes('aria-label')).toBe('Private reply from helper')
    await articles[0]!.get('button').trigger('click')
    expect(w.findAll('article')).toHaveLength(1)
    expect(chat.ephemeral).toHaveLength(2) // the other channel's note stays
  })

  it('keeps the latest twenty and forgets them with the community', () => {
    const chat = useChatStore()
    for (let i = 0; i < 25; i++) chat.handleWSEvent({ type: 'interaction_response', payload: note('c1', `n${i}`) })
    expect(chat.ephemeral).toHaveLength(20)
    expect(chat.ephemeral[0]!.content).toBe('n5')
    chat.resetCommunityState()
    expect(chat.ephemeral).toEqual([])
  })
})
