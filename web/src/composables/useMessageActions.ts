// What every message list does with its messages: edit, delete, react,
// upload. Shared by the channel chat, the thread panel and the Talk chat.
import { ref, toValue, watch } from 'vue'
import type { MaybeRefOrGetter } from 'vue'
import type { Message } from '../types/domain'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'
import { usePermissionsStore } from '../stores/permissions'
import { useDismissable } from './useDismissable'
import { previewText } from '../lib/replies'
import { shortcodesForEditing } from '../lib/customEmoji'
import { confirm } from '../lib/confirm'
import { t, locale } from '../i18n'
import { caughtErrorMessage } from '../lib/api'
import { clockTimeOptions } from '../lib/time'
import { clampChars } from '../lib/textLength'

export function formatTime(dateStr: string | null | undefined) {
  if (!dateStr) return ''
  return new Date(dateStr).toLocaleTimeString([locale.value], clockTimeOptions(locale.value))
}

// Reaction pickers live in elements with this class; presses inside one
// don't close the open picker.
export const PICKER_ANCHOR = 'reaction-picker-anchor'

function confirmDelete(msg: Message, titleKey: string) {
  return confirm({
    title: t(titleKey),
    body: t('chat.deleteBody'),
    excerpt: clampChars(previewText(msg.content), 160) || (msg.attachments?.length ? t('chat.attachment') : ''),
    confirmLabel: t('common.delete'),
    danger: true
  })
}

/** Which reaction picker is open: a message id (hover bar) or `bottom-<id>`. */
function useReactionPicker(container: MaybeRefOrGetter<HTMLElement | null>) {
  const chatStore = useChatStore()
  const pickerId = ref<string | null>(null)

  async function toggleReaction(msgId: string, emoji: string) {
    pickerId.value = null
    try {
      await chatStore.toggleReaction(msgId, emoji)
    } catch (err) {
      console.warn('Reaction error:', err)
    }
  }

  function togglePicker(id: string) {
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
  return { pickerId, toggleReaction, togglePicker, closePicker }
}

/**
 * Sends the file chosen in a file input. The input is cleared afterwards
 * either way, so the same file can be picked again after a failure.
 * `upload` resolves to whether the upload went through.
 */
function useFileUpload() {
  const toasts = useToastStore()
  const permissions = usePermissionsStore()
  const isUploading = ref(false)

  async function upload(input: HTMLInputElement | null, send: (file: File) => unknown | Promise<unknown>) {
    const file = input?.files?.[0]
    if (!input || !file) return false
    const max = permissions.maxUploadMb
    if (max !== null && file.size > max * 1024 * 1024) {
      // Refuse early: the server would only read and drop the body.
      toasts.error(t('chat.uploadTooLarge', { max }), { detail: t('chat.uploadTooLargeDetail', { name: file.name, size: Math.ceil(file.size / (1024 * 1024)) }) })
      input.value = ''
      return false
    }
    isUploading.value = true
    try {
      await send(file)
      return true
    } catch (err) {
      toasts.error(caughtErrorMessage(err, t('chat.uploadFailed')))
      return false
    } finally {
      isUploading.value = false
      input.value = ''
    }
  }
  return { isUploading, upload }
}

function useMessageEditor(channelOf: (msg: Message) => string, onStart?: () => void) {
  const chatStore = useChatStore()
  const toasts = useToastStore()
  const editingId = ref<string | null>(null)
  const editText = ref('')
  const editOriginal = ref('') // the text as the editor opened with it
  const isSavingEdit = ref(false)

  function startEdit(msg: Message) {
    if (msg.poll) return // a poll cannot be edited
    editingId.value = msg.id
    editOriginal.value = shortcodesForEditing(msg.content) // :name:, not the stored token; saving turns it back
    editText.value = editOriginal.value
    onStart?.()
  }

  function cancelEdit() {
    editingId.value = null
    editText.value = ''
  }

  async function saveEdit(msg: Message) {
    const text = editText.value.trim()
    if (!text || isSavingEdit.value) return
    isSavingEdit.value = true
    try {
      await chatStore.editMessage(channelOf(msg), msg.id, text)
      // The member may have moved on to another edit while this one was sent.
      if (editingId.value === msg.id) cancelEdit()
    } catch (err) {
      toasts.error(caughtErrorMessage(err, t('chat.editFailed')))
    } finally {
      isSavingEdit.value = false
    }
  }

  return { editingId, editText, editOriginal, isSavingEdit, startEdit, cancelEdit, saveEdit }
}

/**
 * @param options.container ref/getter of the element holding the list; the
 *                          open reaction picker closes on presses outside its
 *                          anchors within it and on Escape. Rows in it carry
 *                          data-msg-id or data-reply-id.
 * @param options.deleteTitle i18n key of the delete confirmation title.
 */
export function useMessageActions({ container = null, deleteTitle = 'chat.deleteTitle' }: { container?: MaybeRefOrGetter<HTMLElement | null>; deleteTitle?: string } = {}) {
  const chatStore = useChatStore()
  const authStore = useAuthStore()
  const toasts = useToastStore()
  const permissions = usePermissionsStore()
  const { pickerId, toggleReaction, togglePicker, closePicker } = useReactionPicker(container)
  const { isUploading, upload } = useFileUpload()

  function channelOf(msg: Message): string {
    return chatStore.activeChannel?.id || msg.channel_id
  }

  const { editingId, editText, editOriginal, isSavingEdit, startEdit, cancelEdit, saveEdit } = useMessageEditor(channelOf, () => { pickerId.value = null })

  // Leaving the editor drops focus with it: hand it back to the message row
  // so keyboard users stay where they were.
  watch(editingId, (now, was) => {
    if (!was || now) return
    const active = document.activeElement
    if (active && active !== document.body && active.isConnected) return
    toValue(container)?.querySelector<HTMLElement>(`[data-msg-id="${was}"], [data-reply-id="${was}"]`)?.focus()
  }, { flush: 'post' })

  function isOwn(msg: Message) {
    return !!authStore.user?.id && msg.user_id === authStore.user.id
  }

  // Own messages always; others' with Manage messages in the channel, but
  // never the Owner's (the server refuses that too).
  function canDelete(msg: Message) {
    if (isOwn(msg)) return true
    if (chatStore.members.find(m => m.id === msg.user_id)?.role === 'admin') return authStore.isAdmin
    return permissions.canIn(channelOf(msg), 'manage_messages')
  }

  /** Resolves to whether the message is gone. */
  async function deleteMessage(msg: Message) {
    const ok = await confirmDelete(msg, deleteTitle)
    if (!ok) return false
    try {
      await chatStore.deleteMessage(channelOf(msg), msg.id)
      return true
    } catch (err) {
      toasts.error(caughtErrorMessage(err, t('chat.deleteFailed')))
      return false
    }
  }

  // Saving an empty text asks whether to delete the message, as Discord does.
  async function saveOrDelete(msg: Message) {
    if (editText.value.trim()) return saveEdit(msg)
    if (isSavingEdit.value || editingId.value !== msg.id) return
    isSavingEdit.value = true
    try {
      if (await deleteMessage(msg)) cancelEdit()
    } finally {
      isSavingEdit.value = false
    }
  }

  return {
    editingId, editText, editOriginal, isSavingEdit, startEdit, cancelEdit, saveEdit: saveOrDelete,
    deleteMessage, toggleReaction, isOwn, canDelete,
    pickerId, togglePicker, closePicker,
    isUploading, upload
  }
}
