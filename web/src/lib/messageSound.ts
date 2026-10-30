// When a new chat message plays the calm "message" sound. The same rules as the
// OS notification (shouldNotify) decide who is addressed; on top of that the
// sound stays out of the way of whatever the person is already looking at or
// the system is already announcing.
import type { NotifyLevel } from '../types/domain'

/** The member's choice: every message of channels set to "all messages", only what addresses them, or none. */
export type MessageSoundMode = 'all' | 'mentions' | 'off'
export const MESSAGE_SOUND_MODES: readonly MessageSoundMode[] = ['all', 'mentions', 'off']
export const DEFAULT_MESSAGE_SOUND_MODE: MessageSoundMode = 'mentions'

/** Two sounds closer than this count as one: a burst of messages chimes once. */
export const MESSAGE_SOUND_GAP_MS = 2000

export function normalizeMessageSoundMode(value: unknown): MessageSoundMode {
  return MESSAGE_SOUND_MODES.find(mode => mode === value) ?? DEFAULT_MESSAGE_SOUND_MODE
}

export interface MessageSoundContext {
  mode: MessageSoundMode
  isOwn: boolean
  /** Do not disturb or focus. */
  quiet: boolean
  /** The channel's effective level (own setting, category, server default; an active mute included). */
  level: NotifyLevel
  /** A direct message: everything in it is addressed to the member. */
  isDm: boolean
  /** Mentions the member (@name, a role, @all, @here) or replies to them. */
  isMention: boolean
  /** Addressed by name, through a role or as a reply (not by @all/@here): breaks a mute. */
  isDirectMention: boolean
  /** A thread reply of a thread the member neither follows nor is addressed in. */
  threadSilenced: boolean
  /** The open conversation is in front: visible tab and focused window. */
  viewing: boolean
  /** An OS notification was raised for this message; the system plays its own sound. */
  osNotified: boolean
}

export function shouldPlayMessageSound(c: MessageSoundContext): boolean {
  if (c.mode === 'off' || c.isOwn || c.quiet || c.osNotified || c.viewing || c.threadSilenced) return false
  const addressed = c.isDm || c.isMention
  if (c.level === 'mute') return c.isDirectMention
  if (c.level === 'mentions' || c.mode === 'mentions') return addressed
  return true
}
