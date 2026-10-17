import { describe, it, expect } from 'vitest'
import { slashQuery, filterCommands, emptyValues, buildOptions, OPTION } from './slashCommands'
import type { BotChannelCommand } from '../types/rest'

const command = (name: string, options: BotChannelCommand['options'] = []): BotChannelCommand => ({
  id: `id-${name}`, bot_id: 'b', bot_name: 'helper', created_at: '2026-01-01T00:00:00Z', description: 'd', disabled: false, name, options
})

describe('slashQuery', () => {
  it('is the first word after a leading slash, lowercased', () => {
    expect(slashQuery('/')).toBe('')
    expect(slashQuery('/Pi')).toBe('pi')
  })
  it('is nothing for ordinary text, a slash elsewhere, or a finished first word', () => {
    expect(slashQuery('hello')).toBeNull()
    expect(slashQuery('a /b')).toBeNull()
    expect(slashQuery('/ping now')).toBeNull()
    expect(slashQuery('/ping\n')).toBeNull()
    expect(slashQuery('')).toBeNull()
  })
})

describe('filterCommands', () => {
  const list = [command('help'), command('ping'), command('shipping'), command('pong')]
  it('lists names starting with the query first, then those containing it', () => {
    expect(filterCommands(list, 'p').map(c => c.name)).toEqual(['ping', 'pong', 'help', 'shipping'])
    expect(filterCommands(list, '').map(c => c.name)).toEqual(['help', 'ping', 'shipping', 'pong'])
    expect(filterCommands(list, 'zzz')).toEqual([])
  })
  it('shows at most eight', () => {
    const many = Array.from({ length: 12 }, (_, i) => command(`c${i}`))
    expect(filterCommands(many, 'c')).toHaveLength(8)
  })
})

describe('options', () => {
  const cmd = command('say', [
    { name: 'text', description: 't', type: OPTION.string, required: true },
    { name: 'times', description: 'n', type: OPTION.integer, required: false },
    { name: 'loud', description: 'b', type: OPTION.boolean, required: false },
    { name: 'who', description: 'u', type: OPTION.user, required: false },
    { name: 'where', description: 'c', type: OPTION.channel, required: false }
  ])
  it('starts empty, booleans off', () => {
    expect(emptyValues(cmd)).toEqual({ text: '', times: '', loud: false, who: '', where: '' })
  })
  it('sends filled options typed, and leaves empty optional ones out', () => {
    const built = buildOptions(cmd, { text: ' hello ', times: ' 3 ', loud: true, who: 'u1', where: '' })
    expect(built.problem).toBeNull()
    expect(built.options).toEqual({ text: 'hello', times: 3, loud: true, who: 'u1' })
    expect(buildOptions(cmd, { text: 'x', times: '', loud: false, who: '', where: '' }).options).toEqual({ text: 'x', loud: false })
  })
  it('names the first required option without a value', () => {
    expect(buildOptions(cmd, { text: '  ', times: '', loud: false, who: '', where: '' }).problem).toEqual({ name: 'text', kind: 'missing' })
    expect(buildOptions(cmd, { loud: false }).problem).toEqual({ name: 'text', kind: 'missing' })
  })
  it('refuses integers that are none', () => {
    for (const bad of ['1.5', 'abc', '1e400', '9007199254740993']) {
      expect(buildOptions(cmd, { text: 'x', times: bad, loud: false }).problem).toEqual({ name: 'times', kind: 'integer' })
    }
    expect(buildOptions(cmd, { text: 'x', times: '-7', loud: false }).options['times']).toBe(-7)
  })
})
