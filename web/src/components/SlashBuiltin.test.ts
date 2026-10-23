import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import SlashSuggestions from './SlashSuggestions.vue'
import SlashCommandBar from './SlashCommandBar.vue'
import { setLocale, i18nPlugin } from '../i18n'
import { choiceLabel, commandDescription, hasForm, optionDescription } from '../lib/slashCommands'
import type { BotChannelCommand } from '../types/rest'

const base = {
  id: '00000000-0000-4000-8000-000000000001', bot_id: '00000000-0000-0000-0000-000000000000', bot_name: '', created_at: '2026-01-01T00:00:00Z',
  disabled: false
}
const poll: BotChannelCommand = {
  ...base, builtin: true, name: 'poll', description: 'Ask the channel a question',
  options: [
    { name: 'question', description: 'What do you want to ask?', type: 3, required: true },
    { name: 'duration', description: 'How long voting stays open', type: 3, required: false, choices: ['1h', '24h'] }
  ]
}
const bots: BotChannelCommand = { ...base, bot_id: '00000000-0000-4000-8000-000000000009', bot_name: 'helper', name: 'poll', description: 'The bot\'s own poll', options: [] }

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
})

describe('built-in commands in the menu', () => {
  it('are marked built-in and translated, bot commands keep their own text', () => {
    setLocale('de')
    const w = mount(SlashSuggestions, { props: { items: [poll, bots] }, global: { plugins: [i18nPlugin] } })
    const [first, second] = w.findAll('[role="option"]')
    expect(first!.text()).toContain('Stelle dem Kanal eine Frage')
    expect(first!.text()).toContain('Eingebaut')
    expect(second!.text()).toContain('The bot\'s own poll')
    expect(second!.text()).toContain('helper')
  })

  it('fall back to the server text when no translation exists', () => {
    const other: BotChannelCommand = { ...poll, name: 'unknown-thing', description: 'Server text', options: [] }
    expect(commandDescription(other)).toBe('Server text')
    expect(optionDescription(other, { name: 'x', description: 'Server option' })).toBe('Server option')
    expect(choiceLabel(other, { name: 'x' }, 'raw')).toBe('raw')
    expect(commandDescription(bots)).toBe('The bot\'s own poll')
  })

  it('say which have a form of their own', () => {
    expect(hasForm(poll)).toBe(true)
    expect(hasForm(bots)).toBe(false)
    expect(hasForm({ builtin: true, name: 'shrug' })).toBe(false)
  })
})

describe('options with choices', () => {
  it('are a list of the choices, with the built-in\'s labels, and report the pick', async () => {
    const w = mount(SlashCommandBar, { props: { command: poll, values: { question: '', duration: '' } }, global: { plugins: [i18nPlugin] } })
    const select = w.get('select[data-option="duration"]')
    expect(select.findAll('option').map(o => o.text())).toEqual(['', '1 hour', '24 hours'])
    await select.setValue('24h')
    expect(w.emitted('set')).toEqual([['duration', '24h']])
    expect(w.text()).toContain('How long voting stays open')
  })

  it('offer no empty entry when the option is required', () => {
    const required: BotChannelCommand = { ...bots, options: [{ name: 'size', description: 'Size', type: 3, required: true, choices: ['s', 'm'] }] }
    const w = mount(SlashCommandBar, { props: { command: required, values: { size: 's' } }, global: { plugins: [i18nPlugin] } })
    expect(w.get('select[data-option="size"]').findAll('option').map(o => o.text())).toEqual(['s', 'm'])
  })
})
