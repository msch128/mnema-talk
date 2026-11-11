import { afterEach, describe, expect, it } from 'vitest'
import { installRangeFill, paintRange } from './rangeFill'

function slider(attrs: Record<string, string>): HTMLInputElement {
  const input = document.createElement('input')
  input.type = 'range'
  for (const [key, value] of Object.entries(attrs)) input.setAttribute(key, value)
  return input
}

describe('range fill', () => {
  afterEach(() => { document.body.replaceChildren() })

  it('fills a slider in proportion to its value inside its own bounds', () => {
    const input = slider({ min: '0', max: '200', value: '50' })
    paintRange(input)
    expect(input.style.getPropertyValue('--fill')).toBe('25%')
    const offset = slider({ min: '10', max: '20', value: '15' })
    paintRange(offset)
    expect(offset.style.getPropertyValue('--fill')).toBe('50%')
  })

  it('treats a missing range as 0 to 100 and an empty one as unfilled', () => {
    const plain = slider({ value: '40' })
    paintRange(plain)
    expect(plain.style.getPropertyValue('--fill')).toBe('40%')
    const empty = slider({ min: '5', max: '5', value: '5' })
    paintRange(empty)
    expect(empty.style.getPropertyValue('--fill')).toBe('0%')
  })

  it('paints sliders already on the page, new ones, and follows the user', async () => {
    const early = slider({ min: '0', max: '10', value: '5' })
    document.body.append(early)
    const stop = installRangeFill()
    expect(early.style.getPropertyValue('--fill')).toBe('50%')

    const wrapper = document.createElement('div')
    wrapper.append(slider({ min: '0', max: '10', value: '10' }))
    document.body.append(wrapper)
    const direct = slider({ min: '0', max: '4', value: '1' })
    document.body.append(direct)
    const text = document.createElement('input')
    document.body.append(text)
    await Promise.resolve()
    expect(wrapper.querySelector('input')?.style.getPropertyValue('--fill')).toBe('100%')
    expect(direct.style.getPropertyValue('--fill')).toBe('25%')
    expect(text.style.getPropertyValue('--fill')).toBe('')

    early.value = '10'
    early.dispatchEvent(new Event('input', { bubbles: true }))
    expect(early.style.getPropertyValue('--fill')).toBe('100%')
    early.value = '0'
    early.dispatchEvent(new Event('pointerover', { bubbles: true }))
    expect(early.style.getPropertyValue('--fill')).toBe('0%')
    text.dispatchEvent(new Event('input', { bubbles: true }))
    expect(text.style.getPropertyValue('--fill')).toBe('')

    stop()
    early.value = '5'
    early.dispatchEvent(new Event('input', { bubbles: true }))
    expect(early.style.getPropertyValue('--fill')).toBe('0%')
  })
})
