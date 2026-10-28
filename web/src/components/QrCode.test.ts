import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import QrCode from './QrCode.vue'

describe('QrCode', () => {
  it('keeps the four-module quiet zone the QR spec asks for', () => {
    const w = mount(QrCode, { props: { text: 'otpauth://totp/x?secret=ABC', label: 'QR' } })
    const size = Number(w.get('svg').attributes('viewBox')!.split(' ')[2])
    // Version 1 is 21 modules wide and every version adds 4: the margin is what is left.
    expect((size - 21) % 4).toBe(0)
    const first = /^M(\d+) (\d+)/.exec(w.get('path').attributes('d')!)!
    // The finder pattern's corner sits in the top-left module, right after the quiet zone.
    expect([Number(first[1]), Number(first[2])]).toEqual([4, 4])
  })
})
