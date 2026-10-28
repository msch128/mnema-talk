import * as voiceSession from '../../lib/voiceSession'
import { preloadNoiseSuppressor } from '../../lib/noiseSuppressor'
import { useToastStore } from '../../stores/toast'
import { useAuthStore } from '../../stores/auth'
import { usePermissionsStore } from '../../stores/permissions'
import { mediaCall } from '../../lib/media/api'
import { t } from '../../i18n'
import { platform } from '../../lib/platform'
import type { SubscribeRequest, ServerEventPayloads } from '../../types/events'
import { loadIceServers } from './iceServers'
import { isLobby, joinViaLobby, lobbyMoved, lobbyRefused } from './lobby'
import { resumeRemoteAudio as resumeRemotePlayback } from './remoteAudio'
import { closeMediaRoom, openMediaRoom, resetMediaAnnouncements } from './mediaRoom'
import type { Cx } from './state'
import { aiModel } from './audioMath'
import { stopCamera } from './camera'
import { micErrorKey, refreshAudioDevices, requestMicrophone, setupMicPipeline } from './micPipeline'
import { stopMicTest } from './micTest'
import { cleanupPeerConnection, handleRemoteCandidate, handleRemoteOffer, setupPeerConnection } from './peer'
import { stopScreenShare } from './screen'
import { cleanupVoiceAudio, startSpeakingDetection } from './voiceAudio'

function registerSignalingHandlers(cx: Cx) {
  const { voice, chat } = cx
  chat.setWebRTCHandlers({
    onOffer: offer => handleRemoteOffer(cx, offer),
    onCandidate: candidate => handleRemoteCandidate(cx, candidate),
    onKicked: payload => handleKicked(cx, payload),
    onTalkMoved: payload => lobbyMoved(cx, payload),
    onMedia(payload) {
      if (payload.channel_id !== voice.currentChannelId) return
      mediaCall().receive(payload.user_id, payload.wire)
    }
  })
}

function startMediaCall({ voice }: Cx, channelId: string) {
  const mediaUser = useAuthStore().user?.id
  if (mediaUser) {
    mediaCall().start(channelId, mediaUser)
    // Whoever the Talk already lists (also members whose reload is still in
    // its grace period): the group must not take the room for empty and
    // found its own before the roster changes for the first time.
    mediaCall().roster(Object.keys(voice.channelUsers[channelId] || {}))
  }
}

async function captureMicrophone(cx: Cx): Promise<MediaStream | null> {
  const model = aiModel(cx.voice.noiseMode)
  if (model) preloadNoiseSuppressor(model)
  try {
    await refreshAudioDevices(cx)
    return await requestMicrophone(cx)
  } catch (err) {
    console.warn('Microphone access denied or unavailable:', err)
    // The join goes on listen-only; say why instead of leaving the user mute.
    useToastStore().error(t(micErrorKey(err)))
    return null
  }
}

function canJoinVoiceChannel(cx: Cx, channelId: string): boolean {
  if (!usePermissionsStore().canIn(channelId, 'connect')) {
    useToastStore().error(t('talk.noConnect'))
    return false
  }
  if (platform.isDesktop()) {
    useToastStore().error(t('nativeDesktop.mediaUnavailable'))
    return false
  }
  return !(channelId === cx.voice.currentChannelId && cx.st.localAudioStream.value)
}

function stopStreams(...streams: (MediaStream | null | undefined)[]) {
  for (const s of streams) {
    s?.getTracks().forEach(t => t.stop())
  }
}

async function prepareAudioPipeline(cx: Cx, gen: number): Promise<MediaStream | null | false> {
  const stream = await captureMicrophone(cx)
  if (gen !== cx.st.joinGeneration) {
    stopStreams(stream)
    return false
  }
  const sendStream = stream ? await setupMicPipeline(cx, stream) : null
  if (gen !== cx.st.joinGeneration) {
    stopStreams(sendStream, stream)
    return false
  }
  return sendStream
}

/**
 * A browser that cannot seal call media joins nothing; unsealed media is never
 * an option. Null when nothing needs asking, so the join does not wait a tick.
 */
function sealingRefusal(): Promise<boolean> | null {
  const probe = mediaCall().canSeal?.()
  if (!probe) return null
  return probe.then(able => {
    if (!able) useToastStore().error(t('voice.mediaUnsupported'))
    return !able
  })
}

export async function joinVoiceChannel(cx: Cx, channelId: string): Promise<void> {
  if (!canJoinVoiceChannel(cx, channelId)) return
  if (isLobby(cx, channelId)) {
    registerSignalingHandlers(cx)
    return joinViaLobby(cx, channelId, talkId => joinVoiceChannel(cx, talkId))
  }
  const refusal = sealingRefusal()
  if (refusal && await refusal) return
  const { voice, chat, st } = cx
  resetMediaAnnouncements()
  closeMediaRoom()
  stopMicTest(cx)
  cleanupVoiceAudio(cx)
  registerSignalingHandlers(cx)

  const gen = ++st.joinGeneration
  voice.setChannel(channelId)
  startMediaCall(cx, channelId)
  voiceSession.track(channelId)
  await loadIceServers()
  if (gen !== st.joinGeneration) return

  const sendStream = await prepareAudioPipeline(cx, gen)
  if (sendStream === false) return

  st.localAudioStream.value = sendStream
  voice.localAudioStream = sendStream
  setupPeerConnection(cx)
  chat.sendWSEvent('voice_join', { channel_id: channelId })
  openMediaRoom()
  attachSubscriptions(cx)

  if (sendStream) {
    startSpeakingDetection(cx)
    st.pushToTalk.attach(voice)
  }
}

// Everyone sees whether I am muted or deafened (marks on my avatar).
export function sendMuteState(cx: Cx) {
  const { voice, chat } = cx
  if (!voice.currentChannelId) return
  chat.sendWSEvent('voice_mute_state', { muted: !!voice.isMuted, deafened: !!voice.isDeafened })
}

// After the WebSocket reconnected the server may have dropped our media
// peer; start a fresh connection and announce the join again. The server
// treats it as a resume (no leave/join for the others) within its grace time.
export function rejoinAfterReconnect(cx: Cx) {
  const { voice, chat } = cx
  if (platform.isDesktop()) return
  const channelId = voice.currentChannelId
  if (!channelId) return
  // The server keeps the roster across a short disconnect, so nobody else
  // joins or leaves and a handshake that was dropped is not sent again.
  setupPeerConnection(cx)
  chat.sendWSEvent('voice_join', { channel_id: channelId })
  // After the join, so a package it produces is not dropped by the hub.
  mediaCall().resume()
  attachSubscriptions(cx)
}

// Which video I receive is decided on the server: tell it my camera opt-outs
// and screen opt-ins after every join, and forward later changes.
function attachSubscriptions(cx: Cx) {
  const { voice, chat } = cx
  voice.setSubscriptionSink((msg: SubscribeRequest) => chat.sendWSEvent('webrtc_subscribe', msg))
  voice.resendSubscriptions()
  sendMuteState(cx)
}

// Ends the call locally. The stored session is cleared and the channel
// unset, so neither a reload nor a reconnect rejoins it.
function endCall(cx: Cx, { notifyServer }: { notifyServer: boolean }) {
  const { voice, chat, st } = cx
  st.joinGeneration++
  resetMediaAnnouncements()
  closeMediaRoom()
  mediaCall().stop()
  voiceSession.forget()
  cleanupPeerConnection(cx)
  cleanupVoiceAudio(cx)
  stopScreenShare(cx)
  stopCamera(cx)
  if (notifyServer) chat.sendWSEvent('voice_leave', {})
  voice.disconnect()
  voice.audioBlocked = false
}

export function leaveVoiceChannel(cx: Cx) {
  endCall(cx, { notifyServer: true })
}

const KICK_NOTICE: Record<string, string> = {
  room_full: 'voice.roomFull',
  missing_permission: 'voice.noPermission',
  connection_lost: 'voice.connectionLost',
  media_unavailable: 'voice.mediaUnavailable',
  joined_elsewhere: 'voice.joinedElsewhere',
}

// An admin removed me, or the server refused a full room. The server has
// already ended or rejected membership, so no voice_leave goes out.
function handleKicked(cx: Cx, payload: ServerEventPayloads['voice_kicked']) {
  const { voice } = cx
  if (lobbyRefused(cx, payload)) return
  if (!voice.currentChannelId) return
  if (payload.channel_id !== voice.currentChannelId) return
  endCall(cx, { notifyServer: false })
  useToastStore().info(t(KICK_NOTICE[payload.reason ?? ''] ?? 'voice.kicked'))
}

// Rejoins the channel of a call interrupted by a reload less than 30 s ago.
// Must run once the WebSocket is connected (voice_join goes over it).
export async function resumeVoiceSession(cx: Cx) {
  const { voice, chat } = cx
  if (platform.isDesktop()) return false
  const channelId = voiceSession.recent()
  if (!channelId || voice.currentChannelId) return false
  if (!chat.isVoiceChannel(channelId)) {
    voiceSession.forget()
    return false
  }
  await joinVoiceChannel(cx, channelId)
  return true
}

// Called from a user click when autoplay was blocked after a reload.
export function resumeRemoteAudio(cx: Cx) {
  const { voice } = cx
  voice.audioBlocked = false
  resumeRemotePlayback(voice)
}

