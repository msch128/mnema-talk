import { describe, expect, it } from 'vitest'
import { avatarFromManifest, installableChannels, manifestTextFromParam, MAX_AVATAR_BYTES, readManifest, safeHomepage } from './manifest'
import { categoryFixture, channelFixture, fixtureId } from '../../test-fixtures.fixture'

describe('reading a manifest', () => {
  it('decodes an add link, UTF-8 included', () => {
    const text = '{"name":"Mnémo"}'
    const param = btoa(String.fromCharCode(...new TextEncoder().encode(text))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
    expect(manifestTextFromParam(param)).toBe(text)
    expect(manifestTextFromParam('%%%')).toBeNull()
  })

  it('takes a JSON object only', () => {
    expect(readManifest('{"a": 1}')).toEqual({ ok: true, manifest: { a: 1 } })
    expect(readManifest('nope')).toEqual({ ok: false, reason: 'json' })
    expect(readManifest('null')).toEqual({ ok: false, reason: 'object' })
  })
})

describe('the avatar of a manifest', () => {
  it('becomes a file of its own type', () => {
    const read = avatarFromManifest({ avatar: 'data:image/webp;base64,UklGRg==' }, 'Mnemo')
    expect('file' in read && read.file?.type).toBe('image/webp')
    expect('file' in read && read.file?.name).toBe('Mnemo.webp')
  })

  it('is left out with a reason when it is no PNG, JPEG or WebP, too large, or remote', () => {
    expect(avatarFromManifest({})).toEqual({ file: null, problem: null })
    expect(avatarFromManifest({ avatar: 'data:image/svg+xml;base64,PHN2Zz4=' })).toEqual({ file: null, problem: 'type' })
    expect(avatarFromManifest({ avatar: `data:image/png;base64,${'A'.repeat(Math.ceil((MAX_AVATAR_BYTES + 3) / 3) * 4)}` })).toEqual({ file: null, problem: 'size' })
    expect(avatarFromManifest({ avatar: 'https://example.org/a.png' })).toEqual({ file: null, problem: 'data' })
  })
})

describe('what the confirm screen offers', () => {
  it('sorts usable channels into their categories', () => {
    const a = channelFixture({ id: fixtureId(1), name: 'b', sort_order: 2 })
    const b = channelFixture({ id: fixtureId(2), name: 'a', sort_order: 1 })
    const voice = channelFixture({ id: fixtureId(3), type: 'voice' })
    const groups = installableChannels([categoryFixture({ id: fixtureId(9), name: 'Cat', channels: [a, b, voice] }), categoryFixture({ id: fixtureId(8), channels: [voice] })], [])
    expect(groups).toEqual([{ id: fixtureId(9), name: 'Cat', channels: [b, a] }])
  })

  it('links a homepage only over https', () => {
    expect(safeHomepage('https://example.org')).toBe('https://example.org')
    expect(safeHomepage('javascript:alert(1)')).toBe('')
    expect(safeHomepage('http://example.org')).toBe('')
    expect(safeHomepage(undefined)).toBe('')
  })
})
