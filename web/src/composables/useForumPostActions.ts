// What a member does with the open forum post: rename, retag, mark solved, pin,
// lock and delete. The server decides who may; this hides what would fail.
import { computed } from 'vue'
import type { Message } from '../types/domain'
import { useChatStore } from '../stores/chat'
import type { ThreadRoot } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { updateForumPost } from '../lib/forumApi'
import type { UpdateForumPostPayload } from '../lib/forumApi'
import { caughtErrorMessage } from '../lib/api'
import { confirm } from '../lib/confirm'
import { previewText } from '../lib/replies'
import { t } from '../i18n'

export function useForumPostActions() {
  const chatStore = useChatStore()
  const authStore = useAuthStore()
  const permissions = usePermissionsStore()
  const toasts = useToastStore()

  const post = computed(() => chatStore.activeThread)
  const channelId = computed(() => post.value?.channel_id ?? chatStore.activeChannel?.id ?? '')
  const canModerate = computed(() => permissions.canIn(channelId.value, 'manage_messages'))
  const isAuthor = computed(() => !!post.value && post.value.user_id === authStore.user?.id)
  // The author edits an own post unless it is locked; moderators always.
  const canEdit = computed(() => canModerate.value || (isAuthor.value && !post.value?.is_locked))
  const canDelete = computed(() => isAuthor.value || canModerate.value)

  /** Sends a change of the post that was open when it was asked for; resolves to whether it went through. */
  async function change(patch: UpdateForumPostPayload, failure: string): Promise<boolean> {
    const target = post.value
    if (!target) return false
    try {
      const updated = await updateForumPost(channelId.value, target.id, patch)
      if (chatStore.activeThread?.id === target.id) Object.assign(chatStore.activeThread, updated)
      return true
    } catch (err) {
      toasts.error(caughtErrorMessage(err, failure))
      return false
    }
  }

  const rename = (title: string) => change({ title }, t('forum.renameFailed'))
  const retag = (tagIds: string[]) => change({ tag_ids: tagIds }, t('forum.retagFailed'))
  const setSolved = (solved: boolean) => change({ is_resolved: solved }, t('forum.solvedFailed'))
  const setLocked = (locked: boolean) => change({ is_locked: locked }, t('forum.lockFailed'))

  async function setPinned(pinned: boolean) {
    const target = post.value
    if (!target) return
    try {
      await chatStore.setPinned(channelId.value, target.id, pinned)
    } catch (err) {
      toasts.error(caughtErrorMessage(err, t('forum.pinFailed')))
    }
  }

  async function remove() {
    const target: ThreadRoot | null = post.value
    if (!target) return
    const replies = target.reply_count ?? 0
    const ok = await confirm({
      title: t('forum.deletePostTitle'),
      body: replies > 0 ? t('forum.deletePostBodyReplies', { count: replies }) : t('forum.deletePostBody'),
      excerpt: target.title || previewText((target as Message).content).slice(0, 160),
      confirmLabel: t('common.delete'),
      danger: true
    })
    if (!ok) return
    try {
      await chatStore.deleteMessage(channelId.value, target.id)
    } catch (err) {
      toasts.error(caughtErrorMessage(err, t('chat.deleteFailed')))
    }
  }

  return { post, channelId, canModerate, isAuthor, canEdit, canDelete, rename, retag, setSolved, setLocked, setPinned, remove }
}
