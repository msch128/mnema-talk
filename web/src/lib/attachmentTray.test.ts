import { describe, expect, it } from 'vitest'
import { kindOf, MAX_ATTACHMENTS, MAX_TOTAL_BYTES, planAdd } from './attachmentTray'

const file = (name: string, size = 1, type = 'image/png') => {
  const f = new File(['x'], name, { type })
  Object.defineProperty(f, 'size', { value: size })
  return f
}
const MB = 1024 * 1024

describe('kindOf', () => {
  it('tells pictures and videos from other files', () => {
    expect([kindOf(file('a.png')), kindOf(file('b.mp4', 1, 'video/mp4')), kindOf(file('c.pdf', 1, 'application/pdf')), kindOf(file('d', 1, ''))])
      .toEqual(['image', 'video', 'file', 'file'])
  })
})

describe('planAdd', () => {
  it('accepts files up to the limit of ten and sets the rest aside', () => {
    const files = Array.from({ length: 12 }, (_, i) => file(`${i}.png`))
    const plan = planAdd(0, 0, files, null)
    expect(plan.accept).toHaveLength(MAX_ATTACHMENTS)
    expect(plan.overCount.map(f => f.name)).toEqual(['10.png', '11.png'])
  })

  it('counts the files already in the tray', () => {
    const plan = planAdd(9, 9, [file('a.png'), file('b.png')], null)
    expect(plan.accept.map(f => f.name)).toEqual(['a.png'])
    expect(plan.overCount.map(f => f.name)).toEqual(['b.png'])
  })

  it('refuses a file above the per-file limit without using up a slot', () => {
    const plan = planAdd(0, 0, [file('big.mov', 3 * MB, 'video/mp4'), file('ok.png', MB)], 2)
    expect(plan.tooLarge.map(f => f.name)).toEqual(['big.mov'])
    expect(plan.accept.map(f => f.name)).toEqual(['ok.png'])
  })

  it('takes a file exactly at the limit', () => {
    expect(planAdd(0, 0, [file('a.png', 2 * MB)], 2).accept).toHaveLength(1)
  })

  it('keeps a message below the total size limit', () => {
    const plan = planAdd(1, 60 * MB, [file('a.mov', 50 * MB, 'video/mp4'), file('b.png', MB)], null)
    expect(plan.overTotal.map(f => f.name)).toEqual(['a.mov'])
    expect(plan.accept.map(f => f.name)).toEqual(['b.png'])
    expect(MAX_TOTAL_BYTES).toBe(100 * MB)
  })

  it('lets one file above the total limit go alone', () => {
    expect(planAdd(0, 0, [file('huge.mov', 150 * MB, 'video/mp4')], null).accept).toHaveLength(1)
    expect(planAdd(1, MB, [file('huge.mov', 150 * MB, 'video/mp4')], null).overTotal).toHaveLength(1)
  })
})
