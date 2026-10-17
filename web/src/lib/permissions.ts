// Permission bits, mirrored from internal/perm/perm.go. The values are stored
// in the server's database and never change meaning; a Go test compares this
// table with the server's catalog. The server stays the authority: this only
// decides what the interface shows.

export const PERMISSION_BITS = {
  administrator: 1,
  view_channel: 2,
  send_messages: 4,
  attach_files: 8,
  add_reactions: 16,
  manage_messages: 32,
  mention_everyone: 64,
  connect: 128,
  speak: 256,
  video: 512,
  disconnect_members: 1024,
  manage_channels: 2048,
  manage_roles: 4096,
  create_invites: 8192,
  manage_webhooks: 16384,
  manage_members: 32768
} as const

export type PermissionKey = keyof typeof PERMISSION_BITS

/** Display order, the same as the server's catalog. */
export const PERMISSION_KEYS = Object.keys(PERMISSION_BITS) as PermissionKey[]

/** Permissions a category or channel can allow or deny per role or member. */
export const CHANNEL_PERMISSION_KEYS: readonly PermissionKey[] = [
  'view_channel', 'send_messages', 'attach_files', 'add_reactions', 'manage_messages',
  'mention_everyone', 'connect', 'speak', 'video', 'disconnect_members'
]

/** Any of these opens a part of the admin area. */
export const ADMIN_AREA_KEYS: readonly PermissionKey[] = [
  'manage_channels', 'manage_roles', 'create_invites', 'manage_webhooks', 'manage_members', 'disconnect_members'
]

/** What @everyone holds on a fresh install (internal/perm DefaultEveryone). */
export const DEFAULT_EVERYONE = 990

export function hasPermission(set: number, key: PermissionKey): boolean {
  const bit = PERMISSION_BITS[key]
  return (set & bit) === bit
}

export function setPermission(set: number, key: PermissionKey, on: boolean): number {
  const bit = PERMISSION_BITS[key]
  return on ? set | bit : set & ~bit
}

/** CSS colour of a role, or '' for a role without one. */
export function roleColorCss(color: number): string {
  return color > 0 ? `#${color.toString(16).padStart(6, '0')}` : ''
}

/** Parses "#rrggbb" (or "rrggbb") into a role colour; null when invalid. */
export function parseRoleColor(text: string): number | null {
  const match = /^#?([0-9a-fA-F]{6})$/.exec(text.trim())
  return match ? parseInt(match[1] as string, 16) : null
}

/** Colours offered as quick picks in the role editor. */
export const ROLE_COLOR_PRESETS: readonly number[] = [
  0x6aa7ff, 0x5fd0b6, 0x8ad46a, 0xe8c25a, 0xf0955a, 0xe8687a, 0xc08af0, 0xa0a8b8
]
