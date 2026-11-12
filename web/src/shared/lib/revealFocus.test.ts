import { afterEach, describe, expect, it } from 'vitest'
import { focusRevealed, placeOf } from './revealFocus'

afterEach(() => { document.body.innerHTML = '' })

describe('focus after Show', () => {
  // A thread root is in the message list and in the thread panel at once.
  it('goes to the row in the place where Show was pressed, not to the first row with that id', () => {
    document.body.innerHTML = `
      <div id="list"><div data-msg-id="m1" tabindex="-1">list row</div></div>
      <div id="thread"><div id="placeholder"><button id="show">Show</button></div></div>`
    const around = placeOf(document.getElementById('show'))
    const thread = document.getElementById('thread') as HTMLElement
    thread.innerHTML = '<div data-msg-id="m1" tabindex="-1">thread row</div>'
    focusRevealed(null, null, 'm1', around)
    expect(document.activeElement?.textContent).toBe('thread row')
  })

  it('falls back to a row further out when the place holds none', () => {
    document.body.innerHTML = `
      <div id="list"><div data-reply-id="m2" tabindex="-1">only row</div></div>
      <div id="thread"><button id="show">Show</button></div>`
    const around = placeOf(document.getElementById('show'))
    focusRevealed(null, null, 'm2', around)
    expect(document.activeElement?.textContent).toBe('only row')
  })
})
