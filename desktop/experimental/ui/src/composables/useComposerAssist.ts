// Composer helpers shared by the channel chat, threads and the Talk chat:
// @mention autocomplete and inserting text (emoji) at the caret.
import { ref, computed, nextTick } from 'vue'
import type { Ref } from 'vue'
import type { MentionSuggestion } from '../lib/mentionQuery'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { findMentionQuery, suggestMentions, applyMention } from '../lib/mentionQuery'

/**
 * textarea: ref to the <textarea>; model: ref holding its text.
 * Wire onInput to @input/@click/@keyup and call onKeydown first in the
 * textarea's keydown handler: when it returns true the key was consumed.
 */
export function useComposerAssist(textarea: Ref<HTMLTextAreaElement | null>, model: Ref<string>) {
  const chatStore = useChatStore()
  const authStore = useAuthStore()

  const query = ref<ReturnType<typeof findMentionQuery>>(null) // { start, query } while a mention is being typed
  const active = ref(0)
  const dismissedAt = ref(-1)

  const suggestions = computed(() => {
    if (!query.value) return []
    return suggestMentions(query.value.query, chatStore.members, { selfId: authStore.user?.id ?? null })
  })
  const open = computed(() => suggestions.value.length > 0)

  function refresh() {
    const el = textarea.value
    if (!el) return
    const q = findMentionQuery(model.value, el.selectionStart)
    query.value = q && q.start !== dismissedAt.value ? q : null
    if (!q) dismissedAt.value = -1
    if (active.value >= suggestions.value.length) active.value = 0
  }

  function onInput() {
    active.value = 0
    refresh()
  }

  function close() {
    if (query.value) dismissedAt.value = query.value.start
    query.value = null
  }

  async function pick(item: MentionSuggestion | undefined) {
    const el = textarea.value
    if (!query.value || !el || !item) return
    const { text, caret } = applyMention(model.value, query.value.start, el.selectionStart, item.username)
    model.value = text
    query.value = null
    await nextTick()
    el.focus()
    el.setSelectionRange(caret, caret)
  }

  /** Handles navigation keys while suggestions are open; true if consumed. */
  function onKeydown(e: KeyboardEvent) {
    if (!open.value) return false
    const n = suggestions.value.length
    if (e.key === 'ArrowDown') {
      active.value = (active.value + 1) % n
    } else if (e.key === 'ArrowUp') {
      active.value = (active.value - 1 + n) % n
    } else if ((e.key === 'Enter' && !e.shiftKey) || e.key === 'Tab') {
      pick(suggestions.value[active.value])
    } else if (e.key === 'Escape') {
      close()
    } else {
      return false
    }
    e.preventDefault()
    e.stopPropagation()
    return true
  }

  /** Inserts text at the caret (e.g. an emoji) and keeps focus. */
  async function insertText(str: string) {
    const el = textarea.value
    const value = model.value || ''
    const start = el ? el.selectionStart ?? value.length : value.length
    const end = el ? el.selectionEnd ?? value.length : value.length
    model.value = value.slice(0, start) + str + value.slice(end)
    await nextTick()
    if (el) {
      el.focus()
      const caret = start + str.length
      el.setSelectionRange(caret, caret)
    }
  }

  return { suggestions, active, open, onInput, onKeydown, pick, close, insertText }
}
