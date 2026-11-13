// Adding a bot from a manifest (doc/bots.md): reading the pasted or linked
// manifest, its avatar, and the channels the confirm screen offers. The server
// checks the manifest and every tick again; this only prepares what it shows.
import type { BotManifestIntent } from '../../types/rest'
import type { Category, Channel } from '../../types/domain'

/** The avatar of a manifest: a PNG, JPEG or WebP data URL of at most 256 KB. */
export const MAX_AVATAR_BYTES = 256 * 1024
const AVATAR_TYPES: Readonly<Record<string, string>> = { 'image/png': 'png', 'image/jpeg': 'jpg', 'image/webp': 'webp' }

export type ManifestRead = { ok: true; manifest: Record<string, unknown> } | { ok: false; reason: 'json' | 'object' }

/** The JSON text of an add link's base64url manifest, or null when it is not valid UTF-8 base64url. */
export function manifestTextFromParam(param: string): string | null {
  try {
    const base64 = param.replace(/-/g, '+').replace(/_/g, '/').replace(/=+$/, '')
    const binary = atob(base64 + '='.repeat((4 - (base64.length % 4)) % 4))
    const bytes = Uint8Array.from(binary, c => c.charCodeAt(0))
    return new TextDecoder('utf-8', { fatal: true }).decode(bytes)
  } catch {
    return null
  }
}

/** The pasted text as a JSON object, or why it is none. */
export function readManifest(text: string): ManifestRead {
  let value: unknown
  try {
    value = JSON.parse(text)
  } catch {
    return { ok: false, reason: 'json' }
  }
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return { ok: false, reason: 'object' }
  return { ok: true, manifest: value as Record<string, unknown> }
}

export type AvatarRead = { file: File } | { file: null; problem: 'type' | 'size' | 'data' | null }

/**
 * The manifest's avatar as a file to upload, read from its data URL only (a
 * remote address is never fetched). problem is null when there is no avatar.
 */
export function avatarFromManifest(manifest: Record<string, unknown>, name = 'avatar'): AvatarRead {
  const url = manifest['avatar']
  if (url === undefined || url === '') return { file: null, problem: null }
  const match = typeof url === 'string' ? /^data:([a-z/+-]+);base64,([A-Za-z0-9+/]+={0,2})$/i.exec(url) : null
  if (!match) return { file: null, problem: 'data' }
  const type = (match[1] ?? '').toLowerCase()
  const extension = AVATAR_TYPES[type]
  if (!extension) return { file: null, problem: 'type' }
  const data = match[2] ?? ''
  // Decoded size, before decoding: three bytes per four characters, minus the padding.
  if (Math.floor((data.length * 3) / 4) - (data.match(/=+$/)?.[0].length ?? 0) > MAX_AVATAR_BYTES) return { file: null, problem: 'size' }
  try {
    const bytes = Uint8Array.from(atob(data), c => c.charCodeAt(0))
    return { file: new File([bytes], `${name}.${extension}`, { type }) }
  } catch {
    return { file: null, problem: 'data' }
  }
}

/** The key under bots.intents of a manifest intent. */
export const INTENT_KEYS: Readonly<Record<BotManifestIntent['name'], string>> = {
  GUILDS: 'guilds',
  GUILD_MEMBERS: 'members',
  GUILD_MESSAGES: 'messages',
  GUILD_MESSAGE_REACTIONS: 'reactions'
}

export interface ChannelGroup { id: string; name: string; channels: Channel[] }

/**
 * The channels a bot can work in, grouped by category in the sidebar's order:
 * text and forum channels the installer can see (announcement channels are
 * text channels), never Talks. Channels without a category come first.
 */
export function installableChannels(categories: readonly Category[], uncategorized: readonly Channel[]): ChannelGroup[] {
  const usable = (list: readonly Channel[]) => [...list].filter(c => c.type !== 'voice').sort((a, b) => a.sort_order - b.sort_order)
  const groups: ChannelGroup[] = []
  const loose = usable(uncategorized)
  if (loose.length) groups.push({ id: '', name: '', channels: loose })
  for (const category of [...categories].sort((a, b) => a.sort_order - b.sort_order)) {
    const channels = usable(category.channels)
    if (channels.length) groups.push({ id: category.id, name: category.name, channels })
  }
  return groups
}

/**
 * Requested permissions that ping everyone or moderate others start unticked:
 * the installer turns them on knowingly. The others start ticked.
 */
export const OFF_AT_FIRST: ReadonlySet<string> = new Set(['mention_everyone', 'manage_messages'])

/** The permissions ticked when the confirm screen opens. */
export function initialPermissions(requested: readonly { key: string }[]): string[] {
  return requested.map(p => p.key).filter(key => !OFF_AT_FIRST.has(key))
}

// The community's built-in slash commands (internal/slashcmd). A bot command of
// the same name does not replace one: the composer lists both.
const BUILTIN_COMMANDS: ReadonlySet<string> = new Set(['poll', 'me', 'msg', 'shrug', 'tableflip', 'unflip', 'spoiler', 'nick', 'thread', 'timeout', 'ban', 'kick'])

/** Whether a command of the manifest has the name of a built-in one. */
export function sharesBuiltinName(name: string): boolean {
  return BUILTIN_COMMANDS.has(name.toLowerCase())
}

/** The host of a homepage, shown next to the link so it reads as another site. */
export function homepageHost(url: string): string {
  try {
    return new URL(url).host
  } catch {
    return ''
  }
}

/** An https homepage that can be shown as a link, or ''. */
export function safeHomepage(url: string | undefined): string {
  if (!url) return ''
  try {
    return new URL(url).protocol === 'https:' ? url : ''
  } catch {
    return ''
  }
}
