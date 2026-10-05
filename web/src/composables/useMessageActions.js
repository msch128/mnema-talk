// What every message list does with its messages: edit, delete, react,
// upload. Shared by the channel chat, the thread panel and the Talk chat.
import { ref, toValue, watch } from 'vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'
import { useDismissable } from './useDismissable'
import { previewText } from '../lib/replies'
import { confirm } from '../lib/confirm'
import { t, locale } from '../i18n'

export function formatTime(dateStr) {
  if (!dateStr) return ''
  return new Date(dateStr).toLocaleTimeString([locale.value], { hour: '2-digit', minute: '2-digit' })
}

// Reaction pickers live in elements with this class; presses inside one
// don't close the open picker.
export const PICKER_ANCHOR = 'reaction-picker-anchor'

/**
 * @param options.container ref/getter of the element holding the list; the
 *                          open reaction picker closes on presses outside its
 *                          anchors within it and on Escape. Rows in it carry
 *                          data-msg-id or data-reply-id.
 * @param options.deleteTitle i18n key of the delete confirmation title.
 */
export function useMessageActions({ container = null, deleteTitle = 'chat.deleteTitle' } = {}) {
  const chatStore = useChatStore()
  const authStore = useAuthStore()
  const toasts = useToastStore()

  const editingId = ref(null)
  const editText = ref('')
  const isSavingEdit = ref(false)
  // Which reaction picker is open: a message id (hover bar) or `bottom-<id>`
  // (the add button next to the reactions).
  const pickerId = ref(null)
  const isUploading = ref(false)

  // Leaving the editor drops focus with it: hand it back to the message row
  // so keyboard users stay where they were.
  watch(editingId, (now, was) => {
    if (!was || now) return
    const active = document.activeElement
    if (active && active !== document.body && active.isConnected) return
    toValue(container)?.querySelector(`[data-msg-id="${was}"], [data-reply-id="${was}"]`)?.focus()
  }, { flush: 'post' })

  function channelOf(msg) {
    return chatStore.activeChannel?.id || msg.channel_id
  }

  function isOwn(msg) {
    return !!authStore.user?.id && msg.user_id === authStore.user.id
  }

  function canDelete(msg) {
    return isOwn(msg) || authStore.isAdmin
  }

  function startEdit(msg) {
    editingId.value = msg.id
    editText.value = msg.content
    pickerId.value = null
  }

  function cancelEdit() {
    editingId.value = null
    editText.value = ''
  }

  async function saveEdit(msg) {
    const text = editText.value.trim()
    if (!text || isSavingEdit.value) return
    isSavingEdit.value = true
    try {
      await chatStore.editMessage(channelOf(msg), msg.id, text)
      cancelEdit()
    } catch (err) {
      toasts.error(err.message || t('chat.editFailed'))
    } finally {
      isSavingEdit.value = false
    }
  }

  async function deleteMessage(msg) {
    const ok = await confirm({
      title: t(deleteTitle),
      body: t('chat.deleteBody'),
      excerpt: previewText(msg.content).slice(0, 160) || (msg.attachments?.length ? t('chat.attachment') : ''),
      confirmLabel: t('common.delete'),
      danger: true
    })
    if (!ok) return
    try {
      await chatStore.deleteMessage(channelOf(msg), msg.id)
    } catch (err) {
      toasts.error(err.message || t('chat.deleteFailed'))
    }
  }

  async function toggleReaction(msgId, emoji) {
    pickerId.value = null
    try {
      await chatStore.toggleReaction(msgId, emoji)
    } catch (err) {
      console.warn('Reaction error:', err)
    }
  }

  function togglePicker(id) {
    pickerId.value = pickerId.value === id ? null : id
  }

  function closePicker() {
    pickerId.value = null
  }

  useDismissable(
    () => toValue(container)?.querySelectorAll(`.${PICKER_ANCHOR}`) ?? [],
    closePicker,
    { active: () => pickerId.value !== null }
  )

  // Sends the file chosen in a file input. The input is cleared afterwards
  // either way, so the same file can be picked again after a failure.
  // Resolves to whether the upload went through.
  async function upload(input, send) {
    const file = input?.files?.[0]
    if (!file) return false
    isUploading.value = true
    try {
      await send(file)
      return true
    } catch (err) {
      toasts.error(err.message || t('chat.uploadFailed'))
      return false
    } finally {
      isUploading.value = false
      if (input) input.value = ''
    }
  }

  return {
    editingId, editText, isSavingEdit, startEdit, cancelEdit, saveEdit,
    deleteMessage, toggleReaction, isOwn, canDelete,
    pickerId, togglePicker, closePicker,
    isUploading, upload
  }
}
