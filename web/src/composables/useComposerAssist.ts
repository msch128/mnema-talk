// Composer helpers shared by the channel chat, threads and the Talk chat:
// @mention autocomplete and inserting text (emoji) at the caret.
import { ref, computed, nextTick } from 'vue'
import type { Ref } from 'vue'
import type { MentionSuggestion } from '../lib/mentionQuery'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { usePermissionsStore } from '../stores/permissions'
import { findMentionQuery, suggestMentions, applyMention } from '../lib/mentionQuery'
import { findEmojiQuery, suggestEmoji, suggestStandardEmoji, applyEmoji, applyStandardEmoji } from '../lib/emojiQuery'
import { isImeKey } from './useComposerKeys'
import { useEmojiShortcodes } from '../lib/emojiShortcodes'
import { useEmojiStore } from '../stores/emoji'
import type { CustomEmoji } from '../lib/customEmoji'

/** Inserts text at the caret (e.g. an emoji) and keeps focus. A block starts on a line of its own. */
async function insertAtCaret(el: HTMLTextAreaElement | null, model: Ref<string>, text: string, { block = false }: { block?: boolean } = {}) {
  const value = model.value || ''
  const start = el ? el.selectionStart ?? value.length : value.length
  const end = el ? el.selectionEnd ?? value.length : value.length
  const str = block && start > 0 && value[start - 1] !== '\n' ? `\n${text}` : text
  model.value = value.slice(0, start) + str + value.slice(end)
  await nextTick()
  if (el) {
    el.focus()
    const caret = start + str.length
    el.setSelectionRange(caret, caret)
  }
}

// The roles the member may mention here: mentionable ones, or any with
// "Mention everyone". The server decides again when the message is sent.
function useMentionableRoles() {
  const chatStore = useChatStore()
  const permissions = usePermissionsStore()
  return computed(() => {
    const channelId = chatStore.activeChannel?.id
    const mayAll = channelId ? permissions.canIn(channelId, 'mention_everyone') : permissions.can('mention_everyone')
    return permissions.roles.filter(r => !r.is_everyone && !r.managed && (r.mentionable || mayAll))
  })
}

const EMOJI_LIMIT = 8

/** The community's custom emoji first, then standard ones, at most EMOJI_LIMIT together. */
function emojiSuggestions(query: string, items: CustomEmoji[], standard: Readonly<Record<string, string>>): MentionSuggestion[] {
  const custom: MentionSuggestion[] = suggestEmoji(query, items, EMOJI_LIMIT).map(emoji => ({ username: emoji.name, display_name: `:${emoji.name}:`, emoji }))
  const named = new Set(items.map(e => e.name))
  const plain: MentionSuggestion[] = suggestStandardEmoji(query, standard, EMOJI_LIMIT)
    .filter(e => !named.has(e.code))
    .map(e => ({ username: e.code, display_name: `:${e.code}:`, standard: e.glyph }))
  return [...custom, ...plain].slice(0, EMOJI_LIMIT)
}

/**
 * textarea: ref to the <textarea>; model: ref holding its text.
 * Wire onInput to @input/@click/@keyup and call onKeydown first in the
 * textarea's keydown handler: when it returns true the key was consumed.
 */
export function useComposerAssist(textarea: Ref<HTMLTextAreaElement | null>, model: Ref<string>) {
  const chatStore = useChatStore()
  const authStore = useAuthStore()
  const emojiStore = useEmojiStore()

  const mentionableRoles = useMentionableRoles()

  const query = ref<ReturnType<typeof findMentionQuery>>(null) // { start, query } while a mention is being typed
  const emojiQuery = ref<ReturnType<typeof findEmojiQuery>>(null) // the same for a custom emoji (":que")
  const { table: standardTable, ensure: loadStandardEmoji } = useEmojiShortcodes() // ":smile:" -> glyph, loaded on first ":"
  const active = ref(0)
  const dismissedAt = ref(-1)

  const suggestions = computed((): MentionSuggestion[] => {
    if (emojiQuery.value) return emojiSuggestions(emojiQuery.value.query, emojiStore.items, standardTable.value)
    if (!query.value) return []
    return suggestMentions(query.value.query, chatStore.members, { selfId: authStore.user?.id ?? null, roles: mentionableRoles.value })
  })
  const open = computed(() => suggestions.value.length > 0)

  function refresh() {
    const el = textarea.value
    if (!el) return
    const q = findMentionQuery(model.value, el.selectionStart)
    query.value = q && q.start !== dismissedAt.value ? q : null
    const e = q ? null : findEmojiQuery(model.value, el.selectionStart)
    emojiQuery.value = e && e.start !== dismissedAt.value ? e : null
    if (emojiQuery.value) void loadStandardEmoji()
    if (!q && !e) dismissedAt.value = -1
    if (active.value >= suggestions.value.length) active.value = 0
  }

  function onInput() {
    active.value = 0
    refresh()
  }

  function close() {
    const typing = query.value ?? emojiQuery.value
    if (typing) dismissedAt.value = typing.start
    query.value = emojiQuery.value = null
  }

  async function pick(item: MentionSuggestion | undefined) {
    const el = textarea.value
    const typing = query.value ?? emojiQuery.value
    if (!typing || !el || !item) return
    const apply = item.standard ? applyStandardEmoji : item.emoji ? applyEmoji : applyMention
    const { text, caret } = apply(model.value, typing.start, el.selectionStart, item.standard ?? item.username)
    model.value = text
    query.value = emojiQuery.value = null
    await nextTick()
    el.focus()
    el.setSelectionRange(caret, caret)
  }

  /** Handles navigation keys while suggestions are open; true if consumed. */
  function onKeydown(e: KeyboardEvent) {
    if (!open.value || isImeKey(e)) return false
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

  const insertText = (str: string, options?: { block?: boolean }) => insertAtCaret(textarea.value, model, str, options)

  return { suggestions, active, open, onInput, onKeydown, pick, close, insertText }
}
