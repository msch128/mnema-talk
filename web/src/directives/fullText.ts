import type { ObjectDirective } from 'vue'
import { hideTip, showTip, type TooltipElement } from './tooltip'
import { holdToShow } from './tooltipTouch'

// v-full-text on a one-line text that truncates with an ellipsis (a channel,
// member or file name) or a clamped preview. While the text is cut it carries
// the whole text as its title (kept current as its box resizes), and the
// whole text shows in the shared tooltip on keyboard focus and on a long
// press, since a title shows only to a mouse. A text that fits gets nothing,
// so nothing pops up for names that are already whole. data-full-text tells
// the layout checks (e2e clipping sweep) which mechanism is in place.
// Where the surrounding control has a tooltip of its own (v-tooltip), that
// tooltip speaks for it on focus and touch.

interface FullTextElement extends TooltipElement {
  __fullTextOff?: () => void
}

/** Whether the element's text is cut by its box right now (ellipsis or clamp). */
export function isTextCut(el: HTMLElement | null | undefined): boolean {
  return !!el && (el.scrollWidth > el.clientWidth + 1 || el.scrollHeight > el.clientHeight + 1)
}

/** Sets or clears the title for the element's current size and text. */
export function refreshFullText(el: HTMLElement): void {
  const text = (el.textContent ?? '').replace(/\s+/g, ' ').trim()
  if (text && isTextCut(el)) el.setAttribute('title', text)
  else el.removeAttribute('title')
}

// One observer for every cut-text element: a sidebar has hundreds of them.
let sizes: ResizeObserver | null = null
function watchSize(el: HTMLElement): () => void {
  if (typeof ResizeObserver === 'undefined') return () => undefined
  sizes ??= new ResizeObserver(entries => { for (const e of entries) if (e.target instanceof HTMLElement) refreshFullText(e.target) })
  const observer = sizes
  observer.observe(el)
  return () => observer.unobserve(el)
}

function hostOf(el: HTMLElement): HTMLElement {
  return el.closest<HTMLElement>('button, a, [role="button"], [tabindex]') ?? el
}

/** Shows the whole text in the shared tooltip, if it is cut and the control has no tooltip of its own. */
function showFull(el: FullTextElement, touch = false): void {
  refreshFullText(el)
  const host = hostOf(el) as TooltipElement
  const title = el.getAttribute('title')
  if (!title || (host !== el && host.__tip)) return
  el.__tip = { text: title, shortcut: '' }
  showTip(el, true, touch)
}

function wire(el: FullTextElement): () => void {
  const host = hostOf(el)
  const focus = () => {
    refreshFullText(el)
    // The tooltip only for the keyboard; a click focuses buttons too.
    try { if (!host.matches(':focus-visible')) return } catch (err) { void err }
    showFull(el)
  }
  const blur = () => hideTip(el)
  const enter = () => refreshFullText(el)
  const unwatch = watchSize(el)
  const offTouch = holdToShow(el, () => showFull(el, true), () => hideTip(el))
  host.addEventListener('pointerenter', enter)
  host.addEventListener('focusin', focus)
  host.addEventListener('focusout', blur)
  return () => {
    unwatch()
    offTouch()
    hideTip(el)
    host.removeEventListener('pointerenter', enter)
    host.removeEventListener('focusin', focus)
    host.removeEventListener('focusout', blur)
  }
}

export const fullText: ObjectDirective<FullTextElement> = {
  mounted(el) {
    el.setAttribute('data-full-text', '')
    refreshFullText(el)
    el.__fullTextOff = wire(el)
  },
  updated(el) {
    refreshFullText(el)
  },
  beforeUnmount(el) {
    el.__fullTextOff?.()
  }
}
