import type { ChosenPresence } from '../../types/domain'
import { decodeUser } from '../../types/domain'
import { api } from '../../lib/api'
import { applyPresenceUpdate, IDLE_AFTER_MS } from '../../lib/presence'
import { assertCommunity, type Cx } from './state'
import { updateUserEverywhere } from './members'
import { sendWSEvent } from './socket'

export function markActive(cx: Cx) {
  const { cs } = cx
  clearTimeout(cs.idleTimer)
  cs.idleTimer = setTimeout(() => {
    cs.isIdle = true
    sendWSEvent(cx, 'presence_idle', { idle: true })
  }, IDLE_AFTER_MS)
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

/** Sets the signed-in user's presence (online, away, dnd, focus). */
export async function setMyPresence(cx: Cx, presence: ChosenPresence) {
  const { cs, authStore } = cx
  const community = cs.communityGeneration
  const me = authStore.user
  if (!me) return
  const previous = me.presence
  authStore.user = { ...me, presence }
  if (cs.presenceById.value[me.id]) {
    cs.presenceById.value = applyPresenceUpdate(cs.presenceById.value, {
      user_id: me.id,
      status: presence === 'online' && cs.isIdle ? 'away' : presence
    })
  }
  try {
    const updated = await api('/api/users/me/presence', { method: 'PUT', json: { presence }, decode: decodeUser })
    assertCommunity(cx, community)
    authStore.user = updated
  } catch (e) {
    assertCommunity(cx, community)
    // A failed presence request must preserve newer profile updates and a
    // session that was cleared or switched while the request was pending.
    const current = authStore.user
    if (current?.id === me.id) {
      const restored = { ...current }
      if (previous === undefined) delete restored.presence
      else restored.presence = previous
      authStore.user = restored
    }
    throw e
  }
}

/** Sets a status line: the user's own, or (admins) anyone's. */
export async function setStatusText(cx: Cx, userId: string, text: string) {
  const { cs, authStore } = cx
  const community = cs.communityGeneration
  const own = userId === authStore.user?.id
  const updated = await api(own ? '/api/users/me/status' : `/api/admin/users/${userId}/status`, {
    method: 'PUT',
    json: { status_text: text }, decode: decodeUser
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
}
