// Every action the server writes to the audit log must read as a sentence in
// both languages, not as a raw "emoji.create". The server's actions are the
// string literals of its audit calls, so a new action without a label fails here.
import { describe, it, expect } from 'vitest'
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import de from './de.json'
import en from './en.json'

const GROUPS = ['role', 'user', 'channel', 'category', 'layout', 'invite', 'webhook', 'outgoing_webhook', 'media',
  'settings', 'moderation', 'message', 'voice', 'auth', 'system', 'emoji', 'bot', 'poll', 'admin']
const ACTION = new RegExp(`"((?:${GROUPS.join('|')})\\.[a-z_]+)"`, 'g')
// Filenames and similar strings that look like an action.
const NOT_ACTIONS = new Set(['settings.json', 'emoji.json'])

function goFiles(dir: string): string[] {
  return readdirSync(dir).flatMap(name => {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) return goFiles(path)
    return name.endsWith('.go') && !name.endsWith('_test.go') ? [path] : []
  })
}

function serverActions(): string[] {
  const found = new Set<string>()
  for (const file of goFiles(join(process.cwd(), '..', 'internal'))) {
    for (const m of readFileSync(file, 'utf8').matchAll(ACTION)) {
      if (m[1] && !NOT_ACTIONS.has(m[1])) found.add(m[1])
    }
  }
  return [...found].sort()
}

function labelled(tree: Record<string, unknown>): Set<string> {
  const out = new Set<string>()
  const walk = (node: Record<string, unknown>, prefix: string) => {
    for (const [key, value] of Object.entries(node)) {
      if (typeof value === 'string') out.add(prefix + key)
      else if (value && typeof value === 'object') walk(value as Record<string, unknown>, `${prefix}${key}.`)
    }
  }
  walk(tree, '')
  return out
}

describe('audit log labels', () => {
  const actions = serverActions()

  it('finds the server actions', () => {
    expect(actions.length).toBeGreaterThan(70)
    expect(actions).toContain('emoji.create')
    expect(actions).toContain('voice.join')
  })

  it.each([['en', en], ['de', de]] as const)('%s labels every action the server writes', (_locale, messages) => {
    const have = labelled(messages.audit.actions)
    expect(actions.filter(action => !have.has(action))).toEqual([])
  })

  it('offers a filter group for every kind of action', () => {
    const groups = new Set(actions.map(action => action.split('.')[0]))
    for (const group of groups) {
      if (group === 'admin') continue
      expect(Object.keys(en.audit.groups), `no filter group for ${group}`).toContain(group)
      expect(Object.keys(de.audit.groups)).toContain(group)
    }
  })
})
