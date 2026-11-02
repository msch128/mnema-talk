import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { Ref } from 'vue'
import { api } from '../lib/api'
import { arrayDecoder } from '../types/validation'
import { isPermRole } from '../types/rest'
import { decodePermissionsInfo } from '../types/domain'
import type { PermissionsInfo, Role } from '../types/domain'
import { useAuthStore } from './auth'
import { ADMIN_AREA_KEYS, DEFAULT_EVERYONE, hasPermission, roleColorCss } from '../lib/permissions'
import type { PermissionKey } from '../lib/permissions'

const decodeRoles = arrayDecoder('roles', isPermRole)

/** What the server's answer says, as the computed values the interface reads. */
function infoViews(info: Ref<PermissionsInfo | null>) {
  return {
    loaded: computed(() => info.value !== null),
    global: computed(() => (info.value ? info.value.permissions : DEFAULT_EVERYONE)),
    channelPermissions: computed(() => new Map((info.value?.channels ?? []).map(c => [c.channel_id, c.permissions]))),
    topPosition: computed(() => info.value?.top_position ?? 0),
    myRoleIds: computed(() => info.value?.role_ids ?? []),
    // A running timeout, as milliseconds since the epoch; null without one.
    timeoutUntil: computed(() => {
      const until = info.value?.timeout_until
      return until ? Date.parse(until) : null
    }),
    timeoutReason: computed(() => info.value?.timeout_reason ?? ''),
    /** The largest file the member may upload, in MB; null until known. */
    maxUploadMb: computed(() => info.value?.max_upload_mb ?? null)
  }
}

/** Loading permissions and roles from the server; the newest permissions request wins. */
function useServerLoading(info: Ref<PermissionsInfo | null>, roles: Ref<Role[]>) {
  let loadSeq = 0
  // The newest request: a load that was superseded by a later one (the socket
  // opening refreshes too) must not report "done" before that one has landed,
  // or a caller that waits for the permissions (the deep link into the admin
  // area) would look at nothing.
  let latest: Promise<void> = Promise.resolve()

  async function fetchPermissions(seq: number) {
    try {
      const data = await api('/api/permissions', { decode: decodePermissionsInfo })
      if (seq === loadSeq) info.value = data
    } catch (e) {
      console.error('Failed to fetch permissions:', e)
    }
  }

  async function load() {
    const run = fetchPermissions(++loadSeq)
    latest = run
    let waited: Promise<void>
    do {
      waited = latest
      await waited
    } while (waited !== latest)
  }

  async function loadRoles() {
    try {
      roles.value = await api('/api/roles', { decode: decodeRoles })
    } catch (e) {
      console.error('Failed to fetch roles:', e)
    }
  }

  return { load, loadRoles, invalidate: () => { ++loadSeq } }
}

// What the signed-in member may do, as the server reports it (GET
// /api/permissions), plus the roles (GET /api/roles). The server is the
// authority; this only decides what the interface offers. Before the first
// answer a member is assumed to hold what @everyone holds by default, so the
// interface does not flicker, and the Owner (known from the account) holds
// everything.
export const usePermissionsStore = defineStore('permissions', () => {
  const auth = useAuthStore()
  const info = ref<PermissionsInfo | null>(null)
  const roles = ref<Role[]>([])
  // Role IDs of the other members, highest first (from GET /api/members).
  const memberRoles = ref<Record<string, string[]>>({})

  const isOwner = computed(() => auth.isAdmin)
  const { loaded, global, channelPermissions, topPosition, myRoleIds, timeoutUntil, timeoutReason, maxUploadMb } = infoViews(info)

  /** Does the member hold a server-wide permission? */
  function can(key: PermissionKey): boolean {
    return isOwner.value || hasPermission(global.value, key)
  }

  /** Does the member hold a permission in a channel (which they may view)? */
  function canIn(channelId: string, key: PermissionKey): boolean {
    if (isOwner.value) return true
    if (!info.value) return hasPermission(DEFAULT_EVERYONE, key)
    const set = channelPermissions.value.get(channelId)
    return set !== undefined && hasPermission(set, key)
  }

  const canOpenAdmin = computed(() => isOwner.value || ADMIN_AREA_KEYS.some(key => can(key)))

  /** A role the member may edit, delete, assign or move: strictly below their highest. */
  function outranks(role: Role): boolean {
    return isOwner.value || role.position < topPosition.value
  }

  /** May the member change this other member's roles or moderate them? */
  function outranksMember(userId: string): boolean {
    if (isOwner.value) return true
    // A role members choose for themselves is cosmetic and never raises a rank (as on the server).
    const top = Math.max(0, ...(memberRoles.value[userId] ?? []).map(id => { const r = roleById(id); return r && !r.self_assignable ? r.position : 0 }))
    return top < topPosition.value
  }

  function roleById(id: string): Role | undefined {
    return roles.value.find(role => role.id === id)
  }

  function rolesOf(userId: string): Role[] {
    const ids = userId === auth.user?.id ? myRoleIds.value : (memberRoles.value[userId] ?? [])
    return ids.map(roleById).filter((role): role is Role => role !== undefined)
  }

  /** The colour of a member's name: their highest role that has one, or ''. */
  function nameColor(userId: string): string {
    for (const role of rolesOf(userId)) {
      const css = roleColorCss(role.color)
      if (css) return css
    }
    return ''
  }

  function setMemberRoles(members: { id: string; role_ids?: string[] | undefined }[]) {
    memberRoles.value = Object.fromEntries(members.map(m => [m.id, m.role_ids ?? []]))
  }

  const loader = useServerLoading(info, roles)

  function reset() {
    loader.invalidate()
    info.value = null
    roles.value = []
    memberRoles.value = {}
  }

  return {
    info, roles, memberRoles, maxUploadMb, isOwner, loaded, topPosition, myRoleIds, timeoutUntil, timeoutReason, canOpenAdmin,
    can, canIn, outranks, outranksMember, roleById, rolesOf, nameColor, setMemberRoles, load: loader.load, loadRoles: loader.loadRoles, reset
  }
})
