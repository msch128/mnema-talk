// The bot client: a RestClient and a Gateway, with typed events.
import { API_BASE_PATH } from '../constants.ts'
import { Emitter, toError } from '../emitter.ts'
import { Gateway, type GatewayCloseInfo, type GatewayOptions } from '../gateway/gateway.ts'
import type { RestOptions } from '../rest/core.ts'
import { RestClient } from '../rest/client.ts'
import type {
  InteractionPayload,
  MemberAddEvent,
  MessageDeleteEvent,
  PresenceData,
  ReactionsUpdateEvent,
  Ready,
  ReadyUser,
} from '../types/gateway.ts'
import type { Message } from '../types/messages.ts'
import { trimSlash, type Logger } from '../util.ts'
import { CommandManager } from './commands.ts'
import { Interaction } from './interaction.ts'

export interface ClientOptions {
  /** The server, for example https://talk.example.org. */
  baseUrl: string
  /** The bot token (mnb_...). */
  token: string
  /** The sum of the intent bits (Intents.GuildMessages | ...), or a list of them. */
  intents: number | readonly number[]
  /** The status to start with. */
  presence?: PresenceData
  logger?: Logger
  /** More REST options (timeouts, retries, local limits). */
  rest?: Omit<RestOptions, 'baseUrl' | 'token' | 'logger'>
  /** More gateway options (backoff, a WebSocket factory, a fixed gateway URL). */
  gateway?: Partial<Omit<GatewayOptions, 'token' | 'intents' | 'presence' | 'logger'>>
}

export type ClientEvents = {
  ready: [ready: Ready]
  resumed: []
  messageCreate: [message: Message]
  messageUpdate: [message: Message]
  messageDelete: [event: MessageDeleteEvent]
  reactionsUpdate: [event: ReactionsUpdateEvent]
  channelsUpdate: []
  memberAdd: [member: MemberAddEvent]
  interactionCreate: [interaction: Interaction]
  /** Every dispatch, by its name, also the ones above. */
  raw: [name: string, data: unknown, seq: number]
  error: [error: Error]
  close: [info: GatewayCloseInfo]
  debug: [message: string]
}

/** The gateway address derived from the server's: https becomes wss. */
export function gatewayUrlFor(baseUrl: string): string {
  return trimSlash(baseUrl).replace(/^http/i, 'ws') + API_BASE_PATH + '/gateway'
}

export class Client extends Emitter<ClientEvents> {
  readonly rest: RestClient
  readonly gateway: Gateway
  readonly commands: CommandManager
  /** The bot as READY showed it; undefined before the first READY. */
  user: ReadyUser | undefined

  private readonly logger: Logger | undefined

  constructor(options: ClientOptions) {
    super()
    this.logger = options.logger
    this.rest = new RestClient({ ...options.rest, baseUrl: options.baseUrl, token: options.token, logger: options.logger })
    this.commands = new CommandManager(this.rest)
    const gw: GatewayOptions = {
      url: () => this.findGatewayUrl(options.baseUrl),
      ...options.gateway,
      token: options.token,
      intents: options.intents,
      logger: options.logger,
    }
    if (options.presence) gw.presence = options.presence
    this.gateway = new Gateway(gw)
    this.wire()
  }

  /** Checks the token (GET /users/@me) and connects the gateway; resolves with READY. */
  async connect(): Promise<Ready> {
    await this.rest.getMe()
    return this.gateway.connect()
  }

  /** Closes the gateway; no reconnect, no timer stays. */
  close(): Promise<void> {
    return this.gateway.close()
  }

  /** Sets the bot's status and activity (gateway op 3, at most 5 per minute). */
  setPresence(presence: PresenceData): void {
    this.gateway.setPresence(presence)
  }

  protected override onListenerError(err: unknown, event: string): void {
    if (event !== 'error' && this.listenerCount('error') > 0) this.emit('error', toError(err))
    else if (this.logger?.error) this.logger.error(`a listener of "${event}" failed: ${toError(err).message}`, err)
    else super.onListenerError(err, event)
  }

  private async findGatewayUrl(baseUrl: string): Promise<string> {
    try {
      return (await this.rest.getGateway()).url
    } catch (err) {
      this.emit('debug', `GET /gateway failed (${toError(err).message}); deriving the address`)
      return gatewayUrlFor(baseUrl)
    }
  }

  private wire(): void {
    const g = this.gateway
    g.on('ready', (r) => {
      this.user = r.user
      this.emit('ready', r)
    })
    g.on('resumed', () => this.emit('resumed'))
    g.on('close', (info) => this.emit('close', info))
    g.on('debug', (m) => this.emit('debug', m))
    g.on('error', (e) => {
      if (!this.emit('error', e)) this.onListenerError(e, 'error')
    })
    g.on('dispatch', (name, data, seq) => this.onDispatch(name, data, seq))
  }

  private onDispatch(name: string, data: unknown, seq: number): void {
    this.emit('raw', name, data, seq)
    switch (name) {
      case 'MESSAGE_CREATE':
        return void this.emit('messageCreate', data as Message)
      case 'MESSAGE_UPDATE':
        return void this.emit('messageUpdate', data as Message)
      case 'MESSAGE_DELETE':
        return void this.emit('messageDelete', data as MessageDeleteEvent)
      case 'MESSAGE_REACTIONS_UPDATE':
        return void this.emit('reactionsUpdate', data as ReactionsUpdateEvent)
      case 'CHANNELS_UPDATE':
        return void this.emit('channelsUpdate')
      case 'GUILD_MEMBER_ADD':
        return void this.emit('memberAdd', data as MemberAddEvent)
      case 'INTERACTION_CREATE':
        return void this.emit('interactionCreate', new Interaction(data as InteractionPayload, this.rest))
    }
  }
}
