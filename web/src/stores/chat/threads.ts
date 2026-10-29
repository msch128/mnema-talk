import type { Message } from '../../types/domain'
import { decodeThread } from '../../types/domain'
import { api } from '../../lib/api'
import type { ThreadRoot } from './types'
import type { Cx } from './state'
import { sendMessage, uploadMedia } from './messageActions'
import { noteThreadFollowing } from './threadFollow'
import { useToastStore } from '../toast'
import { t } from '../../i18n'

function resolveThreadTarget(msg: Message | ThreadRoot | string): { id: string; initial: ThreadRoot } | null {
  if (!msg) return null
  if (typeof msg === 'string') {
    return msg ? { id: msg, initial: { id: msg } } : null
  }
  return msg.id ? { id: msg.id, initial: msg } : null
}

function isThreadActive(cs: Cx['cs'], id: string, gen: number): boolean {
  return gen === cs.threadGen && cs.activeThread.value?.id === id
}

/** The loaded replies plus those that arrived live while the request ran, oldest first. */
function mergeLiveReplies(loaded: Message[], live: Message[]): Message[] {
  const known = new Set(loaded.map(m => m.id))
  const late = live.filter(m => !known.has(m.id))
  if (!late.length) return loaded
  return [...loaded, ...late].sort((a, b) => Date.parse(a.created_at) - Date.parse(b.created_at))
}

export async function openThread(cx: Cx, msg: Message | ThreadRoot | string) {
  const target = resolveThreadTarget(msg)
  if (!target) return
  const { cs } = cx
  const { id, initial } = target
  const gen = ++cs.threadGen
  cs.activeThread.value = initial
  cs.threadReplies.value = []
  cs.isThreadLoading.value = true
  try {
    const data = await api(`/api/messages/${id}/thread`, { decode: decodeThread })
    if (!isThreadActive(cs, id, gen)) return
    cs.activeThread.value = data.root
    cs.threadReplies.value = mergeLiveReplies(data.replies, cs.threadReplies.value)
    noteThreadFollowing(cx, id, data.following)
  } catch (e) {
    if (gen === cs.threadGen) {
      console.error('Failed to load thread:', e)
      useToastStore().error(t('thread.loadFailed'))
    }
  } finally {
    if (gen === cs.threadGen) cs.isThreadLoading.value = false
  }
}

export function closeThread(cx: Cx) {
  const { cs } = cx
  cs.threadGen++
  cs.activeThread.value = null
  cs.threadReplies.value = []
  cs.isThreadLoading.value = false
}

export async function sendThreadReply(cx: Cx, content: string, replyToId: string | null = null) {
  const { cs } = cx
  if (!cs.activeThread.value) return null
  return sendMessage(cx, content, cs.activeThread.value.id, replyToId)
}

export async function uploadThreadMedia(cx: Cx, file: File, content = '', replyToId: string | null = null) {
  const { cs } = cx
  if (!cs.activeThread.value) return null
  return uploadMedia(cx, file, content, cs.activeThread.value.id, replyToId)
}

