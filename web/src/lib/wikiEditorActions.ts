// The formatting buttons of the wiki editor: each wraps the selection or a line
// in the Markdown the renderer reads. Pure: text and selection in, text and
// selection out, so the textarea only has to apply the result.

export interface EditResult { text: string; start: number; end: number }
export type EditAction = 'bold' | 'italic' | 'code' | 'link' | 'wikilink' | 'heading' | 'list' | 'quote'

const WRAP: Partial<Record<EditAction, [string, string, string]>> = {
  bold: ['**', '**', 'bold'],
  italic: ['*', '*', 'italic'],
  code: ['`', '`', 'code'],
  link: ['[', '](https://)', 'text'],
  wikilink: ['[[', ']]', 'Page']
}
const PREFIX: Partial<Record<EditAction, string>> = { heading: '## ', list: '- ', quote: '> ' }

/** Applies an action to the text around the selection [start, end). */
export function applyEdit(text: string, start: number, end: number, action: EditAction): EditResult {
  const wrap = WRAP[action]
  if (wrap) return wrapSelection(text, start, end, wrap)
  return prefixLines(text, start, end, PREFIX[action] ?? '')
}

function wrapSelection(text: string, start: number, end: number, [open, close, placeholder]: [string, string, string]): EditResult {
  const selected = text.slice(start, end) || placeholder
  const next = text.slice(0, start) + open + selected + close + text.slice(end)
  return { text: next, start: start + open.length, end: start + open.length + selected.length }
}

/** Puts the prefix before every line the selection touches, or takes it off when all of them have it. */
function prefixLines(text: string, start: number, end: number, prefix: string): EditResult {
  const from = text.lastIndexOf('\n', start - 1) + 1
  const nl = text.indexOf('\n', end)
  const to = nl < 0 ? text.length : nl
  const lines = text.slice(from, to).split('\n')
  const all = lines.every(l => l.startsWith(prefix))
  const changed = lines.map(l => (all ? l.slice(prefix.length) : prefix + l)).join('\n')
  return { text: text.slice(0, from) + changed + text.slice(to), start: from, end: from + changed.length }
}

/**
 * Puts `next` into the textarea the way typing would, so the browser's own undo
 * (Ctrl+Z) still takes the change back; assigning `value` would wipe that history.
 * Only the part that differs is replaced. False when the browser cannot do it:
 * the caller then sets the text itself.
 */
export function replaceAsTyped(el: HTMLTextAreaElement, next: string): boolean {
  if (typeof document.execCommand !== 'function') return false
  const before = el.value
  let head = 0
  while (head < before.length && head < next.length && before[head] === next[head]) head++
  let tail = 0
  while (tail < before.length - head && tail < next.length - head && before[before.length - 1 - tail] === next[next.length - 1 - tail]) tail++
  el.focus()
  el.setSelectionRange(head, before.length - tail)
  const insert = next.slice(head, next.length - tail)
  try {
    document.execCommand(insert === '' ? 'delete' : 'insertText', false, insert)
  } catch {
    return false
  }
  return el.value === next
}
