import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  actionRow,
  button,
  ButtonStyle,
  channelSelect,
  linkButton,
  modal,
  roleSelect,
  stringSelect,
  textInput,
  TextInputStyle,
  userSelect,
  validateCommand,
  validateCommands,
  validateMessageComponents,
  ValidationError,
} from '../src/index.ts'

const long = (n: number) => 'x'.repeat(n)

describe('buttons', () => {
  test('build the documented JSON', () => {
    assert.deepEqual(button({ customId: 'deploy:v2.4', label: 'Deploy', style: ButtonStyle.Success }), {
      type: 2,
      style: 3,
      label: 'Deploy',
      custom_id: 'deploy:v2.4',
    })
    assert.deepEqual(linkButton({ url: 'https://example.org/changes', label: 'Changelog' }), {
      type: 2,
      style: 5,
      label: 'Changelog',
      url: 'https://example.org/changes',
    })
    assert.deepEqual(button({ customId: 'like', emoji: '👍' }), { type: 2, style: 1, emoji: { name: '👍' }, custom_id: 'like' })
  })

  test('refuse what the server would refuse', () => {
    assert.throws(() => button({ customId: '', label: 'A' }), /custom_id must be 1 to 100/)
    assert.throws(() => button({ customId: long(101), label: 'A' }), /custom_id must be 1 to 100/)
    assert.throws(() => button({ customId: 'a', label: long(81) }), /label must be 0 to 80/)
    assert.throws(() => button({ customId: 'a' }), /label or an emoji/)
    assert.throws(() => button({ customId: 'a', label: 'A', style: 5 as never }), /linkButton/)
    assert.throws(() => linkButton({ url: 'ftp://example.org', label: 'A' }), /http/)
  })
})

describe('selects', () => {
  test('a string select carries its options', () => {
    const s = stringSelect({
      customId: 'env',
      placeholder: 'Environment',
      options: [
        { label: 'Staging', value: 'staging' },
        { label: 'Production', value: 'prod', default: true },
      ],
    })
    assert.deepEqual(s, {
      type: 3,
      custom_id: 'env',
      placeholder: 'Environment',
      options: [
        { label: 'Staging', value: 'staging' },
        { label: 'Production', value: 'prod', default: true },
      ],
    })
  })

  test('user, role and channel selects have their types', () => {
    assert.equal(userSelect({ customId: 'u' }).type, 5)
    assert.equal(roleSelect({ customId: 'r', maxValues: 3 }).max_values, 3)
    assert.equal(channelSelect({ customId: 'c', minValues: 0 }).min_values, 0)
  })

  test('limits: 1 to 25 options, unique values, lengths, value counts', () => {
    const opts = (n: number) => Array.from({ length: n }, (_, i) => ({ label: `L${i}`, value: `v${i}` }))
    assert.throws(() => stringSelect({ customId: 's', options: [] }), /1 to 25 options/)
    assert.throws(() => stringSelect({ customId: 's', options: opts(26) }), /1 to 25 options/)
    assert.doesNotThrow(() => stringSelect({ customId: 's', options: opts(25) }))
    assert.throws(() => stringSelect({ customId: 's', options: [{ label: 'a', value: 'v' }, { label: 'b', value: 'v' }] }), /unique/)
    assert.throws(() => stringSelect({ customId: 's', options: [{ label: long(101), value: 'v' }] }), /label/)
    assert.throws(() => stringSelect({ customId: 's', options: opts(2), placeholder: long(151) }), /placeholder/)
    assert.throws(() => userSelect({ customId: 'u', maxValues: 26 }), /max_values/)
    assert.throws(() => userSelect({ customId: 'u', minValues: 3, maxValues: 2 }), /min_values must not exceed/)
  })
})

describe('action rows and messages', () => {
  test('a row holds up to 5 buttons or exactly one select', () => {
    const b = (id: string) => button({ customId: id, label: id })
    assert.equal(actionRow(b('1'), b('2'), b('3'), b('4'), b('5')).components.length, 5)
    assert.equal(actionRow([b('1'), b('2')]).components.length, 2)
    assert.throws(() => actionRow(b('1'), b('2'), b('3'), b('4'), b('5'), b('6')), /at most 5/)
    const select = userSelect({ customId: 'u' })
    assert.throws(() => actionRow(select, b('1')), /exactly one select/)
    assert.throws(() => actionRow(), /at least one/)
  })

  test('a message has at most 5 rows and unique custom_ids; plain objects are checked too', () => {
    const row = (id: string) => ({ type: 1 as const, components: [{ type: 2 as const, style: 1 as const, label: id, custom_id: id }] })
    assert.doesNotThrow(() => validateMessageComponents([row('a'), row('b'), row('c'), row('d'), row('e')]))
    assert.throws(() => validateMessageComponents([row('a'), row('b'), row('c'), row('d'), row('e'), row('f')]), /at most 5 action rows/)
    assert.throws(() => validateMessageComponents([row('a'), row('a')]), /"a" is used twice/)
    assert.throws(() => validateMessageComponents([{ type: 2 } as never]), /action row/)
    assert.throws(() => validateMessageComponents([{ type: 1, components: [{ type: 7, custom_id: 'm' }] } as never]), /type 7 cannot/)
  })
})

describe('modals', () => {
  test('wraps each text input in its own row', () => {
    const m = modal({
      customId: 'feedback',
      title: 'Feedback',
      components: [
        textInput({ customId: 'subject', label: 'Subject', maxLength: 80 }),
        textInput({ customId: 'body', label: 'What happened?', style: TextInputStyle.Paragraph, required: false, placeholder: 'Tell us more' }),
      ],
    })
    assert.deepEqual(m, {
      custom_id: 'feedback',
      title: 'Feedback',
      components: [
        { type: 1, components: [{ type: 4, custom_id: 'subject', label: 'Subject', style: 1, max_length: 80 }] },
        { type: 1, components: [{ type: 4, custom_id: 'body', label: 'What happened?', style: 2, required: false, placeholder: 'Tell us more' }] },
      ],
    })
  })

  test('limits: title 45, 1 to 5 rows, label 45, lengths up to 4000', () => {
    const input = (id: string) => textInput({ customId: id, label: id })
    assert.throws(() => modal({ customId: 'm', title: long(46), components: [input('a')] }), /title/)
    assert.throws(() => modal({ customId: 'm', title: 'T', components: [] }), /1 to 5 rows/)
    assert.throws(() => modal({ customId: 'm', title: 'T', components: ['a', 'b', 'c', 'd', 'e', 'f'].map(input) }), /1 to 5 rows/)
    assert.throws(() => modal({ customId: 'm', title: 'T', components: [input('a'), input('a')] }), /used twice/)
    assert.throws(() => textInput({ customId: 'a', label: long(46) }), /label/)
    assert.throws(() => textInput({ customId: 'a', label: 'A', maxLength: 4001 }), /max_length/)
    assert.throws(() => textInput({ customId: 'a', label: 'A', minLength: 10, maxLength: 5 }), /min_length must not exceed/)
    assert.throws(() => textInput({ customId: 'a', label: 'A', value: long(4001) }), /value/)
    const looseRow = actionRow as (...c: unknown[]) => unknown
    assert.throws(() => looseRow(input('a'), input('b')), /exactly one text input/)
  })
})

describe('commands', () => {
  test('accepts the documented example', () => {
    assert.doesNotThrow(() =>
      validateCommand({ name: 'ping', description: 'Replies with pong', options: [{ name: 'text', description: 'What to say', type: 3, required: true }] }),
    )
  })

  test('refuses bad names, too many options, late required options and bad choices', () => {
    assert.throws(() => validateCommand({ name: 'Ping', description: 'x' }), ValidationError)
    assert.throws(() => validateCommand({ name: long(33), description: 'x' }), /1 to 32/)
    assert.throws(() => validateCommand({ name: 'p', description: '' }), /description/)
    const opt = (i: number, required = false) => ({ name: `o${i}`, description: 'd', type: 3 as const, required })
    assert.throws(() => validateCommand({ name: 'p', description: 'd', options: Array.from({ length: 11 }, (_, i) => opt(i)) }), /more than 10/)
    assert.throws(() => validateCommand({ name: 'p', description: 'd', options: [opt(1), opt(2, true)] }), /required options come first/)
    assert.throws(() => validateCommand({ name: 'p', description: 'd', options: [{ ...opt(1), type: 4, choices: ['a'] }] }), /only string options/)
    assert.throws(() => validateCommand({ name: 'p', description: 'd', options: [{ ...opt(1), choices: ['a', 'a'] }] }), /unique/)
    const many = Array.from({ length: 51 }, (_, i) => ({ name: `c${i}`, description: 'd' }))
    assert.throws(() => validateCommands(many), /at most 50/)
    assert.throws(() => validateCommands([{ name: 'a', description: 'd' }, { name: 'a', description: 'd' }]), /twice/)
  })
})
