import { useToastStore } from '../toast'
import { usePermissionsStore } from '../permissions'
import { useAppVersionStore } from '../appVersion'
import { emptyWindow } from '../../lib/messageWindow'
import type { Cx } from './state'
import { resetWindow, setWindow } from './messageWindow'
import { closeWebSocket } from './socket'
import { closeThread } from './threads'

/** Retire all instance data and pending work before selecting another origin. */
export function dismissEphemeral(cx: Cx, id: number) {
  const { cs } = cx
  cs.ephemeral.value = cs.ephemeral.value.filter(n => n.id !== id)
}

export function resetCommunityState(cx: Cx) {
  const { cs } = cx
  cs.ephemeral.value = []
  cs.pinNotices.value = []
  cs.pinsSignal.value = null
  ++cs.communityGeneration
  ++cs.channelsFetchSeq
  cs.latestChannelsFetch = Promise.resolve()
  ++cs.membersFetchSeq
  ++cs.memberRevision
  ++cs.reconnectCheckSeq
  closeWebSocket(cx)
  closeThread(cx)
  cs.followedThreads.value = new Set()
  cs.followKnown.value = false
  cs.pendingThreadOpen = null
  cs.pendingLive = []
  resetWindow(cx)
  setWindow(cx, emptyWindow())
  ++cs.jumpSeq
  cs.jumpTarget.value = null
  cs.categories.value = []
  cs.uncategorized.value = []
  cs.activeChannel.value = null
  cs.members.value = []
  usePermissionsStore().reset()
  cs.presenceById.value = {}
  cs.activityById.value = {}
  cs.disabledMembers.clear()
  cs.suppressedPresence.clear()
  cs.memberProfileUpdates.clear()
  cs.countedReplies.clear()
  cs.readStates.value = {}
  cs.notifySettings.value = { channels: [], categories: [] }
  clearTimeout(cs.notifyExpiryTimer)
  cs.activeChannelLastReadAt.value = null
  cs.voiceChatReading.value = false
  cs.suppressAutoReadFor = null
  cs.selectedUserProfile.value = null
  cs.pendingMention.value = ''
  cs.typingByChannel.value = {}
  for (const timer of cs.typingTimers.values()) clearTimeout(timer)
  cs.typingTimers.clear()
  cs.lastTypingSentAt.clear()
  cs.markReadThrottled.reset()
  clearTimeout(cs.memberStatsTimer)
  clearTimeout(cs.idleTimer)
  cs.isIdle = false
  cs.lastActivityAt = 0
  cs.reconnectDelay = 1000
  cs.reconnectCount.value = 0
  cs.nextRetryAt.value = 0
  cs.webrtcOfferHandler = null
  cs.webrtcCandidateHandler = null
  cs.voiceKickedHandler = null
  const version = useAppVersionStore()
  version.followAdminUpdates(() => false)
  version.setServerVersion('')
  const toasts = useToastStore()
  for (const toast of [...toasts.toasts]) toasts.dismiss(toast.id)
}

