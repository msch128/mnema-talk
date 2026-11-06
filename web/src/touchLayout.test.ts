import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('touch hit-area styles', () => {
  it('keep position: relative in the base layer so absolute utilities still win on touch devices', () => {
    const css = readFileSync('src/style.css', 'utf8')
    const coarse = css.slice(css.indexOf('@media (pointer: coarse)'))
    const rule = coarse.indexOf('position: relative')
    expect(rule).toBeGreaterThan(-1)
    const before = coarse.slice(0, rule)
    const layerAt = before.lastIndexOf('@layer base')
    expect(layerAt).toBeGreaterThan(-1)
    // the layer block must still be open at the rule
    const opened = (before.slice(layerAt).match(/{/g) ?? []).length
    const closed = (before.slice(layerAt).match(/}/g) ?? []).length
    expect(opened).toBeGreaterThan(closed)
  })
})
