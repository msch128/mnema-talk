// The comments of one event: the newest page, older pages, writing, editing,
// deleting and muting the notices, and the live changes of an open list.
import { ref, watch } from 'vue'
import type { Ref } from 'vue'
import { useToastStore } from '../stores/toast'
import { useEventCommentsStore } from '../stores/eventComments'
import type { CommentChange } from '../stores/eventComments'
import { caughtErrorMessage } from '../lib/api'
import * as api from '../lib/eventCommentsApi'
import type { EventComment } from '../lib/eventCommentsApi'
import { t } from '../i18n'

/** The comments after a live change: created ones are added once, edited ones replaced, deleted ones dropped. */
export function applyChange(list: EventComment[], change: CommentChange): EventComment[] {
  const c = change.comment
  if (change.action === 'deleted') return list.filter(x => x.id !== change.comment_id)
  if (!c) return list
  if (change.action === 'updated') return list.map(x => (x.id === c.id ? c : x))
  return list.some(x => x.id === c.id) ? list : [...list, c]
}

/** The pages of one event's comments: the newest, and older ones on request. */
function useCommentPages(eventId: Ref<string>) {
  const toast = useToastStore()
  const comments = ref<EventComment[]>([])
  const hasMore = ref(false)
  const muted = ref(false)
  const loading = ref(false)
  const failed = ref(false)
  const loadingMore = ref(false)
  // An answer for another event than the one shown now is dropped.
  let seq = 0

  async function load() {
    const mine = ++seq
    loading.value = true
    failed.value = false
    try {
      const page = await api.fetchComments(eventId.value)
      if (mine !== seq) return
      comments.value = page.comments
      hasMore.value = page.has_more
      muted.value = page.muted
    } catch (err) {
      if (mine !== seq) return
      console.warn('Failed to load the comments', err)
      failed.value = true
    } finally {
      if (mine === seq) loading.value = false
    }
  }

  async function loadOlder() {
    const first = comments.value[0]
    if (!first || loadingMore.value) return
    const mine = seq
    loadingMore.value = true
    try {
      const page = await api.fetchComments(eventId.value, first.id)
      if (mine !== seq) return
      const have = new Set(comments.value.map(c => c.id))
      comments.value = [...page.comments.filter(c => !have.has(c.id)), ...comments.value]
      hasMore.value = page.has_more
    } catch (err) {
      if (mine === seq) toast.error(caughtErrorMessage(err, t('events.commentsLoadFailed')))
    } finally {
      loadingMore.value = false
    }
  }

  return { comments, hasMore, muted, loading, failed, loadingMore, load, loadOlder }
}

export function useEventComments(eventId: Ref<string>) {
  const toast = useToastStore()
  const live = useEventCommentsStore()
  const pages = useCommentPages(eventId)
  const { comments, muted } = pages
  const busy = ref(false)

  /** Runs a request that changes something; a failure is shown calmly and answers false. */
  async function attempt(failure: string, work: () => Promise<void>): Promise<boolean> {
    if (busy.value) return false
    busy.value = true
    try {
      await work()
      return true
    } catch (err) {
      toast.error(caughtErrorMessage(err, failure))
      return false
    } finally {
      busy.value = false
    }
  }

  const send = (text: string) => attempt(t('events.commentFailed'), async () => {
    const c = await api.postComment(eventId.value, text)
    if (!comments.value.some(x => x.id === c.id)) comments.value = [...comments.value, c]
  })

  const edit = (id: string, text: string) => attempt(t('events.commentEditFailed'), async () => {
    const c = await api.editComment(eventId.value, id, text)
    comments.value = comments.value.map(x => (x.id === id ? c : x))
  })

  const remove = (id: string) => attempt(t('events.commentDeleteFailed'), async () => {
    await api.deleteComment(eventId.value, id)
    comments.value = comments.value.filter(c => c.id !== id)
  })

  const toggleMute = () => attempt(t('events.commentMuteFailed'), async () => {
    muted.value = await api.setCommentsMuted(eventId.value, !muted.value)
  })

  // Changes made by others (and by this member in another tab) while the list is open.
  watch(() => live.last, last => {
    if (last && last.change.event_id === eventId.value) comments.value = applyChange(comments.value, last.change)
  })
  watch(eventId, () => { comments.value = []; void pages.load() })

  return { ...pages, busy, send, edit, remove, toggleMute }
}
