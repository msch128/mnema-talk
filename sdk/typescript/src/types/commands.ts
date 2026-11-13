// Slash commands and interaction responses (doc/bots.md, "Slash commands").
import type { OptionTypeValue } from '../constants.ts'
import type { MessageComponents, ModalData } from './components.ts'
import type { AllowedMentions, Embed } from './messages.ts'

export interface CommandOption {
  name: string
  description: string
  /** 3 string, 4 integer, 5 boolean, 6 user, 7 channel. */
  type: OptionTypeValue
  required?: boolean
  /** Allowed values of a string option: at most 25, each 1 to 100 characters. */
  choices?: string[]
}

/** What a bot registers (PUT and POST /commands). */
export interface CommandInput {
  name: string
  description: string
  options?: CommandOption[]
}

/** A registered command (GET /commands). */
export interface Command {
  id: string
  bot_id: string
  name: string
  description: string
  options: CommandOption[]
  disabled: boolean
  created_at: string
}

/** The `data` of callback 4 and the body of a follow-up: a message to post. */
export interface InteractionResponseData {
  content?: string
  embeds?: Embed[]
  components?: MessageComponents
  /** Narrows whom the message notifies, as on a post. */
  allowed_mentions?: AllowedMentions
  /** 64: ephemeral. */
  flags?: number
}

/**
 * The `data` of callback 7 and the body of an @original edit: absent fields
 * stay. No flags: the first response decides whether an answer is ephemeral,
 * and the server refuses flags here.
 */
export interface InteractionEditData {
  content?: string
  embeds?: Embed[]
  components?: MessageComponents
  /** Narrows whom the new text notifies, as on a post. */
  allowed_mentions?: AllowedMentions
}

export type InteractionCallback =
  | { type: 4; data: InteractionResponseData }
  | { type: 5; data?: { flags?: number } }
  | { type: 6 }
  | { type: 7; data: InteractionEditData }
  | { type: 9; data: ModalData }
