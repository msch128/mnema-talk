// Accessibility behaviour shared by every modal: focus moves into the dialog,
// Tab stays inside, Escape closes the topmost dialog, and focus returns to the
// element that opened it.
import { onMounted, onBeforeUnmount, nextTick } from 'vue'
import type { Ref } from 'vue'

const FOCUSABLE = [
  'a[href]', 'button:not([disabled])', 'input:not([disabled]):not([type="hidden"])',
  'select:not([disabled])', 'textarea:not([disabled])', '[tabindex]:not([tabindex="-1"])'
].join(',')

// Open dialogs, innermost last. Only the last one reacts to keys.
const stack: object[] = []

export function focusableIn(root: HTMLElement) {
  return [...root.querySelectorAll<HTMLElement>(FOCUSABLE)].filter(el => !el.hasAttribute('inert') && el.getAttribute('aria-hidden') !== 'true')
}

/**
 * @param containerRef ref to the dialog element
 * @param opts.onClose called on Escape
 * @param opts.initialFocus selector or () => HTMLElement; default: [data-autofocus],
 *        else the first focusable element that is not a close button
 */
export function useDialog(containerRef: Ref<HTMLElement | null>, { onClose, initialFocus }: { onClose?: () => void; initialFocus?: string | ((root: HTMLElement) => HTMLElement | null | undefined) } = {}) {
  let opener: HTMLElement | null = null
  const token = {}

  function onKeydown(e: KeyboardEvent) {
    if (stack[stack.length - 1] !== token) return
    const root = containerRef.value
    if (!root) return
    if (e.key === 'Escape') {
      e.stopPropagation()
      e.preventDefault()
      onClose?.()
      return
    }
    if (e.key !== 'Tab') return
    const items = focusableIn(root)
    if (!items.length) {
      e.preventDefault()
      root.focus()
      return
    }
    const first = items[0]
    const last = items[items.length - 1]
    const active = document.activeElement
    if (e.shiftKey && (active === first || !root.contains(active))) {
      e.preventDefault()
      last?.focus()
    } else if (!e.shiftKey && (active === last || !root.contains(active))) {
      e.preventDefault()
      first?.focus()
    }
  }

  function focusInitial() {
    const root = containerRef.value
    if (!root) return
    let target: HTMLElement | null | undefined = null
    if (typeof initialFocus === 'function') target = initialFocus(root)
    else if (typeof initialFocus === 'string') target = root.querySelector<HTMLElement>(initialFocus)
    target = target || root.querySelector<HTMLElement>('[data-autofocus]')
    target = target || focusableIn(root).find(el => !el.hasAttribute('data-dialog-close'))
    ;(target || root).focus()
  }

  onMounted(async () => {
    opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
    stack.push(token)
    document.addEventListener('keydown', onKeydown, true)
    await nextTick()
    focusInitial()
  })

  onBeforeUnmount(() => {
    document.removeEventListener('keydown', onKeydown, true)
    const i = stack.indexOf(token)
    if (i !== -1) stack.splice(i, 1)
    if (opener && opener.isConnected && typeof opener.focus === 'function') opener.focus()
  })
}
