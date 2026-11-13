// The events of bot interactions for the member who caused them
// (internal/bot): ephemeral notes, modals and deferrals. Runtime checks by
// event name.
import type { ChatActionRow } from './rest'
import { isChatActionRow } from './rest'
import { hasOwn, isArrayOf, isIdentifier, isRecord, isString, isTimestamp } from './validation'

/** A bot's reply that only this member sees: a note in the channel. */
export interface InteractionResponse {
  /** The note; a later response with the same id replaces it, one with deleted removes it. Older servers send none. */
  id?: string
  channel_id: string
  bot_id: string
  bot_name: string
  bot_avatar_url?: string
  content: string
  embeds: { title?: string; description?: string }[] | null
  components?: ChatActionRow[]
  deleted?: boolean
  created_at: string
}

/** A form a bot opened for this member (callback type 9); submit with POST /api/channels/{channel_id}/modals/{id}. */
export interface InteractionModal {
  id: string
  channel_id: string
  bot_id: string
  bot_name: string
  bot_avatar_url: string
  title: string
  components: ChatActionRow[]
}

/** The bot acknowledged this member's command or click (callback 5 or 6); its answer follows. */
export interface InteractionDeferred {
  interaction_id: string
  channel_id: string
  bot_id: string
}

function isOptionalAvatarUrl(payload: Record<string, unknown>): boolean {
  return !hasOwn(payload, 'bot_avatar_url') || isString(payload['bot_avatar_url'], 512)
}

function isOptionalNoteEmbeds(embeds: unknown): boolean {
  return embeds === null || isArrayOf(embeds, isNoteEmbed)
}

function isInteractionResponse(payload: Record<string, unknown>): boolean {
  if (!isIdentifier(payload['channel_id']) || !isIdentifier(payload['bot_id'])) return false
  if (!isString(payload['bot_name'], 64) || !isString(payload['content'], 8_000)) return false
  if (!isOptionalAvatarUrl(payload) || !isOptionalNoteEmbeds(payload['embeds'])) return false
  if (hasOwn(payload, 'id') && !isIdentifier(payload['id'])) return false
  if (hasOwn(payload, 'components') && !isArrayOf(payload['components'], isChatActionRow)) return false
  if (hasOwn(payload, 'deleted') && typeof payload['deleted'] !== 'boolean') return false
  return isTimestamp(payload['created_at'])
}

function isInteractionModal(payload: Record<string, unknown>): boolean {
  if (!isIdentifier(payload['id']) || !isIdentifier(payload['channel_id']) || !isIdentifier(payload['bot_id'])) return false
  if (!isString(payload['bot_name'], 64) || !isString(payload['title'], 45) || !isOptionalAvatarUrl(payload)) return false
  return isArrayOf(payload['components'], isChatActionRow)
}

function isNoteEmbed(value: unknown): boolean {
  return isRecord(value) && (!hasOwn(value, 'title') || isString(value['title'], 1_000)) && (!hasOwn(value, 'description') || isString(value['description'], 8_000))
}

export const BOT_EVENT_CHECKS: [string, (payload: Record<string, unknown>) => boolean][] = [
  ['interaction_response', isInteractionResponse],
  ['interaction_modal', isInteractionModal],
  ['interaction_deferred', payload => isIdentifier(payload['interaction_id']) && isIdentifier(payload['channel_id']) && isIdentifier(payload['bot_id'])],
]
