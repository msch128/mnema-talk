// Keyboard handling on a focused message row, shared by the channel chat and
// the thread panel: arrows move between rows, r replies, e edits (own
// messages), the menu key / Shift+F10 open the message menu, Escape backs out.

function isTyping(target: EventTarget | null) {
  return target instanceof HTMLElement && (['INPUT', 'TEXTAREA'].includes(target.tagName) || target.isContentEditable)
}

/**
 * @param e        keydown event on the row (currentTarget is the row).
 * @param handlers.rows    () => list of focusable rows in order.
 * @param handlers.reply   () => void
 * @param handlers.edit    () => boolean, false when the message can't be edited.
 * @param handlers.menu    (rowEl) => void, optional.
 * @param handlers.escape  () => boolean, true when Escape closed something.
 */
export function handleMessageKeydown(e: KeyboardEvent, { rows, reply, edit, menu, escape }: { rows?: () => Iterable<HTMLElement>; reply?: () => void; edit?: () => boolean | void; menu?: (row: HTMLElement) => void; escape?: () => boolean }) {
  if (isTyping(e.target)) return

  if (menu && (e.key === 'ContextMenu' || (e.key === 'F10' && e.shiftKey))) {
    e.preventDefault()
    if (e.currentTarget instanceof HTMLElement) menu(e.currentTarget)
    return
  }
  const plain = !e.altKey && !e.ctrlKey && !e.metaKey && !e.shiftKey
  if ((e.key === 'ArrowUp' || e.key === 'ArrowDown') && e.target === e.currentTarget && plain) {
    e.preventDefault()
    const list = [...(rows?.() || [])]
    list[list.findIndex(row => row === e.currentTarget) + (e.key === 'ArrowUp' ? -1 : 1)]?.focus()
    return
  }
  // Ctrl/Cmd/Alt combos belong to the browser or OS (reload, find, ...).
  if (e.ctrlKey || e.metaKey || e.altKey) return

  if (e.key === 'r' || e.key === 'R') {
    e.preventDefault()
    reply?.()
  } else if (e.key === 'e' || e.key === 'E') {
    if (edit?.() !== false) e.preventDefault()
  } else if (e.key === 'Escape') {
    if (escape?.()) e.preventDefault()
  }
}
