import type { Message, User } from '../../types/domain'
import type { InteractionResponse } from '../../types/events'

export type ThreadRoot = Pick<Message, 'id'> & Partial<Omit<Message, 'id'>>
export type ProfilePreview = Pick<User, 'id' | 'username' | 'display_name' | 'role'> & Partial<Omit<User, 'id' | 'username' | 'display_name' | 'role'>>
export interface TypingUser { user_id: string; username: string; display_name: string }
export interface MessageWindow { messages: Message[]; hasMoreBefore: boolean; hasMoreAfter: boolean }
export type Timer = ReturnType<typeof setTimeout>

/** A private reply of a bot to a slash command, shown in its channel until dismissed. */
export interface EphemeralNote extends InteractionResponse {
  id: number
}

export const MAX_PENDING_LIVE = 200
// A channel is marked read on the server at most this often while messages stream in.
export const MARK_READ_INTERVAL_MS = 2000
// Typing notices expire when the user stops (the server relays one per 3 s).
export const TYPING_EXPIRY_MS = 5000
export const TYPING_THROTTLE_MS = 3000
/** How many private replies are kept. */
export const MAX_EPHEMERAL = 20
