import { defineStore } from 'pinia'
import { useAuthStore } from './auth'
import { useVoiceStore } from './voice'
import type { ClientEventPayloads } from '../types/events'
import { createChatState, type ChatState, type Cx } from './chat/state'
import { createCategory, createChannel, deleteCategory, deleteChannel, duplicateChannel, fetchChannels, isVoiceChannel, linkTo, selectChannel, setChannelAutoDelete, updateCategory, updateChannel } from './chat/channels'
import { activityOfUser, closeUserProfile, fetchMembers, insertMention, openUserProfile, patchOpenProfile, presenceOf } from './chat/members'
import { installActivityListeners, setMyPresence, setStatusText } from './chat/presence'
import { fetchMessages, goToMessage, jumpToLatest, jumpToMessage, loadNewer, loadOlder, showToast } from './chat/messageWindow'
import { createPoll, endPoll, fetchPoll, votePoll } from './chat/polls'
import { deleteMessage, editMessage, sendMessage, setPinned, toggleReaction, uploadMedia } from './chat/messageActions'
import { fetchFollowedThreads, setThreadFollow } from './chat/threadFollow'
import { closeThread, openThread, sendThreadReply, uploadThreadMedia } from './chat/threads'
import { closeWebSocket, initWebSocket, installVisibilityRetry, retryNow, sendWSEvent, setWebRTCHandlers } from './chat/socket'
import { handleWSEvent } from './chat/events'
import { fetchReadState, markActiveChannelReadIfReading, markChannelRead, markChannelUnread, setReturnToTabHandler, setVoiceChatReading } from './chat/readState'
import { fetchNotificationSettings, markAllRead, notificationLevel, setCategoryNotificationLevel, setNotificationLevel, setServerNotification } from './chat/notifySettings'
import { sendTyping } from './chat/typing'
import { messageMentionsMe, refreshNotificationPermission, requestNotificationPermission } from './chat/notifications'
import { dismissEphemeral, resetCommunityState } from './chat/reset'
import { dismissPinNotice, fetchPins } from './chat/pins'
import { dmActions } from './chat/dmActions'

export type { EphemeralNote, ProfilePreview, ThreadRoot } from './chat/types'
export { MARK_READ_INTERVAL_MS, TYPING_EXPIRY_MS } from './chat/types'

/** What the store shows of the chat state, as the refs the modules update. */
function exposedState(cs: ChatState) {
  return {
    reconnectCount: cs.reconnectCount,
    wasConnected: cs.wasConnected,
    reconnectAttempt: cs.reconnectAttempt,
    nextRetryAt: cs.nextRetryAt,
    categories: cs.categories,
    allChannels: cs.allChannels,
    ephemeral: cs.ephemeral,
    pinsSignal: cs.pinsSignal,
    forumTagsSignal: cs.forumTagsSignal,
    pinNotices: cs.pinNotices,
    uncategorized: cs.uncategorized,
    activeChannel: cs.activeChannel,
    messages: cs.messages,
    hasMoreBefore: cs.hasMoreBefore,
    hasMoreAfter: cs.hasMoreAfter,
    isLoadingBefore: cs.isLoadingBefore,
    isLoadingAfter: cs.isLoadingAfter,
    isLoadingWindow: cs.isLoadingWindow,
    missedLiveCount: cs.missedLiveCount,
    liveAppendSeq: cs.liveAppendSeq,
    latestLoadSeq: cs.latestLoadSeq,
    jumpTarget: cs.jumpTarget,
    isConnected: cs.isConnected,
    members: cs.members,
    dms: cs.dms,
    dmsLoaded: cs.dmsLoaded,
    dmSettings: cs.dmSettings,
    dmSettingsLoaded: cs.dmSettingsLoaded,
    presenceById: cs.presenceById,
    activityById: cs.activityById,
    onlineUserIds: cs.onlineUserIds,
    onlineMembers: cs.onlineMembers,
    offlineMembers: cs.offlineMembers,
    showMemberList: cs.showMemberList,
    selectedUserProfile: cs.selectedUserProfile,
    pendingMention: cs.pendingMention,
    readStates: cs.readStates,
    notifySettings: cs.notifySettings,
    activeChannelLastReadAt: cs.activeChannelLastReadAt,
    voiceChatReading: cs.voiceChatReading,
    notificationPermission: cs.notificationPermission,
    typingByChannel: cs.typingByChannel,
    activeThread: cs.activeThread,
    followedThreads: cs.followedThreads,
    followKnown: cs.followKnown,
    threadReplies: cs.threadReplies,
    isThreadLoading: cs.isThreadLoading
  }
}

/** The module functions as store actions; each takes the context first, the store exposes them without it. */
function exposedActions(cx: Cx) {
  const bind = <A extends unknown[], R>(fn: (cx: Cx, ...args: A) => R) => (...args: A): R => fn(cx, ...args)
  return {
    ...dmActions(cx),
    resetCommunityState: bind(resetCommunityState),
    retryNow: bind(retryNow),
    dismissEphemeral: bind(dismissEphemeral),
    dismissPinNotice: bind(dismissPinNotice),
    fetchPins: bind(fetchPins),
    isVoiceChannel: bind(isVoiceChannel),
    linkTo: bind(linkTo),
    showToast,
    loadOlder: bind(loadOlder),
    loadNewer: bind(loadNewer),
    jumpToLatest: bind(jumpToLatest),
    jumpToMessage: bind(jumpToMessage),
    presenceOf: bind(presenceOf),
    activityOfUser: bind(activityOfUser),
    setMyPresence: bind(setMyPresence),
    setStatusText: bind(setStatusText),
    messageMentionsMe: bind(messageMentionsMe),
    openUserProfile: bind(openUserProfile),
    closeUserProfile: bind(closeUserProfile),
    patchOpenProfile: bind(patchOpenProfile),
    insertMention: bind(insertMention),
    fetchChannels: bind(fetchChannels),
    fetchMembers: bind(fetchMembers),
    selectChannel: bind(selectChannel),
    fetchMessages: bind(fetchMessages),
    sendMessage: bind(sendMessage),
    editMessage: bind(editMessage),
    deleteMessage: bind(deleteMessage),
    setPinned: bind(setPinned),
    toggleReaction: bind(toggleReaction),
    createPoll: bind(createPoll),
    fetchPoll: bind(fetchPoll),
    votePoll: bind(votePoll),
    endPoll: bind(endPoll),
    uploadMedia: bind(uploadMedia),
    createChannel: bind(createChannel),
    duplicateChannel: bind(duplicateChannel),
    deleteChannel: bind(deleteChannel),
    updateChannel: bind(updateChannel),
    setChannelAutoDelete: bind(setChannelAutoDelete),
    createCategory: bind(createCategory),
    deleteCategory: bind(deleteCategory),
    updateCategory: bind(updateCategory),
    setVoiceChatReading: bind(setVoiceChatReading),
    fetchReadState: bind(fetchReadState),
    markChannelRead: bind(markChannelRead),
    markChannelUnread: bind(markChannelUnread),
    fetchNotificationSettings: bind(fetchNotificationSettings),
    setNotificationLevel: bind(setNotificationLevel),
    setCategoryNotificationLevel: bind(setCategoryNotificationLevel),
    setServerNotification: bind(setServerNotification),
    markAllRead: bind(markAllRead),
    notificationLevel: bind(notificationLevel),
    goToMessage: bind(goToMessage),
    sendTyping: bind(sendTyping),
    refreshNotificationPermission: bind(refreshNotificationPermission),
    requestNotificationPermission: bind(requestNotificationPermission),
    openThread: bind(openThread),
    setThreadFollow: bind(setThreadFollow),
    fetchFollowedThreads: bind(fetchFollowedThreads),
    closeThread: bind(closeThread),
    sendThreadReply: bind(sendThreadReply),
    uploadThreadMedia: bind(uploadThreadMedia),
    initWebSocket: bind(initWebSocket),
    closeWebSocket: bind(closeWebSocket),
    sendWSEvent: <K extends keyof ClientEventPayloads>(type: K, payload: ClientEventPayloads[K]) => sendWSEvent(cx, type, payload),
    setWebRTCHandlers: bind(setWebRTCHandlers),
    handleWSEvent: bind(handleWSEvent)
  }
}

export const useChatStore = defineStore('chat', () => {
  const authStore = useAuthStore()
  const voiceStore = useVoiceStore()
  const cx: Cx = { cs: createChatState(channelId => markChannelRead(cx, channelId)), authStore, voiceStore }
  const cs = cx.cs

  installVisibilityRetry(cx)
  installActivityListeners(cx)
  setReturnToTabHandler(() => markActiveChannelReadIfReading(cx))

  return { ...exposedState(cs), ...exposedActions(cx) }
})

export type ChatStore = ReturnType<typeof useChatStore>
