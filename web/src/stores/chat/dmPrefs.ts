// What the member does to their own list of conversations: favourites and the
// list-wide settings (mute all, the folded section). Closing a conversation is
// in dms.ts, as opening one reopens it. All of it is server-side per member,
// so it follows them to every device; other sessions hear dm_prefs_updated.
import type { ChatDMSettings } from '../../types/rest'
import { decodeChatDMSettings } from '../../types/rest'
import { api } from '../../lib/api'
import { DM_MAX_FAVOURITES, splitDms } from '../../lib/dmList'
import { assertCommunity, type Cx } from './state'
import { fetchDmSettings, fetchDms, refreshDm, setDms } from './dms'

// Saves go to the server one at a time, each on top of what the server last
// confirmed, so overlapping changes can neither answer out of order nor roll
// each other back to a value the server no longer has.
interface SettingsSaves { community: number; tail: Promise<unknown>; pending: number; confirmed: ChatDMSettings }
const settingsSaves = new WeakMap<object, SettingsSaves>()

/** Saves a change to the list-wide settings; the screen follows at once and goes back if the server refuses. */
export function saveDmSettings(cx: Cx, change: Partial<ChatDMSettings>) {
  const { cs } = cx
  const community = cs.communityGeneration
  let saves = settingsSaves.get(cs)
  if (!saves || saves.community !== community) {
    saves = { community, tail: saves?.tail ?? Promise.resolve(), pending: 0, confirmed: cs.dmSettings.value }
    settingsSaves.set(cs, saves)
  }
  const state = saves
  if (state.pending === 0) state.confirmed = cs.dmSettings.value
  state.pending++
  cs.dmSettings.value = { ...cs.dmSettings.value, ...change }

  let settled = false
  const settle = () => {
    if (settled) return
    settled = true
    state.pending--
  }
  async function send() {
    const before = state.confirmed
    try {
      assertCommunity(cx, community)
      const saved = await api('/api/users/me/dm-settings', { method: 'PUT', json: { ...before, ...change }, decode: decodeChatDMSettings })
      assertCommunity(cx, community)
      state.confirmed = saved
      settle()
      // A newer save is still queued: its answer is the one the screen waits for.
      if (state.pending === 0) cs.dmSettings.value = saved
      // Muting all moves every conversation's level and highlight: read the list again.
      if (before.mute_all !== saved.mute_all) await fetchDms(cx)
    } catch (error) {
      settle()
      if (community === cs.communityGeneration) {
        if (state.pending === 0) cs.dmSettings.value = state.confirmed
      }
      throw error
    }
  }
  const run = state.tail.then(send, send)
  state.tail = run.catch(() => undefined)
  return run
}

/** The ids of the caller's favourites, in order. */
export function favouriteIds(cx: Cx): string[] {
  return splitDms(cx.cs.dms.value, null).favourites.map(dm => dm.id)
}

/**
 * Replaces the favourites with ids in that order. The list follows at once; a
 * refusal reads the real state back and tells the caller.
 */
export async function setFavourites(cx: Cx, ids: string[]) {
  const { cs } = cx
  const community = cs.communityGeneration
  if (ids.length > DM_MAX_FAVOURITES) throw new Error('too many favourites')
  setDms(cx, cs.dms.value.map(dm => {
    const at = ids.indexOf(dm.id)
    return { ...dm, favourite_position: at < 0 ? null : at, hidden: at < 0 ? dm.hidden : false }
  }))
  try {
    await api('/api/dms/favourites', { method: 'PUT', json: { ids } })
    assertCommunity(cx, community)
  } catch (error) {
    if (community === cs.communityGeneration) await fetchDms(cx)
    throw error
  }
}

/** Adds the conversation at the end of the favourites, or takes it out. */
export async function toggleFavourite(cx: Cx, id: string) {
  const ids = favouriteIds(cx)
  await setFavourites(cx, ids.includes(id) ? ids.filter(x => x !== id) : [...ids, id])
}

/** Moves a favourite one place up (-1) or down (+1). */
export async function moveFavourite(cx: Cx, id: string, step: -1 | 1) {
  const ids = favouriteIds(cx)
  const from = ids.indexOf(id)
  const to = from + step
  if (from < 0 || to < 0 || to >= ids.length) return
  ids.splice(to, 0, ...ids.splice(from, 1))
  await setFavourites(cx, ids)
}

/** dm_prefs_updated: the caller changed their list on another device. */
export function onDmPrefsUpdated(cx: Cx, p: { dm_id: string | null }) {
  if (p.dm_id) void refreshDm(cx, p.dm_id, true)
  else void Promise.all([fetchDms(cx), fetchDmSettings(cx)])
}
