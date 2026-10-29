// Keyboard editing of your own messages, as in Discord and Slack: ArrowUp in an
// empty composer opens the newest own message in the inline editor, ArrowUp /
// ArrowDown on the first / last line of the editor walk to the next older /
// newer one, and past the newest the composer gets the focus back.
import { nextTick, toValue, watch } from 'vue'
import type { MaybeRefOrGetter } from 'vue'
import type { Message } from '../types/domain'
import type { useMessageActions } from './useMessageActions'
import { shortcodesForEditing } from '../lib/customEmoji'
import { isImeKey } from './useComposerKeys'

type Actions = Pick<ReturnType<typeof useMessageActions>, 'isOwn' | 'editingId' | 'editText' | 'startEdit' | 'cancelEdit'>

function plain(e: KeyboardEvent) {
  return !e.shiftKey && !e.altKey && !e.ctrlKey && !e.metaKey && !isImeKey(e)
}

/**
 * Which way an arrow key leaves the edit box: -1 (older) for ArrowUp with the
 * caret on the first line, 1 (newer) for ArrowDown on the last line, else 0.
 * A selection or a caret inside multi-line text keeps its normal movement.
 */
export function arrowStep(e: KeyboardEvent): -1 | 0 | 1 {
  const el = e.target
  if (!(el instanceof HTMLTextAreaElement) || !plain(e)) return 0
  if (el.selectionStart !== el.selectionEnd) return 0
  if (e.key === 'ArrowUp') return el.value.slice(0, el.selectionStart).includes('\n') ? 0 : -1
  if (e.key === 'ArrowDown') return el.value.indexOf('\n', el.selectionEnd) === -1 ? 1 : 0
  return 0
}

/** Messages the keyboard can open for editing: own, plain text. */
function editable(msg: Message, isOwn: (m: Message) => boolean) {
  return isOwn(msg) && !msg.poll && !msg.forward && !msg.title && !!msg.content.trim()
}

/**
 * @param options.messages  the list in reading order (oldest first)
 * @param options.container the element holding the rows (data-msg-id / data-reply-id)
 * @param options.composer  the composer to hand the focus back to
 */
export function useOwnMessageNav({ messages, actions, container, composer }: {
  messages: () => Message[]
  actions: Actions
  container: MaybeRefOrGetter<HTMLElement | null>
  composer: () => { focus: () => void } | null
}) {
  let fromComposer = false

  const list = () => messages().filter(m => editable(m, actions.isOwn))

  function open(msg: Message) {
    actions.startEdit(msg)
    void nextTick(() => {
      toValue(container)?.querySelector<HTMLElement>(`[data-msg-id="${msg.id}"], [data-reply-id="${msg.id}"]`)?.scrollIntoView?.({ block: 'nearest' })
    })
  }

  /** ArrowUp in the composer. Returns true when it opened an edit (the key is used). */
  function fromComposerKey(e: KeyboardEvent, draft: string): boolean {
    if (e.key !== 'ArrowUp' || !plain(e) || draft !== '' || actions.editingId.value) return false
    const last = list().at(-1)
    if (!last) return false
    e.preventDefault()
    fromComposer = true
    open(last)
    return true
  }

  /** The editor of `msg` asked to move on (see arrowStep). Unsaved text stays where it is. */
  function step(dir: -1 | 1, e: KeyboardEvent, msg: Message) {
    if (actions.editText.value !== shortcodesForEditing(msg.content)) return
    const all = list()
    const at = all.findIndex(m => m.id === msg.id)
    if (at < 0) return
    const next = all[at + dir]
    if (next) {
      e.preventDefault()
      open(next)
    } else if (dir === 1) {
      e.preventDefault()
      actions.cancelEdit()
      composer()?.focus()
    }
  }

  // Leaving the editor (save or Esc) lands in the composer when it was opened from there.
  watch(actions.editingId, (now, was) => {
    if (!was || now || !fromComposer) return
    fromComposer = false
    composer()?.focus()
  }, { flush: 'post' })

  return { fromComposerKey, step }
}
