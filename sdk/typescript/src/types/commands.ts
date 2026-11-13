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

/** The `data` of callback types 4 and 7, follow-ups and @original edits. */
export interface InteractionResponseData {
  content?: string
  embeds?: Embed[]
  components?: MessageComponents
  allowed_mentions?: AllowedMentions
  /** 64: ephemeral. */
  flags?: number
}

export type InteractionCallback =
  | { type: 4; data: InteractionResponseData }
  | { type: 5; data?: { flags?: number } }
  | { type: 6 }
  | { type: 7; data: InteractionResponseData }
  | { type: 9; data: ModalData }
