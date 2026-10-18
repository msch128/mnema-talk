import type { ServerEvent } from '../../types/events'
import { useToastStore } from '../toast'
import { usePermissionsStore } from '../permissions'
import { useAppVersionStore } from '../appVersion'
import { noticeText } from '../../lib/moderation'
import { snapshotToMap, applyPresenceUpdate } from '../../lib/presence'
import { MAX_EPHEMERAL } from './types'
import type { Cx } from './state'
import { fetchChannels } from './channels'
import { applyActivity, countMessageOf, fetchMembers, refreshMemberStats, updateUserEverywhere } from './members'
import { applyToMessage, bumpReplyCount, countReply, insertMessage, onOriginalDeleted, onOriginalEdited, removeFromWindow } from './messageWindow'
import { messageMentionsMe, triggerBrowserNotification } from './notifications'
import { fetchReadState, isReadingActiveChannel } from './readState'
import { closeThread } from './threads'
import { clearTypingForUser, handleTypingEvent } from './typing'

export function handleWSEvent(cx: Cx, event: ServerEvent) {
  const { cs, authStore, voiceStore } = cx
  const { type, payload: p } = event
  switch (type) {
    case 'pong':
      if (p?.t) voiceStore.recordPing(Date.now() - p.t)
      break

    case 'server_info': {
      const versions = useAppVersionStore()
      versions.setServerVersion(p?.version)
      voiceStore.serverScreenLayers = p?.screen_layers ?? 0
      versions.followAdminUpdates(() => authStore.isAdmin)
      break
    }

    case 'system_update':
      // An admin started an update: the server restarts in a moment.
      useAppVersionStore().setUpdating(p?.version)
      break

    case 'presence_snapshot': {
      const snapshot = snapshotToMap(p)
      for (const id of cs.disabledMembers.keys()) {
        const presence = snapshot[id]
        if (presence) cs.suppressedPresence.set(id, presence)
        else cs.suppressedPresence.delete(id)
        delete snapshot[id]
      }
      cs.presenceById.value = snapshot
      cs.activityById.value = {}
      break
    }

    case 'presence_update':
      if (p) applyActivity(cx, p)
      if (p && cs.disabledMembers.has(p.user_id)) {
        if (p.status === 'offline') cs.suppressedPresence.delete(p.user_id)
        else cs.suppressedPresence.set(p.user_id, p.status)
      } else {
        cs.presenceById.value = applyPresenceUpdate(cs.presenceById.value, p)
      }
      break

    case 'member_joined':
      fetchMembers(cx)
      break

    case 'channels_changed':
      fetchChannels(cx)
      break

    case 'permissions_changed':
      // My roles, a role of mine or a channel's overwrites changed.
      usePermissionsStore().load()
      fetchChannels(cx)
      break

    case 'roles_changed': {
      const permissions = usePermissionsStore()
      permissions.loadRoles()
      permissions.load()
      fetchMembers(cx)
      break
    }

    case 'typing':
      if (p) handleTypingEvent(cx, p)
      break

    case 'read_state':
      if (!p?.channel_id) break
      if ('refresh' in p && p.refresh) {
        fetchReadState(cx)
      } else {
        const prev = cs.readStates.value[p.channel_id] || { channel_id: p.channel_id, notify_level: 'all' as const, unread_count: 0, mention_count: 0, last_read_at: null }
        cs.readStates.value = {
          ...cs.readStates.value,
          [p.channel_id]: {
            ...prev,
            ...p
          }
        }
      }
      break

    case 'message_create': {
      if (!p) break
      countReply(cx, p)
      insertMessage(cx, p)
      countMessageOf(cx, p.user_id)

      clearTypingForUser(cx, p.channel_id, p.user_id)

      const isOwn = p.user_id === authStore.user?.id
      const isCurrentChannel = p.channel_id === cs.activeChannel.value?.id
      const isMention = messageMentionsMe(cx, p)

      // Read right now: open channel, visible tab, window at the newest end.
      if (isCurrentChannel && isReadingActiveChannel(cx)) {
        if (!isOwn) cs.markReadThrottled.call(p.channel_id)
      } else if (!isOwn && p.channel_id) {
        const state = cs.readStates.value[p.channel_id] || {
          channel_id: p.channel_id,
          unread_count: 0,
          mention_count: 0,
          notify_level: 'all' as const,
          last_read_at: null
        }
        cs.readStates.value = {
          ...cs.readStates.value,
          [p.channel_id]: {
            ...state,
            unread_count: (state.unread_count || 0) + 1,
            mention_count: isMention ? (state.mention_count || 0) + 1 : (state.mention_count || 0)
          }
        }
      }

      if (!isOwn && p.channel_id) {
        triggerBrowserNotification(cx, p, isMention)
      }
      break
    }

    case 'user_update':
      if (p) updateUserEverywhere(cx, p)
      break

    case 'member_timeout': {
      const idx = cs.members.value.findIndex(member => member.id === p.id)
      const existing = cs.members.value[idx]
      if (existing) {
        const next = { ...existing }
        if (p.timeout_until) next.timeout_until = p.timeout_until
        else delete next.timeout_until
        cs.members.value[idx] = next
      }
      break
    }

    case 'moderation_notice':
      useToastStore().info(noticeText(p))
      usePermissionsStore().load()
      break

    case 'message_update':
      if (!p) break
      applyToMessage(cx, p.id, m => Object.assign(m, p))
      onOriginalEdited(cx, p)
      break

    case 'message_delete':
      if (!p?.id) break
      removeFromWindow(cx, p.id)
      refreshMemberStats(cx)
      cs.threadReplies.value = cs.threadReplies.value.filter(m => m.id !== p.id)
      onOriginalDeleted(cx, p.id)
      if (p.parent_id) bumpReplyCount(cx, p.parent_id, -1)
      if (cs.activeThread.value?.id === p.id) closeThread(cx)
      break

    case 'message_reaction':
      if (p?.message_id) applyToMessage(cx, p.message_id, m => { m.reactions = p.reactions })
      break

    case 'interaction_response':
      if (p?.channel_id) cs.ephemeral.value = [...cs.ephemeral.value, { ...p, id: ++cs.ephemeralSeq }].slice(-MAX_EPHEMERAL)
      break

    case 'webrtc_offer':
      cs.webrtcOfferHandler?.(p)
      break

    case 'webrtc_candidate':
      cs.webrtcCandidateHandler?.(p)
      break

    case 'media_handshake':
      cs.mediaHandshakeHandler?.(p)
      break

    case 'voice_snapshot':
      voiceStore.setVoiceSnapshot(p)
      break

    case 'voice_state_update':
      voiceStore.handleVoiceStateUpdate(p)
      break

    case 'voice_rooms':
      voiceStore.setVoiceRooms(p)
      break

    case 'user_stats':
      if (p?.user_id) {
        const m = cs.members.value.find(x => x.id === p.user_id)
        if (m) m.voice_seconds = p.voice_seconds
      }
      break

    case 'webrtc_media_state':
      voiceStore.handleMediaState(p)
      break

    case 'screen_viewers':
      voiceStore.handleScreenViewers(p)
      break

    case 'screen_layers':
      voiceStore.handleScreenLayers(p)
      break

    case 'screen_quality':
      voiceStore.handleScreenQuality(p)
      break

    case 'screen_layers_wanted':
      voiceStore.handleLayersWanted(p)
      break

    case 'voice_speaking':
      voiceStore.handleSpeakingEvent(p)
      break

    case 'voice_mute_state':
      voiceStore.handleMuteState(p)
      break

    case 'voice_kicked':
      // An admin removed me from voice; the call ends locally.
      if (cs.voiceKickedHandler) cs.voiceKickedHandler(p)
      else if (voiceStore.currentChannelId) voiceStore.disconnect()
      break
  }
}

