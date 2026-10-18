import { defineStore } from 'pinia'
import { useAuthStore } from './auth'
import { useVoiceStore } from './voice'
import type { ClientEventPayloads } from '../types/events'
import { createChatState, type Cx } from './chat/state'
import { createCategory, createChannel, deleteCategory, deleteChannel, duplicateChannel, fetchChannels, isVoiceChannel, selectChannel, updateCategory, updateChannel } from './chat/channels'
import { activityOfUser, closeUserProfile, fetchMembers, insertMention, openUserProfile, presenceOf } from './chat/members'
import { installActivityListeners, setMyPresence, setStatusText } from './chat/presence'
import { fetchMessages, goToMessage, jumpToLatest, jumpToMessage, loadNewer, loadOlder, showToast } from './chat/messageWindow'
import { deleteMessage, editMessage, sendMessage, setPinned, toggleReaction, uploadMedia } from './chat/messageActions'
import { closeThread, openThread, sendThreadReply, uploadThreadMedia } from './chat/threads'
import { closeWebSocket, initWebSocket, installVisibilityRetry, retryNow, sendWSEvent, setWebRTCHandlers } from './chat/socket'
import { handleWSEvent } from './chat/events'
import { fetchReadState, markActiveChannelReadIfReading, markChannelRead, markChannelUnread, notificationLevel, setNotificationLevel, setReturnToTabHandler, setVoiceChatReading } from './chat/readState'
import { sendTyping } from './chat/typing'
import { messageMentionsMe, requestNotificationPermission } from './chat/notifications'
import { dismissEphemeral, resetCommunityState } from './chat/reset'

export type { EphemeralNote, ProfilePreview, ThreadRoot } from './chat/types'
export { MARK_READ_INTERVAL_MS, TYPING_EXPIRY_MS } from './chat/types'

export const useChatStore = defineStore('chat', () => {
  const authStore = useAuthStore()
  const voiceStore = useVoiceStore()
  const cx: Cx = { cs: createChatState(channelId => markChannelRead(cx, channelId)), authStore, voiceStore }
  const cs = cx.cs
  // Each module function takes the context first; the store exposes them without it.
  const bind = <A extends unknown[], R>(fn: (cx: Cx, ...args: A) => R) => (...args: A): R => fn(cx, ...args)

  installVisibilityRetry(cx)
  installActivityListeners(cx)
  setReturnToTabHandler(() => markActiveChannelReadIfReading(cx))

  return {
    resetCommunityState: bind(resetCommunityState),
    reconnectCount: cs.reconnectCount,
    wasConnected: cs.wasConnected,
    reconnectAttempt: cs.reconnectAttempt,
    nextRetryAt: cs.nextRetryAt,
    retryNow: bind(retryNow),
    categories: cs.categories,
    allChannels: cs.allChannels,
    ephemeral: cs.ephemeral,
    dismissEphemeral: bind(dismissEphemeral),
    isVoiceChannel: bind(isVoiceChannel),
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
    showToast,
    loadOlder: bind(loadOlder),
    loadNewer: bind(loadNewer),
    jumpToLatest: bind(jumpToLatest),
    jumpToMessage: bind(jumpToMessage),
    isConnected: cs.isConnected,
    members: cs.members,
    presenceById: cs.presenceById,
    presenceOf: bind(presenceOf),
    activityById: cs.activityById,
    activityOfUser: bind(activityOfUser),
    onlineUserIds: cs.onlineUserIds,
    setMyPresence: bind(setMyPresence),
    setStatusText: bind(setStatusText),
    messageMentionsMe: bind(messageMentionsMe),
    onlineMembers: cs.onlineMembers,
    offlineMembers: cs.offlineMembers,
    showMemberList: cs.showMemberList,
    selectedUserProfile: cs.selectedUserProfile,
    openUserProfile: bind(openUserProfile),
    closeUserProfile: bind(closeUserProfile),
    pendingMention: cs.pendingMention,
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
    uploadMedia: bind(uploadMedia),
    createChannel: bind(createChannel),
    duplicateChannel: bind(duplicateChannel),
    deleteChannel: bind(deleteChannel),
    updateChannel: bind(updateChannel),
    createCategory: bind(createCategory),
    deleteCategory: bind(deleteCategory),
    updateCategory: bind(updateCategory),
    readStates: cs.readStates,
    activeChannelLastReadAt: cs.activeChannelLastReadAt,
    voiceChatReading: cs.voiceChatReading,
    setVoiceChatReading: bind(setVoiceChatReading),
    fetchReadState: bind(fetchReadState),
    markChannelRead: bind(markChannelRead),
    markChannelUnread: bind(markChannelUnread),
    setNotificationLevel: bind(setNotificationLevel),
    notificationLevel: bind(notificationLevel),
    notificationPermission: cs.notificationPermission,
    goToMessage: bind(goToMessage),
    typingByChannel: cs.typingByChannel,
    sendTyping: bind(sendTyping),
    requestNotificationPermission: bind(requestNotificationPermission),
    activeThread: cs.activeThread,
    threadReplies: cs.threadReplies,
    isThreadLoading: cs.isThreadLoading,
    openThread: bind(openThread),
    closeThread: bind(closeThread),
    sendThreadReply: bind(sendThreadReply),
    uploadThreadMedia: bind(uploadThreadMedia),
    initWebSocket: bind(initWebSocket),
    closeWebSocket: bind(closeWebSocket),
    sendWSEvent: <K extends keyof ClientEventPayloads>(type: K, payload: ClientEventPayloads[K]) => sendWSEvent(cx, type, payload),
    setWebRTCHandlers: bind(setWebRTCHandlers),
    handleWSEvent: bind(handleWSEvent)
  }
})

export type ChatStore = ReturnType<typeof useChatStore>
