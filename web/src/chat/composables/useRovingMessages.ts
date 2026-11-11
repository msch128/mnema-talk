// One Tab stop for a message list. Every row used to be a stop, together with
// its author name, links and action buttons: the composer was hundreds of Tab
// presses away. Now the row that last held focus (the newest one to begin
// with) is the only stop; arrow keys move between rows (useMessageKeyboard)
// and Tab inside a focused row still walks that row's own buttons and links.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { Ref } from 'vue'

const INNER_STOPS = 'a[href], button, input, textarea, select, summary, [contenteditable="true"], [tabindex]'
const SAVED = 'data-roving-tabindex'

function rowOf(el: EventTarget | null): HTMLElement | null {
  return el instanceof Element ? el.closest<HTMLElement>('[data-msg-id]') : null
}

/** Takes the controls inside a row out of the Tab order, or puts them back. */
function setInnerStops(row: HTMLElement, tabbable: boolean) {
  for (const el of row.querySelectorAll<HTMLElement>(INNER_STOPS)) {
    if (el === row) continue
    if (tabbable) {
      const saved = el.getAttribute(SAVED)
      if (saved === null) continue
      if (saved === '') el.removeAttribute('tabindex')
      else el.setAttribute('tabindex', saved)
      el.removeAttribute(SAVED)
    } else if (el.getAttribute(SAVED) === null && el.getAttribute('tabindex') !== '-1') {
      el.setAttribute(SAVED, el.getAttribute('tabindex') ?? '')
      el.setAttribute('tabindex', '-1')
    }
  }
}

/**
 * @param container  the scrolling element that holds the rows
 * @param ids        message ids in display order
 * Returns `stopId`: the id of the row that is the list's Tab stop.
 */
export function useRovingMessages(container: Ref<HTMLElement | null>, ids: () => string[]) {
  const focused = ref<string | null>(null)
  const stopId = computed(() => {
    const all = ids()
    return focused.value && all.includes(focused.value) ? focused.value : (all[all.length - 1] ?? null)
  })

  function apply() {
    for (const row of container.value?.querySelectorAll<HTMLElement>('[data-msg-id]') ?? []) {
      setInnerStops(row, row.dataset.msgId === stopId.value)
    }
  }

  function onFocusIn(e: FocusEvent) {
    const id = rowOf(e.target)?.dataset.msgId
    if (id) focused.value = id
  }

  let observer: MutationObserver | null = null
  onMounted(() => {
    container.value?.addEventListener('focusin', onFocusIn)
    // Rows and their content (markdown, embeds) arrive after the first render.
    observer = new MutationObserver(apply)
    if (container.value) observer.observe(container.value, { childList: true, subtree: true })
    apply()
  })
  onBeforeUnmount(() => {
    container.value?.removeEventListener('focusin', onFocusIn)
    observer?.disconnect()
  })
  watch(stopId, () => queueMicrotask(apply))

  return { stopId }
}
