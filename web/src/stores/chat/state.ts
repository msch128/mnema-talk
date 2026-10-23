import { computed, ref, shallowRef } from 'vue'
import type { Category, Channel, Member, Message, NotificationSettings, Presence, ReadState, User } from '../../types/domain'
import type { ServerEventPayloads } from '../../types/events'
import { platform, type PlatformSocket } from '../../lib/platform'
import { createKeyedThrottle } from '../../lib/chatLogic'
import type { useAuthStore } from '../auth'
import type { useVoiceStore } from '../voice'
import { MARK_READ_INTERVAL_MS, type EphemeralNote, type PinNotice, type ProfilePreview, type ThreadRoot, type Timer, type TypingUser } from './types'

type Handler<K extends keyof ServerEventPayloads> = ((payload: ServerEventPayloads[K]) => void | Promise<void>) | null

/** The channels, the open channel with its windowed history and thread, and the socket. */
function channelState() {
  return {
    communityGeneration: 0,
    categories: ref<Category[]>([]),
    uncategorized: ref<Channel[]>([]),
    activeChannel: ref<Channel | null>(null),
    // Windowed history of the active channel (see lib/messageWindow).
    messages: ref<Message[]>([]),
    hasMoreBefore: ref(false),
    hasMoreAfter: ref(false),
    isLoadingBefore: ref(false),
    isLoadingAfter: ref(false),
    isLoadingWindow: ref(false),
    // Live messages that could not join the window (it doesn't reach the newest end).
    missedLiveCount: ref(0),
    // Change signals for the view: what caused the last window update.
    liveAppendSeq: ref(0),
    latestLoadSeq: ref(0),
    jumpTarget: ref<{ id: string; seq: number } | null>(null),
    activeThread: ref<ThreadRoot | null>(null),
    // Root ids of the threads I follow, and whether the server told me (older servers do not).
    followedThreads: ref<ReadonlySet<string>>(new Set()),
    followKnown: ref(false),
    // Pinned messages: bumped when the pins of a channel changed, so an open pinned list refetches; plus the notes about new pins.
    pinsSignal: ref<{ channelId: string; seq: number } | null>(null),
    pinNotices: ref<PinNotice[]>([]),
    pinNoticeSeq: 0,
    threadReplies: ref<Message[]>([]),
    isThreadLoading: ref(false),
    ws: shallowRef<PlatformSocket | null>(null),
    isConnected: ref(false)
  }
}

/** Members, presence, read states, typing and notification permission. */
function memberState() {
  return {
    // Community members & real-time presence
    members: ref<Member[]>([]),
    // Replies to slash commands that only this member sees; kept in memory, never stored.
    ephemeral: ref<EphemeralNote[]>([]),
    ephemeralSeq: 0,
    membersFetchSeq: 0,
    memberRevision: 0,
    // A disable event wins over requests already in flight. A later active-only
    // roster may restore someone re-enabled while this client was disconnected.
    disabledMembers: new Map<string, number>(),
    suppressedPresence: new Map<string, Presence>(),
    memberProfileUpdates: new Map<string, { revision: number; user: User }>(),
    // Live status of every connected user: user_id → online | away | dnd | focus.
    presenceById: ref<Record<string, Presence>>({}),
    // What connected bots say they are doing: user_id → activity.
    activityById: ref<Record<string, { type: number; name: string }>>({}),
    showMemberList: ref(true),
    selectedUserProfile: ref<ProfilePreview | null>(null),
    pendingMention: ref(''),

    // Read states per channel: channel_id -> { channel_id, unread_count, mention_count, last_read_at, notify_level }
    readStates: ref<Record<string, ReadState>>({}),
    // The caller's stored notification settings (the read states carry the effective level).
    notifySettings: ref<NotificationSettings>({ channels: [], categories: [] }),
    // Re-reads read states and settings when the next timed mute runs out.
    notifyExpiryTimer: undefined as Timer | undefined,
    activeChannelLastReadAt: ref<string | null>(null),
    // A voice channel's chat is only read while its panel next to the Talk is
    // open (VoiceChatPanel sets this); with the panel closed it collects unreads.
    voiceChatReading: ref(false),

    // Typing state per channel: channel_id -> Array<{ user_id, username, display_name }>
    typingByChannel: ref<Record<string, TypingUser[]>>({}),
    typingTimers: new Map<string, Timer>(),
    // channel_id -> when my last typing notice for it went out.
    lastTypingSentAt: new Map<string, number>(),

    // Desktop notification permission: 'default' | 'granted' | 'denied' | 'unsupported'
    notificationPermission: ref<string>(platform.notificationPermission()),
    // Thread to open once the jump to its root message has landed (search / notification targets).
    pendingThreadOpen: null as { rootId: string; channelId: string } | null
  }
}

/** Socket generations, timers and the handlers the voice layer installs. */
function socketState() {
  return {
    socketGeneration: 0,
    reconnectCheckSeq: 0,
    pingTimer: undefined as Timer | undefined,
    reconnectTimer: undefined as Timer | undefined,
    reconnectDelay: 1000,
    webrtcOfferHandler: null as Handler<'webrtc_offer'>,
    webrtcCandidateHandler: null as Handler<'webrtc_candidate'>,
    voiceKickedHandler: null as Handler<'voice_kicked'>,
    mediaHandshakeHandler: null as Handler<'media_handshake'>
  }
}

/** Fetch ordering, window generations, reconnect bookkeeping and idle tracking. */
function syncState(markRead: (channelId: string) => void) {
  return {
    // Overlapping fetches (a channels_changed broadcast while a refetch runs)
    // may answer out of order; only the latest one counts. A superseded fetch
    // resolves once the latest one is applied, so whoever awaits it (e.g. the
    // sidebar before it drops its pending order) never sees the older list.
    channelsFetchSeq: 0,
    latestChannelsFetch: Promise.resolve() as Promise<void>,
    // A deletion may take thread replies of several people with it: reload the
    // members' totals once things calm down.
    memberStatsTimer: undefined as Timer | undefined,

    // Bumped whenever the window is replaced; responses from an older
    // generation (other channel, earlier jump) are ignored.
    windowGen: 0,
    pendingLive: [] as Message[],
    jumpSeq: 0,
    countedReplies: new Set<string>(),
    threadGen: 0,

    // Bumped on every successful reconnect; voice and other views watch it to
    // restore their server-side state.
    reconnectCount: ref(0),
    hadConnection: false,
    // For the connection banner: true once the socket has been up at least once
    // in this session, how many reconnects were tried, and when the next one runs.
    wasConnected: ref(false),
    reconnectAttempt: ref(0),
    nextRetryAt: ref(0),

    // After IDLE_AFTER_MS without input the server shows "online" as "away".
    isIdle: false,
    idleTimer: undefined as Timer | undefined,
    lastActivityAt: 0,
    suppressAutoReadFor: null as string | null,
    markReadThrottled: createKeyedThrottle(markRead, MARK_READ_INTERVAL_MS)
  }
}

/** Everything the chat store keeps; the modules in this folder read and write it through Cx. */
export function createChatState(markRead: (channelId: string) => void) {
  const base = {
    ...channelState(),
    ...memberState(),
    ...socketState(),
    ...syncState(markRead)
  }
  const allChannels = computed(() => [
    ...base.categories.value.flatMap(c => c.channels),
    ...base.uncategorized.value
  ])
  const onlineUserIds = computed(() => new Set(Object.keys(base.presenceById.value)))
  return {
    ...base,
    allChannels,
    onlineUserIds,
    onlineMembers: computed(() => base.members.value.filter(m => onlineUserIds.value.has(m.id))),
    offlineMembers: computed(() => base.members.value.filter(m => !onlineUserIds.value.has(m.id)))
  }
}

export type ChatState = ReturnType<typeof createChatState>

/** The context every function of the chat modules receives. */
export interface Cx {
  cs: ChatState
  authStore: ReturnType<typeof useAuthStore>
  voiceStore: ReturnType<typeof useVoiceStore>
}

export function assertCommunity(cx: Cx, generation: number) {
  if (generation !== cx.cs.communityGeneration) throw new Error('Community request retired')
}
