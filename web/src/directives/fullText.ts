import type { ObjectDirective } from 'vue'

// v-full-text on a one-line text that truncates with an ellipsis (a channel,
// member or file name): when the pointer or the keyboard reaches it while it
// is cut, the browser's own title shows the whole text; a text that fits gets
// no title, so nothing pops up for names that are already whole. The element
// carries data-full-text, which tells the layout checks (e2e clipping sweep)
// that its full text is available.

interface FullTextElement extends HTMLElement {
  __fullTextShow?: () => void
}

function cut(el: HTMLElement): boolean {
  return el.scrollWidth > el.clientWidth + 1 || el.scrollHeight > el.clientHeight + 1
}

/** Sets or clears the title for the element's current size and text. */
export function refreshFullText(el: HTMLElement): void {
  const text = (el.textContent ?? '').replace(/\s+/g, ' ').trim()
  if (text && cut(el)) el.setAttribute('title', text)
  else el.removeAttribute('title')
}

export const fullText: ObjectDirective<FullTextElement> = {
  mounted(el) {
    el.setAttribute('data-full-text', '')
    const show = () => refreshFullText(el)
    el.__fullTextShow = show
    // The pointer often lands on the row around the text, so listen there too.
    const host = el.closest('button, a, [role="button"], [tabindex]') ?? el
    host.addEventListener('pointerenter', show)
    host.addEventListener('focusin', show)
  },
  updated(el) {
    if (el.hasAttribute('title')) refreshFullText(el)
  },
  beforeUnmount(el) {
    const show = el.__fullTextShow
    if (!show) return
    const host = el.closest('button, a, [role="button"], [tabindex]') ?? el
    host.removeEventListener('pointerenter', show)
    host.removeEventListener('focusin', show)
  }
}
