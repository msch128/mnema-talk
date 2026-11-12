// Where focus goes after "Show" reveals a folded message (BlockedFold). The
// Show button leaves the page with the placeholder, so focus would fall to
// <body>; it moves to what was revealed instead, so a keyboard or screen
// reader user stays in place.
const FOCUSABLE = 'button:not([disabled]), a[href], input, textarea, select, [tabindex]'

/** The first focusable element between the two markers, else a control around them, else the first revealed element. */
function revealedTarget(start: HTMLElement, end: HTMLElement | null): HTMLElement | null {
  let first: HTMLElement | null = null
  for (let el = start.nextElementSibling; el instanceof HTMLElement && el !== end; el = el.nextElementSibling) {
    const found = el.matches(FOCUSABLE) ? el : el.querySelector<HTMLElement>(FOCUSABLE)
    if (found) return found
    first ??= el
  }
  const around = start.parentElement?.closest<HTMLElement>('button, a[href], [role="button"]')
  if (around) return around
  if (first) {
    // Focusable from script only; the element keeps its look (no outline of its own).
    first.tabIndex = -1
    first.classList.add('focus:outline-hidden')
  }
  return first
}

/** The elements around a placeholder, innermost first: read before Show, they say where its row was. */
export function placeOf(el: Element | null): HTMLElement[] {
  const around: HTMLElement[] = []
  for (let up = el?.parentElement ?? null; up; up = up.parentElement) around.push(up)
  return around
}

/** The row of a message nearest to where the placeholder was: a message can be in the list and in the thread at once. */
function replacedRow(around: readonly HTMLElement[], messageId: string): HTMLElement | null {
  const row = `[data-msg-id="${CSS.escape(messageId)}"], [data-reply-id="${CSS.escape(messageId)}"]`
  for (const el of around) {
    const found = el.isConnected ? el.querySelector<HTMLElement>(row) : null
    if (found) return found
  }
  return null
}

/**
 * Focus the revealed content. Without markers the placeholder's whole row was
 * replaced (the message list and the thread): the row of that message in the
 * same place takes it.
 */
export function focusRevealed(start: HTMLElement | null, end: HTMLElement | null, messageId: string, around: readonly HTMLElement[]) {
  const target = start ? revealedTarget(start, end) : replacedRow(around, messageId)
  target?.focus({ preventScroll: true })
}
