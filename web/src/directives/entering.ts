import type { Directive } from 'vue'

// v-entering="key": when the key takes a new, non-empty value (a channel was
// opened and its messages are there, a list was opened), the element carries
// data-entering for a moment, so its rows arrive 30 ms apart
// (styles/motion.css). Rows that come later, while the key stays the same,
// arrive without it. v-entering.end staggers the last rows instead of the
// first: a timeline that sits at the bottom.
const HOLD_MS = 600
const timers = new WeakMap<HTMLElement, ReturnType<typeof setTimeout>>()

function mark(el: HTMLElement, end: boolean) {
  clearTimeout(timers.get(el))
  el.dataset.entering = end ? 'end' : 'start'
  timers.set(el, setTimeout(() => { delete el.dataset.entering }, HOLD_MS))
}

export const entering: Directive<HTMLElement, unknown> = {
  mounted(el, binding) {
    if (binding.value) mark(el, !!binding.modifiers.end)
  },
  updated(el, binding) {
    if (binding.value && binding.value !== binding.oldValue) mark(el, !!binding.modifiers.end)
  },
  unmounted(el) {
    clearTimeout(timers.get(el))
  }
}
