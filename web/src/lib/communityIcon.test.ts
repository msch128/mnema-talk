import { describe, it, expect, beforeEach } from 'vitest'
import { communityIconUrl, syncDocumentIcons } from './communityIcon'

beforeEach(() => {
  document.head.innerHTML = `<link rel="icon" type="image/svg+xml" href="/favicon.svg" />` +
    `<link rel="apple-touch-icon" href="/apple-touch-icon.png" />`
})

const icon = () => document.head.querySelector<HTMLLinkElement>('link[rel="icon"]')!
const touch = () => document.head.querySelector<HTMLLinkElement>('link[rel="apple-touch-icon"]')!

describe('communityIconUrl', () => {
  it('names the size and the version, and is empty without a version', () => {
    expect(communityIconUrl('abc', 512)).toBe('/api/community/icon?size=512&v=abc')
    expect(communityIconUrl('a b', 128)).toBe('/api/community/icon?size=128&v=a%20b')
    expect(communityIconUrl('', 128)).toBe('')
  })
})

describe('syncDocumentIcons', () => {
  it('points the tab and home-screen icons at the community icon', () => {
    syncDocumentIcons('v1')
    expect(icon().getAttribute('href')).toBe('/api/community/icon?size=128&v=v1')
    expect(icon().type).toBe('image/png')
    expect(touch().getAttribute('href')).toBe('/api/community/icon?size=512&v=v1')
  })

  it('goes back to the Mnema icons when the icon is removed', () => {
    syncDocumentIcons('v1')
    syncDocumentIcons('')
    expect(icon().getAttribute('href')).toBe('/favicon.svg')
    expect(icon().type).toBe('image/svg+xml')
    expect(icon().hasAttribute('sizes')).toBe(false)
    expect(touch().getAttribute('href')).toBe('/apple-touch-icon.png')
  })

  it('leaves a page without icon links alone', () => {
    document.head.innerHTML = ''
    expect(() => syncDocumentIcons('v1')).not.toThrow()
  })
})
