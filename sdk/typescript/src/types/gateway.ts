// Gateway frames and dispatch payloads (doc/bots.md, "Gateway").
import type { ActivityTypeValue } from '../constants.ts'
import type { MessageComponents, SubmittedTextInput } from './components.ts'
import type { Embed, Message, ReactionSummary } from './messages.ts'

export interface GatewayFrame {
  op: number
  d?: unknown
  s?: number | null
  t?: string | null
}

export interface ReadyUser {
  /** The bot's user id: the author of its messages and the id in `mentions`. */
  id: string
  bot_id: string
  /** The bot's name as members see it (its display name). */
  username: string
  /**
   * The account's unique name, the one members mention (@bot-...); Discord
   * calls this the username. Servers before 3.1 do not send it: recognise a
   * mention of the bot by its id in a message's `mentions` instead.
   */
  handle?: string
  bot: true
  avatar_url?: string
}

export interface Ready {
  v: number
  user: ReadyUser
  session_id: string
  /** The intents granted. */
  intents: number
  resume_gateway_url: string
  heartbeat_interval: number
}

export type PresenceStatus = 'online' | 'idle' | 'dnd' | 'invisible'

export interface Activity {
  name: string
  /** 0 playing, 2 listening, 3 watching, 4 custom, 5 competing. */
  type: ActivityTypeValue
  /** The text of a custom activity (type 4); `name` is used when absent. */
  state?: string
}

export interface PresenceData {
  status: PresenceStatus
  activity?: Activity | null
}

export interface MessageDeleteEvent {
  id: string
  channel_id: string
  parent_id?: string | null
}

export interface ReactionsUpdateEvent {
  message_id: string
  channel_id: string
  reactions: ReactionSummary[]
}

export interface MemberAddEvent {
  id: string
  username: string
  display_name: string
  avatar_url?: string
}

export interface InteractionUser {
  id: string
  username: string
  display_name: string
  avatar_url?: string
}

export interface CommandOptionValue {
  name: string
  type: number
  value: string | number | boolean
}

export interface ResolvedData {
  users?: Record<string, InteractionUser>
  roles?: Record<string, { id: string; name: string; color: number }>
  channels?: Record<string, { id: string; name: string; type: string }>
}

export interface InteractionData {
  /** Command id (type 2). */
  id?: string
  /** Command name (type 2). */
  name?: string
  options?: CommandOptionValue[]
  /** The component's or modal's custom_id (types 3 and 5). */
  custom_id?: string
  component_type?: number
  values?: string[]
  resolved?: ResolvedData
  /** Modal submit rows (type 5). */
  components?: { type: 1; components: SubmittedTextInput[] }[]
}

/** The message a component is on, as INTERACTION_CREATE carries it. */
export type InteractionMessage = Partial<Message> & {
  id: string
  content?: string
  components?: MessageComponents
  embeds?: Embed[]
  flags?: number
}

/** The `d` of an INTERACTION_CREATE dispatch. */
export interface InteractionPayload {
  id: string
  token: string
  type: 2 | 3 | 5
  channel_id: string
  application_id: string
  data: InteractionData
  member: { user: InteractionUser }
  message?: InteractionMessage
}
