import type { User, UserUpdate } from '../../types/domain'
import { decodeUser } from '../../types/domain'
import { isChatMember } from '../../types/rest'
import { arrayDecoder } from '../../types/validation'
import { api } from '../../lib/api'
import { usePermissionsStore } from '../permissions'
import { applyPresenceUpdate, activityOf } from '../../lib/presence'
import type { Cx } from './state'
import { loadedLists } from './messageWindow'
import type { ProfilePreview } from './types'

const decodeMembers = arrayDecoder('members', isChatMember)

/** The activity a bot shows, or null. */
export function activityOfUser(cx: Cx, userId: string | null) {
  const { cs } = cx
  return userId ? cs.activityById.value[userId] ?? null : null
}

export function applyActivity(cx: Cx, update: { user_id: string; status?: unknown; activity?: unknown }) {
  const { cs } = cx
  const activity = update.status === 'offline' ? null : activityOf(update.activity)
  const next = { ...cs.activityById.value }
  if (activity) next[update.user_id] = activity
  else delete next[update.user_id]
  cs.activityById.value = next
}

/** Live status of a user; 'offline' when not connected. */
export function presenceOf(cx: Cx, userId: string | null) {
  const { cs } = cx
  return userId ? cs.presenceById.value[userId] || 'offline' : 'offline'
}

function confirmActiveMember(cx: Cx, id: string) {
  const { cs } = cx
  cs.disabledMembers.delete(id)
  const presence = cs.suppressedPresence.get(id)
  if (presence) cs.presenceById.value = applyPresenceUpdate(cs.presenceById.value, { user_id: id, status: presence })
  cs.suppressedPresence.delete(id)
}

export async function fetchMembers(cx: Cx) {
  const { cs } = cx
  const seq = ++cs.membersFetchSeq
  const revision = cs.memberRevision
  try {
    const data = await api('/api/members', { decode: decodeMembers })
    if (seq !== cs.membersFetchSeq) return
    cs.members.value = data.filter(member => {
      const disabledAt = cs.disabledMembers.get(member.id)
      if (disabledAt !== undefined && disabledAt > revision) return false
      // This request started after the disable event. Since the server only
      // returns active accounts, presence here confirms a later re-enable.
      confirmActiveMember(cx, member.id)
      return true
    }).map(member => {
      const latest = cs.memberProfileUpdates.get(member.id)
      return latest && latest.revision > revision ? { ...member, ...latest.user } : member
    })
    usePermissionsStore().setMemberRoles(cs.members.value)
  } catch (e) {
    console.error('Failed to fetch members:', e)
  }
}

export function countMessageOf(cx: Cx, userId: string) {
  const { cs } = cx
  const m = cs.members.value.find(x => x.id === userId)
  if (m) m.message_count = (m.message_count || 0) + 1
}

export function refreshMemberStats(cx: Cx) {
  const { cs } = cx
  clearTimeout(cs.memberStatsTimer)
  cs.memberStatsTimer = setTimeout(() => fetchMembers(cx), 3000)
}

type DisabledUpdate = Extract<UserUpdate, { disabled: boolean }>
type ProfileUpdate = Exclude<UserUpdate, { disabled: boolean }>

function applyDisabledUpdate(cx: Cx, updated: DisabledUpdate, revision: number) {
  const { cs } = cx
  if (updated.disabled) {
    cs.disabledMembers.set(updated.id, revision)
    cs.suppressedPresence.delete(updated.id)
    cs.members.value = cs.members.value.filter(member => member.id !== updated.id)
    cs.presenceById.value = applyPresenceUpdate(cs.presenceById.value, { user_id: updated.id, status: 'offline' })
  } else {
    confirmActiveMember(cx, updated.id)
    fetchMembers(cx)
  }
}

function applyProfileUpdate({ cs }: Cx, updated: ProfileUpdate, revision: number) {
  cs.memberProfileUpdates.set(updated.id, { revision, user: updated })
  const idx = cs.members.value.findIndex(member => member.id === updated.id)
  const existing = cs.members.value[idx]
  // Profiles do not establish membership. Registration uses member_joined,
  // and re-enabling uses the authoritative active-member roster above.
  if (existing) cs.members.value[idx] = { ...existing, ...updated }
}

// Disabled updates carry no author fields; full profiles always carry a name.
function renameAuthorsEverywhere(cx: Cx, updated: ProfileUpdate) {
  const copy = (target: { avatar_url?: string; display_name?: string }) => {
    target.display_name = updated.display_name
    if ('avatar_url' in updated && updated.avatar_url !== undefined) target.avatar_url = updated.avatar_url
  }
  for (const list of loadedLists(cx)) {
    for (const m of list) {
      if (m.user_id === updated.id) copy(m)
      if (m.reply_to?.user_id === updated.id) copy(m.reply_to)
    }
  }
}

export function updateUserEverywhere(cx: Cx, updated: UserUpdate) {
  const { cs, authStore, voiceStore } = cx
  if (!updated?.id) return
  const revision = ++cs.memberRevision
  if ('disabled' in updated) applyDisabledUpdate(cx, updated, revision)
  else applyProfileUpdate(cx, updated, revision)
  if (!('disabled' in updated)) renameAuthorsEverywhere(cx, updated)
  voiceStore.updateUser(updated)
  if (authStore.user?.id === updated.id) authStore.user = { ...authStore.user, ...updated }
  if (cs.selectedUserProfile.value?.id === updated.id) {
    cs.selectedUserProfile.value = { ...cs.selectedUserProfile.value, ...updated }
  }
}

type ProfileCandidate = Pick<User, 'id'> & Partial<User> & { user_id?: string }

function resolveUserId(candidate?: ProfileCandidate): string | null {
  if (!candidate) return null
  return candidate.user_id || candidate.id || null
}

function pickProfileField<T>(primary?: T | null, secondary?: T | null, fallback: T = '' as T): T {
  if (primary) return primary
  if (secondary) return secondary
  return fallback
}

function buildInitialProfile(
  userId: string,
  candidate: ProfileCandidate,
  known?: Partial<User>
): ProfilePreview {
  const username = pickProfileField(candidate.username, known?.username, '')
  const displayName = pickProfileField(candidate.display_name, known?.display_name, candidate.username || '')
  const avatarUrl = pickProfileField(candidate.avatar_url, known?.avatar_url, '')
  const role = pickProfileField(candidate.role, known?.role, 'user')
  const createdAt = candidate.created_at || known?.created_at

  const profile: ProfilePreview = {
    id: userId,
    username,
    display_name: displayName,
    avatar_url: avatarUrl,
    role
  }
  if (createdAt) profile.created_at = createdAt
  return profile
}

export async function openUserProfile(cx: Cx, userOrMessage: ProfileCandidate) {
  const { cs } = cx
  const community = cs.communityGeneration
  const userId = resolveUserId(userOrMessage)
  if (!userId) return

  const known = cs.members.value.find(m => m.id === userId)
  cs.selectedUserProfile.value = buildInitialProfile(userId, userOrMessage, known)

  try {
    const full = await api(`/api/users/${userId}`, { decode: decodeUser })
    // Closed, or another profile opened, while this one loaded.
    const isCurrent = community === cs.communityGeneration && cs.selectedUserProfile.value?.id === userId
    if (!isCurrent) return
    cs.selectedUserProfile.value = { ...cs.selectedUserProfile.value, ...full }
  } catch (e) {
    console.warn('Failed to fetch full user profile:', e)
  }
}

export function closeUserProfile(cx: Cx) {
  const { cs } = cx
  cs.selectedUserProfile.value = null
}

export function insertMention(cx: Cx, username: string) {
  const { cs } = cx
  if (username) cs.pendingMention.value = username
}

