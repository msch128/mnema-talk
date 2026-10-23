import type { ServerEvent, ServerEventPayloads, ServerEventType } from '../../types/events'
import { useToastStore } from '../toast'
import { useEmojiStore } from '../emoji'
import { useSavedStore } from '../saved'
import { usePermissionsStore } from '../permissions'
import { useAppVersionStore } from '../appVersion'
import { noticeText, kickedText, KICKED_NOTICE_MS } from '../../lib/moderation'
import { snapshotToMap, applyPresenceUpdate } from '../../lib/presence'
import { MAX_EPHEMERAL } from './types'
import type { Cx } from './state'
import { fetchChannels } from './channels'
import { applyActivity, countMessageOf, fetchMembers, refreshMemberStats, updateUserEverywhere } from './members'
import { applyToMessage, bumpReplyCount, countReply, insertMessage, onOriginalDeleted, onOriginalEdited, removeFromWindow } from './messageWindow'
import { onThreadFollowUpdated } from './threadFollow'
import { messageMentionsMe, messageMentionsMeDirectly, triggerBrowserNotification } from './notifications'
import { fetchReadState, isReadingActiveChannel } from './readState'
import { notificationLevel, refreshNotifications } from './notifySettings'
import { applyPoll } from './polls'
import { onPinnedMaybeDeleted, onPinsChanged } from './pins'
import { closeThread } from './threads'
import { clearTypingForUser, handleTypingEvent } from './typing'

type Payload<K extends ServerEventType> = ServerEventPayloads[K]
type Handlers = { [K in ServerEventType]: (cx: Cx, p: Payload<K>) => void }

function onPresenceSnapshot({ cs }: Cx, p: Payload<'presence_snapshot'>) {
  const snapshot = snapshotToMap(p)
  for (const id of cs.disabledMembers.keys()) {
    const presence = snapshot[id]
    if (presence) cs.suppressedPresence.set(id, presence)
    else cs.suppressedPresence.delete(id)
    delete snapshot[id]
  }
  cs.presenceById.value = snapshot
  cs.activityById.value = {}
}

function onPresenceUpdate(cx: Cx, p: Payload<'presence_update'>) {
  const { cs } = cx
  if (p) applyActivity(cx, p)
  if (p && cs.disabledMembers.has(p.user_id)) {
    if (p.status === 'offline') cs.suppressedPresence.delete(p.user_id)
    else cs.suppressedPresence.set(p.user_id, p.status)
  } else {
    cs.presenceById.value = applyPresenceUpdate(cs.presenceById.value, p)
  }
}

function onReadState(cx: Cx, p: Payload<'read_state'>) {
  const { cs } = cx
  if (!p) return
  // Settings changed or everything was marked read (in this or another session).
  if (!('channel_id' in p) || !p.channel_id) {
    if ('refresh' in p && p.refresh) void refreshNotifications(cx)
    return
  }
  if ('refresh' in p && p.refresh) {
    fetchReadState(cx)
    return
  }
  const prev = cs.readStates.value[p.channel_id] || { channel_id: p.channel_id, notify_level: 'all' as const, unread_count: 0, mention_count: 0, last_read_at: null }
  cs.readStates.value = {
    ...cs.readStates.value,
    [p.channel_id]: {
      ...prev,
      ...p
    }
  }
}

// A message in a channel that is not being read right now counts as unread (and as a mention if it is one).
// Thread replies never add to the unread count (the server counts top-level messages only), but a mention in one still counts.
function countUnread({ cs }: Cx, channelId: string, isMention: boolean, isThreadReply: boolean) {
  const state = cs.readStates.value[channelId] || {
    channel_id: channelId,
    unread_count: 0,
    mention_count: 0,
    notify_level: 'all' as const,
    last_read_at: null
  }
  cs.readStates.value = {
    ...cs.readStates.value,
    [channelId]: {
      ...state,
      unread_count: (state.unread_count || 0) + (isThreadReply ? 0 : 1),
      mention_count: isMention ? (state.mention_count || 0) + 1 : (state.mention_count || 0)
    }
  }
}

interface IncomingReadStateOptions {
  channelId: string
  isOwn: boolean
  isCurrentChannel: boolean
  isMention: boolean
  isThreadReply: boolean
}

function handleIncomingMessageReadState(cx: Cx, opts: IncomingReadStateOptions) {
  if (opts.isOwn || !opts.channelId) return
  if (opts.isCurrentChannel && isReadingActiveChannel(cx)) {
    cx.cs.markReadThrottled.call(opts.channelId)
    return
  }
  if (opts.isThreadReply && !opts.isMention) return
  countUnread(cx, opts.channelId, opts.isMention, opts.isThreadReply)
}

function onMessageCreate(cx: Cx, p: Payload<'message_create'>) {
  if (!p) return
  const { cs, authStore } = cx
  countReply(cx, p)
  insertMessage(cx, p)
  countMessageOf(cx, p.user_id)
  clearTypingForUser(cx, p.channel_id, p.user_id)

  const isOwn = p.user_id === authStore.user?.id
  const isCurrentChannel = p.channel_id === cs.activeChannel.value?.id
  // A muted channel only counts what is addressed to me, the way the server counts it.
  const muted = notificationLevel(cx, p.channel_id) === 'mute'
  const isMention = muted ? messageMentionsMeDirectly(cx, p) : messageMentionsMe(cx, p)

  handleIncomingMessageReadState(cx, { channelId: p.channel_id, isOwn, isCurrentChannel, isMention, isThreadReply: !!p.parent_id })
  if (!isOwn && p.channel_id) {
    triggerBrowserNotification(cx, p, isMention)
  }
}

function onMemberTimeout({ cs }: Cx, p: Payload<'member_timeout'>) {
  const idx = cs.members.value.findIndex(member => member.id === p.id)
  const existing = cs.members.value[idx]
  if (!existing) return
  const next = { ...existing }
  if (p.timeout_until) next.timeout_until = p.timeout_until
  else delete next.timeout_until
  cs.members.value[idx] = next
}

function onMessageDelete(cx: Cx, p: Payload<'message_delete'>) {
  const { cs } = cx
  if (!p?.id) return
  removeFromWindow(cx, p.id)
  onPinnedMaybeDeleted(cx, p.channel_id)
  refreshMemberStats(cx)
  cs.threadReplies.value = cs.threadReplies.value.filter(m => m.id !== p.id)
  onOriginalDeleted(cx, p.id)
  if (p.parent_id) bumpReplyCount(cx, p.parent_id, -1)
  if (cs.activeThread.value?.id === p.id) closeThread(cx)
}

const HANDLERS: Handlers = {
  pong: ({ voiceStore }, p) => {
    if (p?.t) voiceStore.recordPing(Date.now() - p.t)
  },
  server_info: ({ authStore, voiceStore }, p) => {
    const versions = useAppVersionStore()
    versions.setServerVersion(p?.version)
    voiceStore.serverScreenLayers = p?.screen_layers ?? 0
    versions.followAdminUpdates(() => authStore.isAdmin)
  },
  // An admin started an update: the server restarts in a moment.
  system_update: (_cx, p) => useAppVersionStore().setUpdating(p?.version),
  presence_snapshot: onPresenceSnapshot,
  presence_update: onPresenceUpdate,
  member_joined: cx => { fetchMembers(cx) },
  channels_changed: cx => { fetchChannels(cx) },
  // My roles, a role of mine or a channel's overwrites changed.
  permissions_changed: cx => {
    usePermissionsStore().load()
    fetchChannels(cx)
  },
  emoji_changed: () => { void useEmojiStore().load() },
  thread_follow_updated: onThreadFollowUpdated,
  // The block list lives in the blocks store of the direct messages client (next package); nothing to refresh yet.
  block_updated: () => {},
  saved_updated: (_cx, p) => { if (p) useSavedStore().onUpdated(p) },
  // The direct messages client (next package) keeps these lists; nothing to refresh yet.
  dm_created: () => {},
  dm_member_added: () => {},
  dm_member_removed: () => {},
  roles_changed: cx => {
    const permissions = usePermissionsStore()
    permissions.loadRoles()
    permissions.load()
    fetchMembers(cx)
  },
  typing: (cx, p) => { if (p) handleTypingEvent(cx, p) },
  read_state: onReadState,
  message_create: onMessageCreate,
  user_update: (cx, p) => { if (p) updateUserEverywhere(cx, p) },
  member_timeout: onMemberTimeout,
  moderation_notice: (_cx, p) => {
    useToastStore().info(noticeText(p))
    usePermissionsStore().load()
  },
  // Signed out everywhere by a moderator: a calm note that stays long enough to read, no sanction.
  kicked: (_cx, p) => {
    useToastStore().info(kickedText(p?.reason ?? ''), { duration: KICKED_NOTICE_MS })
  },
  message_update: (cx, p) => {
    if (!p) return
    applyToMessage(cx, p.id, m => Object.assign(m, p))
    onOriginalEdited(cx, p)
  },
  message_delete: onMessageDelete,
  pins_changed: (cx, p) => { if (p) onPinsChanged(cx, p) },
  message_reaction: (cx, p) => {
    if (p?.message_id) applyToMessage(cx, p.message_id, m => { m.reactions = p.reactions })
  },
  poll_update: (cx, p) => { if (p?.message_id) applyPoll(cx, p.message_id, p.poll) },
  interaction_response: ({ cs }, p) => {
    if (p?.channel_id) cs.ephemeral.value = [...cs.ephemeral.value, { ...p, id: ++cs.ephemeralSeq }].slice(-MAX_EPHEMERAL)
  },
  webrtc_offer: ({ cs }, p) => { cs.webrtcOfferHandler?.(p) },
  webrtc_candidate: ({ cs }, p) => { cs.webrtcCandidateHandler?.(p) },
  media_handshake: ({ cs }, p) => { cs.mediaHandshakeHandler?.(p) },
  voice_snapshot: ({ voiceStore }, p) => voiceStore.setVoiceSnapshot(p),
  voice_state_update: ({ voiceStore }, p) => voiceStore.handleVoiceStateUpdate(p),
  voice_rooms: ({ voiceStore }, p) => voiceStore.setVoiceRooms(p),
  user_stats: ({ cs }, p) => {
    if (!p?.user_id) return
    const m = cs.members.value.find(x => x.id === p.user_id)
    if (m) m.voice_seconds = p.voice_seconds
  },
  webrtc_media_state: ({ voiceStore }, p) => voiceStore.handleMediaState(p),
  screen_viewers: ({ voiceStore }, p) => voiceStore.handleScreenViewers(p),
  screen_layers: ({ voiceStore }, p) => voiceStore.handleScreenLayers(p),
  screen_quality: ({ voiceStore }, p) => voiceStore.handleScreenQuality(p),
  screen_layers_wanted: ({ voiceStore }, p) => voiceStore.handleLayersWanted(p),
  voice_speaking: ({ voiceStore }, p) => voiceStore.handleSpeakingEvent(p),
  voice_mute_state: ({ voiceStore }, p) => voiceStore.handleMuteState(p),
  // An admin removed me from voice; the call ends locally.
  voice_kicked: ({ cs, voiceStore }, p) => {
    if (cs.voiceKickedHandler) cs.voiceKickedHandler(p)
    else if (voiceStore.currentChannelId) voiceStore.disconnect()
  }
}

export function handleWSEvent(cx: Cx, event: ServerEvent) {
  if (!Object.hasOwn(HANDLERS, event.type)) return
  const handler = HANDLERS[event.type] as (cx: Cx, payload: unknown) => void
  handler(cx, event.payload)
}
