// REST objects of the bot API (api/openapi.json, tag "Bot API").
import type { MessageComponents } from './components.ts'

/** Ids are UUIDs. */
export type Snowflake = string

export interface EmbedAuthor {
  name: string
  url?: string
}
export interface EmbedField {
  name: string
  value: string
  inline?: boolean
}
export interface EmbedFooter {
  text: string
}
export interface EmbedMedia {
  url: string
}

export interface Embed {
  title?: string
  description?: string
  url?: string
  /** 0 to 0xFFFFFF. */
  color?: number
  timestamp?: string
  author?: EmbedAuthor
  footer?: EmbedFooter
  image?: EmbedMedia
  thumbnail?: EmbedMedia
  fields?: EmbedField[]
}

export interface Attachment {
  id: string
  url: string
  mime_type: string
  original_filename: string
  size_bytes: number
  is_deleted: boolean
}

export interface ReactionSummary {
  emoji: string
  count: number
  users: string[]
}

export interface UserRef {
  id: string
  username: string
  display_name: string
  avatar_url?: string
}

export interface ReplyPreview {
  id: string
  deleted: boolean
  user_id?: string
  username?: string
  display_name?: string
  avatar_url?: string
  content?: string
  has_attachments?: boolean
  is_bot?: boolean
  is_webhook?: boolean
}

export interface PollOption {
  id: string
  label: string
  votes: number
  voters?: string[]
}

export interface Poll {
  options: PollOption[]
  multiple: boolean
  anonymous: boolean
  closes_at: string
  closed: boolean
  total_voters: number
}

export interface Message {
  id: string
  channel_id: string
  user_id: string
  username: string
  display_name: string
  avatar_url?: string
  content: string
  created_at: string
  updated_at: string
  number: number
  is_edited: boolean
  is_pinned: boolean
  is_bot?: boolean
  is_webhook?: boolean
  is_locked?: boolean
  is_resolved?: boolean
  attachments: Attachment[]
  embeds?: Embed[]
  components?: MessageComponents
  reactions: ReactionSummary[]
  mentions: string[]
  direct_mentions?: string[]
  mention_roles?: string[]
  /** The sender's nonce, in MESSAGE_CREATE when the sender sent one. */
  nonce?: string
  parent_id?: string
  reply_count: number
  reply_to_id?: string
  reply_to?: ReplyPreview
  forward?: { forwarded: boolean }
  poll?: Poll
  pinned_at?: string
  pinned_by?: UserRef
  last_activity_at?: string
  title?: string
  tags?: unknown[]
  /** 64 on an ephemeral note (in a component interaction's message). */
  flags?: number
}

export type ChannelType = 'text' | 'voice' | 'forum' | (string & {})

export interface Channel {
  id: string
  name: string
  type: ChannelType
  category_id: string | null
  created_at: string
  number: number
  slug?: string
  topic: string
  sort_order: number
  slow_mode_seconds: number
  user_limit: number
  auto_delete_days?: number
  previous_slugs?: string[]
  is_lobby?: boolean
  is_stage?: boolean
  is_temp?: boolean
  leader_id?: string
}

export interface Category {
  id: string
  name: string
  sort_order: number
  created_at: string
  channels: Channel[]
}

/** GET /channels: the channels the bot can view, with their categories. */
export interface ChannelHierarchy {
  categories: Category[]
  uncategorized: Channel[]
}

export interface Member {
  id: string
  username: string
  display_name: string
  avatar_url?: string
  bot?: boolean
}

/** GET /users/@me */
export interface BotSelf {
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
  allowed_intents: number
}

/** GET /gateway */
export interface GatewayInfo {
  url: string
  heartbeat_interval: number
  max_connections: number
}

export interface AllowedMentions {
  /** "users", "everyone", "roles". */
  parse?: ('users' | 'everyone' | 'roles')[]
  users?: string[]
  roles?: string[]
}

export interface MessageReference {
  message_id: string
}

/** Body of POST /channels/{id}/messages. */
export interface CreateMessageData {
  content?: string
  embeds?: Embed[]
  components?: MessageComponents
  allowed_mentions?: AllowedMentions
  message_reference?: MessageReference
  parent_id?: string
}

/** Body of PATCH /channels/{id}/messages/{id}: absent fields stay. */
export interface EditMessageData {
  content?: string
  /** `[]` removes the components. */
  components?: MessageComponents
  /** Whom the new text notifies, as on a post; an edit with it counts as a post for the channel's limit. */
  allowed_mentions?: AllowedMentions
}

export interface ListMessagesQuery {
  /** 1 to 100, default 50. */
  limit?: number
  before?: string
  after?: string
  around?: string
}

export interface UploadFileData {
  file: Blob | Uint8Array
  /** File name, sent as the multipart file name. */
  name: string
  /** MIME type when `file` is a Uint8Array/Buffer; the server sniffs it anyway. */
  contentType?: string
  content?: string
  allowed_mentions?: AllowedMentions
  parent_id?: string
  reply_to_id?: string
}
