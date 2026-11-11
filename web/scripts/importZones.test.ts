import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'
import { ALLOWED, EXCEPTIONS, FEATURES, specifierViolation, zoneOf, zoneViolation } from './importZones.ts'

test('a feature imports itself, shared and the base folders', () => {
  assert.equal(zoneViolation('wiki/components/WikiHome.vue', 'wiki/lib/wikiApi.ts'), '')
  assert.equal(zoneViolation('wiki/components/WikiHome.vue', 'shared/components/BaseDialog.vue'), '')
  assert.equal(zoneViolation('wiki/components/WikiHome.vue', 'stores/wiki.ts'), '')
  assert.equal(zoneViolation('wiki/components/WikiHome.vue', 'types/domain.ts'), '')
})

test('a feature does not import another feature', () => {
  assert.match(zoneViolation('wiki/components/WikiHome.vue', 'chat/lib/replies.ts'), /wiki must not import the feature chat/)
  assert.match(zoneViolation('talk/lib/talks.ts', 'chat/components/ChatArea.vue'), /talk must not import the feature chat/)
})

test('the listed edges point one way only', () => {
  assert.equal(zoneViolation('forum/components/ForumArea.vue', 'chat/lib/replies.ts'), '')
  assert.match(zoneViolation('chat/lib/replies.ts', 'forum/lib/forumApi.ts'), /chat must not import the feature forum/)
})

test('shared imports no feature, and nothing below the root imports the shell', () => {
  assert.match(zoneViolation('shared/lib/api.ts', 'chat/lib/drafts.ts'), /shared must not import the feature chat/)
  assert.match(zoneViolation('chat/components/ChatArea.vue', 'app/components/Sidebar.vue'), /Nothing imports the shell/)
  assert.match(zoneViolation('stores/chat.ts', 'app/components/Sidebar.vue'), /Nothing imports the shell/)
  assert.equal(zoneViolation('App.vue', 'app/components/Sidebar.vue'), '')
  assert.equal(zoneViolation('app/components/Sidebar.vue', 'wiki/components/SidebarWikiEntry.vue'), '')
})

test('lib/media counts as Talk', () => {
  assert.equal(zoneOf('lib/media/frame.ts'), 'talk')
  assert.equal(zoneViolation('talk/composables/useWebRTC.ts', 'lib/media/guard.ts'), '')
  assert.match(zoneViolation('wiki/lib/wikiApi.ts', 'lib/media/guard.ts'), /wiki must not import the feature talk/)
})

test('an exception names one module and the zones that may reach it', () => {
  assert.equal(zoneViolation('chat/components/ThreadSidebar.vue', 'forum/components/ForumPostHeader.vue'), '')
  assert.match(zoneViolation('chat/components/ThreadSidebar.vue', 'forum/components/ForumArea.vue'), /must not import/)
  assert.match(zoneViolation('wiki/components/WikiHome.vue', 'forum/components/ForumPostHeader.vue'), /must not import/)
})

test('tests and fixtures drive flows across features', () => {
  assert.equal(zoneViolation('wiki/components/WikiHome.test.ts', 'chat/lib/replies.ts'), '')
  assert.equal(zoneViolation('shared/lib/helpers.fixture.ts', 'chat/lib/replies.ts'), '')
})

test('a specifier is resolved against the importing file', () => {
  const root = '/repo/web/src'
  assert.match(specifierViolation(root, `${root}/wiki/components/WikiHome.vue`, '../../chat/lib/replies'), /must not import/)
  assert.equal(specifierViolation(root, `${root}/wiki/components/WikiHome.vue`, '../lib/wikiApi'), '')
  assert.equal(specifierViolation(root, `${root}/wiki/components/WikiHome.vue`, 'vue'), '')
})

test('the listed edges have no cycle', () => {
  const reaches = (from: string, to: string, seen = new Set<string>()): boolean =>
    ALLOWED[from as keyof typeof ALLOWED].some(next => next === to || (!seen.has(next) && reaches(next, to, seen.add(next))))
  for (const feature of FEATURES) assert.equal(reaches(feature, feature), false, `${feature} reaches itself`)
})

// The tables only shrink: an edge or an exception that no import needs any more must go.
test('every listed edge and exception is still used by web/src', () => {
  const root = fileURLToPath(new URL('../src', import.meta.url))
  const walk = (dir: string): string[] => readdirSync(dir).flatMap(name => {
    const path = join(dir, name)
    return statSync(path).isDirectory() ? (name === 'third_party' ? [] : walk(path)) : [path]
  })
  const edges = new Set<string>()
  const exceptions = new Set<string>()
  for (const file of walk(root).filter(path => /\.(ts|vue)$/.test(path) && !/\.(test|fixture)\.ts$/.test(path))) {
    const from = relative(root, file).split('\\').join('/')
    for (const match of readFileSync(file, 'utf8').matchAll(/(?:from\s*|import\s*\(\s*)['"](\.{1,2}\/[^'"]+)['"]/g)) {
      const to = join(from, '..', match[1] ?? '').split('\\').join('/')
      edges.add(`${zoneOf(from)}>${zoneOf(to)}`)
      const target = to.replace(/\.(ts|vue)$/, '')
      for (const source of EXCEPTIONS.filter(e => e.target === target).flatMap(e => e.from)) if (source === zoneOf(from)) exceptions.add(`${source}>${target}`)
    }
  }
  for (const feature of FEATURES) for (const target of ALLOWED[feature]) assert.ok(edges.has(`${feature}>${target}`), `unused edge ${feature} -> ${target}`)
  for (const e of EXCEPTIONS) for (const source of e.from) assert.ok(exceptions.has(`${source}>${e.target}`), `unused exception ${source} -> ${e.target}`)
})
