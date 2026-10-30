import type { DirectiveBinding, ObjectDirective } from 'vue'
import { charCount, clampChars } from '../lib/textLength'

type Field = HTMLInputElement | HTMLTextAreaElement
type MaxCharsElement = Field & {
  __maxChars?: number | undefined
  __maxCharsOff?: () => void
}

// v-max-chars="24" limits a text field to 24 characters as the server counts
// them. It replaces the maxlength attribute, which counts UTF-16 units and
// would stop at half the limit for emoji. A longer value (typed, pasted or
// dropped) is cut to the limit at a character boundary and v-model is told.
// Text being composed (IME) is left alone until the composition ends.
export const maxChars: ObjectDirective<MaxCharsElement, number> = {
  mounted(el, binding) {
    mark(el, binding)
    let composing = false
    const enforce = () => {
      const max = el.__maxChars
      if (composing || max === undefined || charCount(el.value) <= max) return
      const caret = el.selectionStart
      const cut = clampChars(el.value, max)
      el.value = cut
      if (caret !== null) el.setSelectionRange(Math.min(caret, cut.length), Math.min(caret, cut.length))
      el.dispatchEvent(new Event('input', { bubbles: true }))
    }
    const start = () => { composing = true }
    const end = () => { composing = false; enforce() }
    el.addEventListener('input', enforce)
    el.addEventListener('compositionstart', start)
    el.addEventListener('compositionend', end)
    el.__maxCharsOff = () => {
      el.removeEventListener('input', enforce)
      el.removeEventListener('compositionstart', start)
      el.removeEventListener('compositionend', end)
    }
  },
  updated(el, binding) {
    mark(el, binding)
  },
  unmounted(el) {
    el.__maxCharsOff?.()
  },
}

// The limit is kept on the element (data-max-chars) for the enforcement above and for tests and styles to read.
function mark(el: MaxCharsElement, binding: DirectiveBinding<number>) {
  const max = limit(binding)
  el.__maxChars = max
  if (max === undefined) delete el.dataset.maxChars
  else el.dataset.maxChars = String(max)
}

function limit(binding: DirectiveBinding<number>): number | undefined {
  return Number.isFinite(binding.value) && binding.value > 0 ? binding.value : undefined
}
