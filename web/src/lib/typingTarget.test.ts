import { afterEach, describe, expect, it } from 'vitest'
import { isTypingEvent } from './typingTarget'

afterEach(() => { document.body.innerHTML = '' })

// Checks the event the way a document-level listener sees it.
function keyFrom(target: Element) {
  const seen: { target: EventTarget | null, typing: boolean }[] = []
  const listener = (e: Event) => { seen.push({ target: e.target, typing: isTypingEvent(e) }) }
  document.addEventListener('keydown', listener)
  target.dispatchEvent(new KeyboardEvent('keydown', { key: 's', bubbles: true, composed: true }))
  document.removeEventListener('keydown', listener)
  const first = seen[0]
  if (!first) throw new Error('no event')
  return first
}

describe('isTypingEvent', () => {
  it('sees an input inside a shadow root although the event is retargeted to the host', () => {
    // The test DOM does not retarget, so the browser's view is built by hand:
    // the target is the host, the composed path starts at the inner input.
    const host = document.createElement('div')
    document.body.appendChild(host)
    const input = document.createElement('input')
    host.attachShadow({ mode: 'open' }).appendChild(input)
    const e = new KeyboardEvent('keydown', { key: 's' })
    Object.defineProperty(e, 'target', { value: host })
    Object.defineProperty(e, 'composedPath', { value: () => [input, host, document.body] })
    expect(isTypingEvent(e)).toBe(true)
  })

  it('counts text fields, selects and editable regions, not buttons', () => {
    const editable = document.createElement('div')
    editable.contentEditable = 'true'
    const inner = document.createElement('span')
    editable.appendChild(inner)
    const button = document.createElement('button')
    document.body.append(document.createElement('textarea'), document.createElement('select'), editable, button)
    expect(keyFrom(document.querySelector('textarea')!).typing).toBe(true)
    expect(keyFrom(document.querySelector('select')!).typing).toBe(true)
    expect(keyFrom(inner).typing).toBe(true)
    expect(keyFrom(button).typing).toBe(false)
  })
})
