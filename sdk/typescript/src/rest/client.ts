// The routes of the bot REST API (/api/bot/v1), typed.
import { validateCommand, validateCommands } from '../builders/commands.ts'
import { validateMessageComponents } from '../builders/validate.ts'
import type { Command, CommandInput, InteractionCallback, InteractionEditData, InteractionResponseData } from '../types/commands.ts'
import type {
  BotSelf,
  ChannelHierarchy,
  CreateMessageData,
  EditMessageData,
  GatewayInfo,
  ListMessagesQuery,
  Member,
  Message,
  UploadFileData,
} from '../types/messages.ts'
import { RestCore, type RequestOptions, type RestOptions } from './core.ts'

const seg = encodeURIComponent

function checkComponents(data: { components?: unknown } | undefined): void {
  if (data?.components !== undefined) validateMessageComponents(data.components as never)
}

export class RestClient {
  private readonly core: RestCore

  constructor(options: RestOptions) {
    this.core = new RestCore(options)
  }

  /** The server address without a trailing slash. */
  get baseUrl(): string {
    return this.core.baseUrl
  }

  /** Any route under /api/bot/v1, with the same buckets and retries. */
  request<T = unknown>(method: string, path: string, options?: RequestOptions): Promise<T> {
    return this.core.request<T>(method, path, options)
  }

  getMe(): Promise<BotSelf> {
    return this.core.request('GET', '/users/@me')
  }

  getGateway(): Promise<GatewayInfo> {
    return this.core.request('GET', '/gateway')
  }

  listChannels(): Promise<ChannelHierarchy> {
    return this.core.request('GET', '/channels')
  }

  listMessages(channelId: string, query: ListMessagesQuery = {}): Promise<Message[]> {
    return this.core.request('GET', `/channels/${seg(channelId)}/messages`, { query: { ...query } })
  }

  getMessage(channelId: string, messageId: string): Promise<Message> {
    return this.core.request('GET', `/channels/${seg(channelId)}/messages/${seg(messageId)}`)
  }

  /** Posts a message; a string is the content. */
  createMessage(channelId: string, data: CreateMessageData | string): Promise<Message> {
    const body = typeof data === 'string' ? { content: data } : data
    checkComponents(body)
    return this.core.request('POST', `/channels/${seg(channelId)}/messages`, { body })
  }

  /** Edits an own message: absent fields stay, `components: []` removes them. */
  editMessage(channelId: string, messageId: string, data: EditMessageData | string): Promise<Message> {
    const body = typeof data === 'string' ? { content: data } : data
    checkComponents(body)
    return this.core.request('PATCH', `/channels/${seg(channelId)}/messages/${seg(messageId)}`, { body })
  }

  deleteMessage(channelId: string, messageId: string): Promise<void> {
    return this.core.request('DELETE', `/channels/${seg(channelId)}/messages/${seg(messageId)}`)
  }

  /** Reacts with a Unicode emoji or a custom one as `<:name:id>`. */
  addReaction(channelId: string, messageId: string, emoji: string): Promise<void> {
    return this.core.request('PUT', `/channels/${seg(channelId)}/messages/${seg(messageId)}/reactions/${seg(emoji)}/@me`)
  }

  removeReaction(channelId: string, messageId: string, emoji: string): Promise<void> {
    return this.core.request('DELETE', `/channels/${seg(channelId)}/messages/${seg(messageId)}/reactions/${seg(emoji)}/@me`)
  }

  /** Uploads a file (multipart `file`), with an optional caption. */
  uploadFile(channelId: string, data: UploadFileData): Promise<Message> {
    const form = new FormData()
    const blob = data.file instanceof Blob ? data.file : new Blob([data.file as Uint8Array<ArrayBuffer>], data.contentType ? { type: data.contentType } : {})
    // The text fields go first: the server reads them before it streams the
    // file to storage, so a bad target is refused before the upload.
    if (data.content !== undefined) form.append('content', data.content)
    if (data.allowed_mentions !== undefined) form.append('allowed_mentions', JSON.stringify(data.allowed_mentions))
    if (data.parent_id !== undefined) form.append('parent_id', data.parent_id)
    if (data.reply_to_id !== undefined) form.append('reply_to_id', data.reply_to_id)
    form.append('file', blob, data.name)
    return this.core.request('POST', `/channels/${seg(channelId)}/upload`, { form })
  }

  /** Needs the GUILD_MEMBERS intent allowed. */
  listMembers(): Promise<Member[]> {
    return this.core.request('GET', '/members')
  }

  getMember(userId: string): Promise<Member> {
    return this.core.request('GET', `/members/${seg(userId)}`)
  }

  listCommands(): Promise<Command[]> {
    return this.core.request('GET', '/commands')
  }

  /** Replaces all commands (bulk overwrite). */
  setCommands(commands: CommandInput[]): Promise<Command[]> {
    validateCommands(commands)
    return this.core.request('PUT', '/commands', { body: commands.map(withOptions) })
  }

  /** Adds a command or updates the one with the same name. */
  createCommand(command: CommandInput): Promise<Command> {
    validateCommand(command)
    return this.core.request('POST', '/commands', { body: withOptions(command) })
  }

  deleteCommand(commandId: string): Promise<void> {
    return this.core.request('DELETE', `/commands/${seg(commandId)}`)
  }

  /** The first answer to an interaction (callback types 4, 5, 6, 7, 9). */
  createInteractionResponse(id: string, token: string, callback: InteractionCallback): Promise<void> {
    if (callback.type === 4 || callback.type === 7) checkComponents(callback.data)
    return this.core.request('POST', `${interactionPath(id, token)}/callback`, { body: callback })
  }

  /** A further message; answers undefined for an ephemeral one. */
  createFollowup(id: string, token: string, data: InteractionResponseData): Promise<Message | undefined> {
    checkComponents(data)
    return this.core.request('POST', `${interactionPath(id, token)}/followups`, { body: data })
  }

  /** Edits (or, after callback 5, posts) the original response; undefined for an ephemeral note. */
  editOriginalResponse(id: string, token: string, data: InteractionEditData): Promise<Message | undefined> {
    checkComponents(data)
    return this.core.request('PATCH', `${interactionPath(id, token)}/messages/@original`, { body: data })
  }

  deleteOriginalResponse(id: string, token: string): Promise<void> {
    return this.core.request('DELETE', `${interactionPath(id, token)}/messages/@original`)
  }
}

function interactionPath(id: string, token: string): string {
  return `/interactions/${seg(id)}/${seg(token)}`
}

function withOptions(c: CommandInput): CommandInput {
  return { ...c, options: c.options ?? [] }
}
