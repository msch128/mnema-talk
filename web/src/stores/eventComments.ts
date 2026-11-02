// What reaches the client about event comments: the live changes an open event
// reads (a signal with the last one) and the notice of a new comment.
import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { ServerEventPayloads } from '../types/events'
import { useToastStore } from './toast'
import { openEvents } from '../lib/eventsDialog'
import { t } from '../i18n'

export type CommentChange = ServerEventPayloads['event_comment']

export const useEventCommentsStore = defineStore('eventComments', () => {
  const last = ref<{ seq: number; change: CommentChange } | null>(null)

  return {
    last,
    /** A comment was created, edited or deleted: open comment lists of that event follow. */
    handleChange: (change: CommentChange) => {
      last.value = { seq: (last.value?.seq ?? 0) + 1, change }
    },
    /** A new comment on an event the member created or attends (and did not mute). */
    handleNotice: (p: ServerEventPayloads['event_comment_notice']) => {
      useToastStore().info(
        t('events.commentToast', { title: p.title, author: p.author_name, excerpt: p.excerpt }),
        { action: { label: t('events.commentOpen'), onClick: () => openEvents(p.event_id) } }
      )
    },
    reset: () => { last.value = null }
  }
})
