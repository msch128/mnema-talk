import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { describe, test } from 'node:test'
import { addBotLink, decodeManifest, encodeManifest, VERSION, type Manifest } from '../src/index.ts'

const manifest: Manifest = {
  manifest_version: 1,
  name: 'Mnemo',
  description: 'Answers questions and keeps notes ✓ 📝',
  homepage: 'https://github.com/msch128/mnema-talk-mnemo-bot',
  intents: ['GUILDS', 'GUILD_MESSAGES'],
  permissions: ['view_channel', 'send_messages', 'add_reactions'],
  commands: [
    { name: 'ask', description: 'Ask Mnemo something', options: [{ name: 'question', description: 'Your question', type: 3, required: true }] },
  ],
}

describe('add link', () => {
  test('puts the manifest as base64url JSON into the fragment', () => {
    const link = addBotLink('https://talk.example.org/', manifest)
    const prefix = 'https://talk.example.org/admin/bots/add#manifest='
    assert.ok(link.startsWith(prefix))
    const encoded = link.slice(prefix.length)
    assert.match(encoded, /^[A-Za-z0-9_-]+$/)
    assert.equal(encoded, encodeManifest(manifest))
    assert.deepEqual(JSON.parse(Buffer.from(encoded, 'base64url').toString('utf8')), manifest)
    assert.deepEqual(decodeManifest(encoded), manifest)
  })

  test('refuses a manifest the server would refuse', () => {
    assert.throws(() => encodeManifest({ ...manifest, manifest_version: 2 as never }), /manifest_version/)
    assert.throws(() => encodeManifest({ ...manifest, name: 'x'.repeat(25) }), /name/)
    assert.throws(() => encodeManifest({ ...manifest, homepage: 'http://example.org' }), /https/)
    assert.throws(() => encodeManifest({ ...manifest, avatar: 'https://example.org/a.png' }), /data: URL/)
    assert.throws(() => encodeManifest({ ...manifest, commands: [{ name: 'Ask', description: 'x' }] }), /command name/)
  })

  test('the example manifest is valid', () => {
    const example = JSON.parse(readFileSync(new URL('../examples/manifest.json', import.meta.url), 'utf8')) as Manifest
    assert.doesNotThrow(() => encodeManifest(example))
  })
})

describe('version', () => {
  test('VERSION follows package.json', () => {
    const pkg = JSON.parse(readFileSync(new URL('../package.json', import.meta.url), 'utf8')) as { version: string }
    assert.equal(VERSION, pkg.version)
  })
})
