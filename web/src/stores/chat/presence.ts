import { watch } from 'vue'
import type { ChosenPresence, Presence } from '../../types/domain'
import { decodeUser } from '../../types/domain'
import { api } from '../../lib/api'
import { applyPresenceUpdate, IDLE_AFTER_MS, VOICE_ACTIVITY_THROTTLE_MS } from '../../lib/presence'
import { assertCommunity, type Cx } from './state'
import { updateUserEverywhere } from './members'
import { sendWSEvent } from './socket'

/** Sharing the screen or the camera in a Talk is activity without input events. */
export function holdsPresence({ voiceStore: voice }: Cx): boolean {
  return voice.isConnected && (voice.isScreenSharing || voice.isCameraOn)
}

/** Starts the idle countdown over; an idle member stays idle. */
function armIdleTimer(cx: Cx) {
  const { cs } = cx
  clearTimeout(cs.idleTimer)
  cs.idleTimer = setTimeout(() => {
    if (holdsPresence(cx)) return armIdleTimer(cx)
    cs.isIdle = true
    sendWSEvent(cx, 'presence_idle', { idle: true })
  }, IDLE_AFTER_MS)
}

export function markActive(cx: Cx) {
  const { cs } = cx
  armIdleTimer(cx)
  if (cs.isIdle) {
    cs.isIdle = false
    sendWSEvent(cx, 'presence_idle', { idle: false })
  }
}

export function onActivity(cx: Cx) {
  const { cs } = cx
  // Input events fire constantly; re-arming once a second is plenty.
  const now = Date.now()
  if (now - cs.lastActivityAt < 1000 && !cs.isIdle) return
  cs.lastActivityAt = now
  markActive(cx)
}

/** Speaking and push-to-talk are activity; the timer is re-armed at most once per VOICE_ACTIVITY_THROTTLE_MS. */
export function onVoiceActivity(cx: Cx) {
  const { cs } = cx
  const now = Date.now()
  if (now - cs.lastVoiceActivityAt < VOICE_ACTIVITY_THROTTLE_MS && !cs.isIdle) return
  cs.lastVoiceActivityAt = now
  markActive(cx)
}

function updateLocalPresence(cx: Cx, meId: string, presence: ChosenPresence): void {
  const { cs } = cx
  if (!cs.presenceById.value[meId]) return
  cs.presenceById.value = applyPresenceUpdate(cs.presenceById.value, {
    user_id: meId,
    status: presence === 'online' && cs.isIdle ? 'away' : presence
  })
}

/** Puts the member list's own entry back, unless a newer change already replaced the optimistic one. */
function restoreListedPresence(cs: Cx['cs'], meId: string, optimistic: Presence | undefined, previous: Presence | undefined): void {
  if (cs.presenceById.value[meId] !== optimistic) return
  cs.presenceById.value = previous === undefined
    ? Object.fromEntries(Object.entries(cs.presenceById.value).filter(([id]) => id !== meId))
    : { ...cs.presenceById.value, [meId]: previous }
}

function restorePresenceOnFailure(authStore: Cx['authStore'], meId: string, previous?: ChosenPresence): void {
  const current = authStore.user
  if (current?.id !== meId) return
  const restored = { ...current }
  if (previous === undefined) delete restored.presence
  else restored.presence = previous
  authStore.user = restored
}

/** Sets the signed-in user's presence (online, away, dnd, focus). */
export async function setMyPresence(cx: Cx, presence: ChosenPresence) {
  const { cs, authStore } = cx
  const community = cs.communityGeneration
  const me = authStore.user
  if (!me) return
  const previous = me.presence
  const previousListed = cs.presenceById.value[me.id]
  authStore.user = { ...me, presence }
  updateLocalPresence(cx, me.id, presence)
  const optimisticListed = cs.presenceById.value[me.id]
  try {
    const updated = await api('/api/users/me/presence', { method: 'PUT', json: { presence }, decode: decodeUser })
    assertCommunity(cx, community)
    authStore.user = updated
  } catch (e) {
    assertCommunity(cx, community)
    // A failed presence request must preserve newer profile updates and a
    // session that was cleared or switched while the request was pending.
    restorePresenceOnFailure(authStore, me.id, previous)
    restoreListedPresence(cs, me.id, optimisticListed, previousListed)
    throw e
  }
}

/** Sets a status line: the user's own, or (admins) anyone's. */
export async function setStatusText(cx: Cx, userId: string, text: string, expiresAt?: Date | null) {
  const { cs, authStore } = cx
  const community = cs.communityGeneration
  const own = userId === authStore.user?.id
  const updated = await api(own ? '/api/users/me/status' : `/api/admin/users/${userId}/status`, {
    method: 'PUT',
    json: own && expiresAt ? { status_text: text, expires_at: expiresAt.toISOString() } : { status_text: text }, decode: decodeUser
  })
  assertCommunity(cx, community)
  updateUserEverywhere(cx, updated)
  return updated
}

export function installActivityListeners(cx: Cx) {
  if (typeof window !== 'undefined') {
    for (const ev of ['pointerdown', 'pointermove', 'keydown', 'wheel', 'touchstart']) {
      window.addEventListener(ev, () => onActivity(cx), { passive: true })
    }
    markActive(cx)
  }
  // Joining, undeafening and turning the camera or a share on are activity; the
  // opposite changes (leaving, a drop, camera or share off) only restart the
  // countdown, so an idle member is not woken by them.
  const { voiceStore: voice } = cx
  const onChange = (now: boolean, before: boolean) => { if (now && !before) markActive(cx); else if (!now && before) armIdleTimer(cx) }
  watch(() => voice.isConnected, onChange, { flush: 'sync' })
  watch(() => voice.isDeafened, (now, before) => onChange(!now, !before), { flush: 'sync' })
  watch(() => voice.isScreenSharing, onChange, { flush: 'sync' })
  watch(() => voice.isCameraOn, onChange, { flush: 'sync' })
  // Push-to-talk also covers the desktop global hotkey, which sets the same flag.
  watch(() => voice.isPttPressed, pressed => { if (pressed && voice.isConnected) onVoiceActivity(cx) }, { flush: 'sync' })
  watch(() => voice.isSpeaking(cx.authStore.user?.id ?? null), speaking => { if (speaking && voice.isConnected) onVoiceActivity(cx) }, { flush: 'sync' })
}
