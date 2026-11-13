// An INTERACTION_CREATE (a slash command, a click or a modal submit) and the
// ways to answer it (doc/bots.md, "Interactions in detail").
import { CallbackType, InteractionType, MessageFlags } from '../constants.ts'
import { validateModal } from '../builders/validate.ts'
import type { RestClient } from '../rest/client.ts'
import type { InteractionEditData, InteractionResponseData } from '../types/commands.ts'
import type { ModalData } from '../types/components.ts'
import type {
  InteractionMessage,
  InteractionPayload,
  InteractionUser,
  ResolvedData,
} from '../types/gateway.ts'
import type { Message } from '../types/messages.ts'

/** What reply and followUp take: a string is the content. */
export type ReplyData = string | (InteractionResponseData & { ephemeral?: boolean })

/**
 * What editReply and update take: a string is the content. No ephemeral or
 * flags: the first response decides whether an answer is ephemeral.
 */
export type EditData = string | InteractionEditData

function toData(data: ReplyData): InteractionResponseData {
  if (typeof data === 'string') return { content: data }
  const { ephemeral, ...rest } = data
  if (ephemeral) rest.flags = (rest.flags ?? 0) | MessageFlags.Ephemeral
  return rest
}

function toEdit(data: EditData, what: string): InteractionEditData {
  if (typeof data === 'string') return { content: data }
  if ('ephemeral' in data || 'flags' in data) {
    throw new TypeError(`${what}: ephemeral and flags belong to the first response (reply or deferReply), not to an edit`)
  }
  return data
}

const SELECT_TYPES = new Set([3, 5, 6, 8])

export class Interaction {
  readonly id: string
  readonly token: string
  readonly type: 2 | 3 | 5
  readonly channelId: string
  readonly applicationId: string
  /** The member who ran the command, clicked or submitted; the server sends it with every interaction. */
  readonly user: InteractionUser
  /** The message a component is on (also for a modal opened from a component). */
  readonly message: InteractionMessage | undefined
  /** The command's name (type 2), else ''. */
  readonly commandName: string
  /** The command's id (type 2). */
  readonly commandId: string | undefined
  /** The command's options by name (type 2). */
  readonly options: Map<string, string | number | boolean>
  /** The custom_id of the component or modal (types 3 and 5), else ''. */
  readonly customId: string
  /** The component's type (type 3). */
  readonly componentType: number | undefined
  /** The picked values of a select (type 3), else []. */
  readonly values: string[]
  /** What user, role and channel selects picked. */
  readonly resolved: ResolvedData | undefined
  /** The text inputs of a modal submit by custom_id (type 5). */
  readonly fields: Map<string, string>
  /** The payload as it arrived. */
  readonly raw: InteractionPayload

  /** Whether the first answer (reply, defer, update, modal) was sent. */
  replied = false
  /** Whether the first answer was a deferral (deferReply or deferUpdate). */
  deferred = false

  private readonly rest: RestClient

  constructor(payload: InteractionPayload, rest: RestClient) {
    const d = payload.data ?? {}
    this.raw = payload
    this.rest = rest
    this.id = payload.id
    this.token = payload.token
    this.type = payload.type
    this.channelId = payload.channel_id
    this.applicationId = payload.application_id
    const user = payload.member?.user
    if (!user) throw new TypeError('an interaction without member.user')
    this.user = user
    this.message = payload.message
    this.commandName = d.name ?? ''
    this.commandId = d.id
    this.options = new Map((d.options ?? []).map((o) => [o.name, o.value]))
    this.customId = d.custom_id ?? ''
    this.componentType = d.component_type
    this.values = d.values ?? []
    this.resolved = d.resolved
    this.fields = new Map((d.components ?? []).flatMap((row) => row.components.map((c) => [c.custom_id, c.value ?? ''] as const)))
  }

  isCommand(): boolean {
    return this.type === InteractionType.ApplicationCommand
  }

  isComponent(): boolean {
    return this.type === InteractionType.MessageComponent
  }

  isButton(): boolean {
    return this.isComponent() && this.componentType === 2
  }

  isSelect(): boolean {
    return this.isComponent() && SELECT_TYPES.has(this.componentType ?? 0)
  }

  isModalSubmit(): boolean {
    return this.type === InteractionType.ModalSubmit
  }

  /** Posts the response message (callback 4); `ephemeral` shows it to the member only. */
  async reply(data: ReplyData): Promise<void> {
    await this.callback({ type: CallbackType.ChannelMessageWithSource, data: toData(data) })
  }

  /** Acknowledges now (callback 5); the answer follows with editReply or followUp. */
  async deferReply(options: { ephemeral?: boolean } = {}): Promise<void> {
    const data = options.ephemeral ? { flags: MessageFlags.Ephemeral } : undefined
    await this.callback(data ? { type: CallbackType.DeferredChannelMessageWithSource, data } : { type: CallbackType.DeferredChannelMessageWithSource })
    this.deferred = true
  }

  /** Edits the original response, or posts it after deferReply; undefined for an ephemeral note. */
  async editReply(data: EditData): Promise<Message | undefined> {
    return this.rest.editOriginalResponse(this.id, this.token, toEdit(data, 'editReply'))
  }

  deleteReply(): Promise<void> {
    return this.rest.deleteOriginalResponse(this.id, this.token)
  }

  /** A further message (up to 5 responses in total); undefined for an ephemeral one. */
  followUp(data: ReplyData): Promise<Message | undefined> {
    return this.rest.createFollowup(this.id, this.token, toData(data))
  }

  /** Updates the message the component is on (callback 7); absent fields stay. */
  async update(data: EditData): Promise<void> {
    this.requireComponentOrigin('update')
    await this.callback({ type: CallbackType.UpdateMessage, data: toEdit(data, 'update') })
  }

  /** Acknowledges a click without changing anything yet (callback 6). */
  async deferUpdate(): Promise<void> {
    this.requireComponentOrigin('deferUpdate')
    await this.callback({ type: CallbackType.DeferredUpdateMessage })
    this.deferred = true
  }

  /** Opens a modal (callback 9); not allowed in answer to a modal submit. */
  async showModal(modal: ModalData): Promise<void> {
    if (this.isModalSubmit()) throw new Error('showModal: a modal submit cannot open another modal')
    validateModal(modal)
    await this.callback({ type: CallbackType.Modal, data: modal })
  }

  private requireComponentOrigin(what: string): void {
    const fromComponent = this.isComponent() || (this.isModalSubmit() && this.message !== undefined)
    if (!fromComponent) throw new Error(`${what}: only for a component interaction or a modal submit opened from one`)
  }

  private async callback(cb: Parameters<RestClient['createInteractionResponse']>[2]): Promise<void> {
    if (this.replied) throw new Error('this interaction was already answered; use editReply or followUp')
    await this.rest.createInteractionResponse(this.id, this.token, cb)
    this.replied = true
  }
}
