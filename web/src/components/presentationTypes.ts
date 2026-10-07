/** Minimal display contracts: these components do not require account secrets. */
export interface AvatarUser {
  display_name?: string | undefined
  username?: string | undefined
  avatar_url?: string | undefined
}

export type LivePresence = 'online' | 'away' | 'dnd' | 'focus' | 'offline'
export type AvatarSize = 'xxs' | 'xs' | 'sm' | 'md' | 'lg' | 'xl'
export type DropIndicator = '' | 'top' | 'bottom' | 'inside'

export type { ResizePanel } from '../composables/useResizable'

/** A local member may appear before its voice-state announcement arrives. */
export type { TalkParticipant } from '../composables/useTalkStage'
