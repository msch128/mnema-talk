import { describe, it, expect } from 'vitest'
import { THIRD_PARTY } from './thirdParty'
import pkg from '../../package.json'

describe('third-party notices', () => {
  it('names every runtime dependency shipped in the web bundle', () => {
    const listed = THIRD_PARTY.flatMap(g => g.items).map(i => i.pkg).filter(Boolean)
    expect(listed.sort()).toEqual(Object.keys(pkg.dependencies).sort())
  })

  it('gives every entry a license and a link', () => {
    for (const item of THIRD_PARTY.flatMap(g => g.items)) {
      expect(item.license, item.name).toBeTruthy()
      expect(item.url, item.name).toMatch(/^https:\/\//)
    }
  })
})
