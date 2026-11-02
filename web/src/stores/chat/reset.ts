import { useToastStore } from '../toast'
import { usePermissionsStore } from '../permissions'
import { useRaidStore } from '../raid'
import { useAppVersionStore } from '../appVersion'
import { useBlocksStore } from '../blocks'
import { useLaterStore } from '../later'
import { useSavedStore } from '../saved'
import { useForwardQuotesStore } from '../forwardQuotes'
import { useEventsStore } from '../events'
import { useOnboardingStore } from '../onboarding'
import { useFirstRunStore } from '../firstRun'
import { useAccountStore } from '../account'
import { useDmCallsStore } from '../dmCalls'
import { clearAllTrays } from '../../composables/attachmentTrayState'
import { emptyWindow } from '../../lib/messageWindow'
import { emptyNotificationSettings, type Cx } from './state'
import { resetWindow, setWindow } from './messageWindow'
import { closeWebSocket } from './socket'
import { closeThread } from './threads'

/** Drops what the stores keep of the signed-in member: saved list, quotes, events, onboarding record, first-run checklist, account dialog. */
export function resetMemberStores() {
  useSavedStore().reset()
  useForwardQuotesStore().reset()
  useEventsStore().reset()
  useOnboardingStore().reset()
  useFirstRunStore().reset()
  useAccountStore().reset()
  useDmCallsStore().reset()
  clearAllTrays()
}

/** Removes one ephemeral note. */
export function dismissEphemeral(cx: Cx, id: number) {
  const { cs } = cx
  cs.ephemeral.value = cs.ephemeral.value.filter(n => n.id !== id)
}

/** Retire all instance data and pending work before selecting another origin. */
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
  cs.dms.value = []
  cs.dmsLoaded.value = false
  cs.dmSettings.value = { mute_all: false, list_collapsed: false }
  cs.dmSettingsLoaded.value = false
  ++cs.dmsFetchSeq
  useBlocksStore().reset()
  useLaterStore().reset()
  resetMemberStores()
  usePermissionsStore().reset()
  useRaidStore().reset()
  cs.presenceById.value = {}
  cs.activityById.value = {}
  cs.disabledMembers.clear()
  cs.suppressedPresence.clear()
  cs.memberProfileUpdates.clear()
  cs.countedReplies.clear()
  cs.readStates.value = {}
  cs.notifySettings.value = emptyNotificationSettings()
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
  cs.lastVoiceActivityAt = 0
  cs.reconnectDelay = 1000
  cs.reconnectCount.value = 0
  cs.nextRetryAt.value = 0
  cs.webrtcOfferHandler = null
  cs.webrtcCandidateHandler = null
  cs.voiceKickedHandler = null
  cs.talkMovedHandler = null
  cs.mediaHandshakeHandler = null
  cs.forumTagsChangedHandler = null
  cs.forumTagsSignal.value = null
  cs.forumPostsSignal.value = null
  const version = useAppVersionStore()
  version.followAdminUpdates(() => false)
  version.setServerVersion('')
  const toasts = useToastStore()
  for (const toast of [...toasts.toasts]) toasts.dismiss(toast.id)
}

