// Every code the server can send for the admin status tab (api/status-codes.json, kept equal to the Go
// catalogs by a Go test) needs an English and a German text, so a new check or warning cannot ship untranslated.
import { describe, expect, it } from 'vitest'
import codes from '../../../api/status-codes.json'
import de from './de.json'
import en from './en.json'
import { describeWarning } from '../lib/operatorWarnings'
import { describeCheck } from '../lib/setupChecks'
import { setLocale } from './index'

type Tree = { [key: string]: Tree | string }

function node(tree: Tree, path: string): Tree | string | undefined {
  let cur: Tree | string | undefined = tree
  for (const part of path.split('.')) {
    if (cur === undefined || typeof cur === 'string') return undefined
    cur = cur[part]
  }
  return cur
}

const placeholders = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map(m => m[1]).sort()

describe.each([['en', en], ['de', de]] as const)('status codes in %s', (_name, messages) => {
  const tree = messages as Tree

  it.each(codes.setup_checks)('check %s has a detail, and a fix whenever the other language has one', code => {
    const entry = node(tree, `admin.operatorStatus.checkMsg.${code}`)
    expect(typeof entry === 'object' && typeof entry.detail === 'string' && entry.detail !== '').toBe(true)
    for (const other of [en, de] as Tree[]) {
      const o = node(other, `admin.operatorStatus.checkMsg.${code}`) as Tree
      const e = entry as Tree
      expect(typeof e.fix).toBe(typeof o.fix)
      expect(placeholders(String(e.detail))).toEqual(placeholders(String(o.detail)))
      expect(placeholders(String(e.fix ?? ''))).toEqual(placeholders(String(o.fix ?? '')))
    }
  })

  it.each(codes.operator_warnings)('warning %s has a title and a body', code => {
    for (const part of ['title', 'body']) {
      expect(typeof node(tree, `admin.system.warn.${code}.${part}`)).toBe('string')
    }
  })

  it('has no check text for a code the server does not send', () => {
    const known = new Set(codes.setup_checks)
    const have = Object.entries(node(tree, 'admin.operatorStatus.checkMsg') as Tree)
      .flatMap(([id, outcomes]) => Object.keys(outcomes as Tree).map(o => `${id}.${o}`))
    expect(have.filter(c => !known.has(c))).toEqual([])
  })
})

describe('describing a code', () => {
  it('maps every warning code to its own text, not the unknown-code fallback', () => {
    setLocale('en')
    for (const code of codes.operator_warnings) {
      expect(describeWarning(code, 1).title, code).not.toMatch(/^Server warning/)
    }
  })

  it('fills the parameters into the check text and falls back to the server sentence without a code', () => {
    setLocale('de')
    const c = { id: 'voice_udp', level: 'ok' as const, code: 'voice_udp.ok', params: { port: '50000' }, detail: 'English.' }
    expect(describeCheck(c).detail).toContain('UDP 50000')
    const { code: _code, ...withoutCode } = c
    expect(describeCheck(withoutCode)).toEqual({ detail: 'English.', fix: '' })
    setLocale('de')
  })
})
