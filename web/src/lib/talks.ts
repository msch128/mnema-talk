// Create-a-Talk: who may configure a Talk, and the calls the interface makes
// (doc/talks.md). The server decides everything again; this only offers what fits.
import { shallowRef } from 'vue'
import { api } from './api'
import { decodeChatMessage, decodeChatStartTalkResponse } from '../types/rest'
import { decodeChannel } from '../types/domain'
import { t } from '../i18n'
import { useToastStore } from '../stores/toast'
import type { Channel, Message } from '../types/domain'

export function isTempTalk(channel: Channel | null | undefined): boolean {
  return channel?.is_temp === true
}

/** Whether userId leads the temporary Talk. */
export function isTalkLeader(channel: Channel | null | undefined, userId: string | null | undefined): boolean {
  return !!userId && isTempTalk(channel) && channel?.leader_id === userId
}

/**
 * Whether the member sees the Talk settings: a Talk (never a text channel) they
 * may manage, or a temporary Talk they lead.
 */
export function canConfigureTalk(channel: Channel | null | undefined, userId: string | null | undefined, canManage: boolean): boolean {
  if (!channel || channel.type !== 'voice') return false
  return canManage || isTalkLeader(channel, userId)
}

export interface TalkSeats {
  limit: number
  count: number
  /** No seat left for a member without the bypass. */
  full: boolean
}

/**
 * The seats of a Talk with a user limit, or null without one (0 = unlimited,
 * lobbies hold nobody). More people than seats (3/2) is possible: members
 * with Disconnect members, and Administrators, may exceed the limit.
 */
export function talkSeats(channel: Channel | null | undefined, count: number): TalkSeats | null {
  if (!channel || channel.type !== 'voice' || channel.is_lobby) return null
  const limit = channel.user_limit ?? 0
  if (limit <= 0) return null
  return { limit, count, full: count >= limit }
}

/**
 * Tells a member who clicks a full Talk that it is full, and reports whether
 * it did. Not for those already in it or exempt (Disconnect members); the
 * server refuses an over-limit join anyway, this saves the failing attempt.
 */
export function refuseFullTalk(channel: Channel, users: Record<string, unknown> | undefined, exempt: boolean): boolean {
  const count = Object.keys(users ?? {}).length
  if (exempt || !talkSeats(channel, count)?.full) return false
  useToastStore().info(t('talk.fullToast', { count, limit: channel.user_limit }))
  return true
}

/** The Talk whose settings are open; one host in App.vue shows it. */
export const talkSettingsFor = shallowRef<string | null>(null)

export function openTalkSettings(channelId: string) {
  talkSettingsFor.value = channelId
}

export function closeTalkSettings() {
  talkSettingsFor.value = null
}

export interface TalkChange {
  name?: string
  user_limit?: number
}

/** Renames a Talk or sets its limit: through the admin route for those who manage channels, else as the Talk's leader. */
export function updateTalk(channelId: string, change: TalkChange, asManager: boolean): Promise<Channel> {
  const path = asManager ? `/api/admin/channels/${channelId}` : `/api/talks/${channelId}`
  return api(path, { method: 'PATCH', json: change, decode: decodeChannel })
}

export function setLobby(channelId: string, lobby: boolean): Promise<Channel> {
  return api(`/api/admin/channels/${channelId}`, { method: 'PATCH', json: { is_lobby: lobby }, decode: decodeChannel })
}

/** Turns a permanent Talk into a Stage, or back into a normal Talk (needs Manage channels). */
export function setStage(channelId: string, stage: boolean): Promise<Channel> {
  return api(`/api/admin/channels/${channelId}`, { method: 'PATCH', json: { is_stage: stage }, decode: decodeChannel })
}

/** A Stage: only members with talk power send audio and video there. */
export function isStageTalk(channel: Channel | null | undefined): boolean {
  return channel?.type === 'voice' && channel.is_stage === true
}

/** Whether the menu offers Make Stage / Make normal Talk: a permanent Talk that is no lobby. */
export function canToggleStage(channel: Channel | null | undefined): boolean {
  return channel?.type === 'voice' && !channel.is_lobby && !channel.is_temp
}

/** "Start a Talk" in a conversation: the new Talk and the invitation posted into it. */
export function startConversationTalk(conversationId: string) {
  return api(`/api/dms/${conversationId}/talk`, { method: 'POST', decode: decodeChatStartTalkResponse })
}

/** "Invite to my Talk": posts the invitation card for the Talk into a conversation. */
export function inviteConversationToTalk(talkId: string, conversationId: string): Promise<Message> {
  return api(`/api/talks/${talkId}/invites`, { method: 'POST', json: { conversation_id: conversationId }, decode: decodeChatMessage })
}
