import { t } from '../../i18n'
import type { ApiReply, ApiRequest } from '../api'
import type { StatusChannel } from '../trustPort'
import { NativeChatView, nativeReceiptId } from './nativeChatView'
import {
  assertReceipt, badAnchor, chatRoute, completedMessageId, listingParamsAllowed, messageActionInput,
  mutationResult, pageOf, postBody, threadBody, type TargetRoute, type TimelineRoute,
} from './nativeChatApiInput'

interface Reply extends ApiReply { context: string }
export interface NativeTypedChatBridge {
  readonly context: string
  isCurrent(): boolean
  call(command: string, args: Record<string, unknown>, signal?: AbortSignal): Promise<Reply>
  channel(): StatusChannel
}

type Selected = NonNullable<ReturnType<NativeChatView['lookup']>>
interface Attempt { fingerprint: string | null; event: string | null }
interface Intake {
  delivered: boolean
  invalid: boolean
  batch: unknown
  arrival: Promise<void>
  receive: (value: unknown) => void
}
interface ChatWork {
  bridge: NativeTypedChatBridge
  channel: StatusChannel
  channelId: string
  input: Record<string, unknown> | null
  signal: AbortSignal | undefined
  intake: Intake
  attempt: Attempt
  generation: number
}
interface PendingWait { done: Promise<void>; cancel: () => void }

function denied(): ApiReply {
  return { status: 503, body: { error: { code: 'UNAVAILABLE', message: t('nativeDesktop.chatAuthorizationRequired') } } }
}
function cancelled(): DOMException {
  return new DOMException('Native chat operation retired', 'AbortError')
}
function retired(bridge: NativeTypedChatBridge, signal: AbortSignal | undefined): boolean {
  if (!bridge.isCurrent()) return true
  return signal?.aborted === true
}
function intact(intake: Intake): boolean {
  return intake.delivered && !intake.invalid
}
function stringId(affected: unknown): string | null {
  if (typeof affected === 'string') return affected
  return null
}
function commandName(input: Record<string, unknown> | null): string {
  if (input) return 'native_chat_mutate'
  return 'native_chat_snapshot'
}
function commandArgs(work: ChatWork): Record<string, unknown> {
  if (!work.input) return { channelId: work.channelId, onMessages: work.channel }
  return { channelId: work.channelId, input: work.input, clientEventId: work.attempt.event, onMessages: work.channel }
}

function takeBatch(intake: Intake, stale: () => boolean, value: unknown, wake: () => void): void {
  if (stale()) return
  if (intake.delivered) intake.invalid = true
  else {
    intake.delivered = true
    intake.batch = value
  }
  wake()
}

function createIntake(stale: () => boolean): Intake {
  let wake: () => void = () => {}
  const arrival = new Promise<void>(resolve => { wake = resolve })
  const intake: Intake = { delivered: false, invalid: false, batch: undefined, arrival, receive: () => {} }
  intake.receive = value => takeBatch(intake, stale, value, wake)
  return intake
}

function clearWait(timer: ReturnType<typeof setTimeout> | undefined): void {
  if (timer !== undefined) clearTimeout(timer)
}

function waitForBatch(arrival: Promise<void>): PendingWait {
  let timer: ReturnType<typeof setTimeout> | undefined
  const done = Promise.race([arrival, new Promise<void>(resolve => { timer = setTimeout(resolve, 5000) })])
  return { done, cancel: () => clearWait(timer) }
}

export function isNativeTypedChatPath(path: string): boolean {
  return chatRoute(path) !== null
}

/** Shared store paths map only to fixed native typed commands.
 * No fetch, plaintext route, credential, source grant, or MLS wire is exposed.
 */
export class NativeTypedChatApi {
  readonly view = new NativeChatView()
  private generation = 0
  private busy = false
  private attempts = new Map<string, string>()

  clear(): void {
    this.generation++
    this.busy = false
    this.attempts.clear()
    this.view.clear()
  }

  private stale(generation: number, bridge: NativeTypedChatBridge, signal: AbortSignal | undefined): boolean {
    if (generation !== this.generation) return true
    return retired(bridge, signal)
  }

  private release(generation: number): void {
    if (generation === this.generation) this.busy = false
  }

  private async request(bridge: NativeTypedChatBridge, channelId: string, input: Record<string, unknown> | null, signal?: AbortSignal): Promise<{ reply: Reply; affected: string | null }> {
    if (retired(bridge, signal)) throw cancelled()
    if (this.busy) throw new Error('Native chat operation already pending')
    const generation = this.generation
    const channel = bridge.channel()
    const intake = createIntake(() => this.stale(generation, bridge, signal))
    const attempt = this.intentFor(channelId, input)
    this.busy = true
    channel.onmessage = intake.receive
    try {
      return await this.finish({ bridge, channel, channelId, input, signal, intake, attempt, generation })
    } finally {
      channel.onmessage = () => {}
      this.release(generation)
    }
  }

  private async finish(work: ChatWork): Promise<{ reply: Reply; affected: string | null }> {
    const reply = await this.callBridge(work)
    if (reply.status !== 200) return { reply, affected: null }
    await this.settle(work)
    const affected = completedMessageId(reply.body, work.input, intact(work.intake))
    this.publish(work, affected)
    this.forget(work.attempt.fingerprint)
    return { reply, affected: stringId(affected) }
  }

  private async callBridge(work: ChatWork): Promise<Reply> {
    const reply = await work.bridge.call(commandName(work.input), commandArgs(work), work.signal)
    if (this.stale(work.generation, work.bridge, work.signal)) throw cancelled()
    return reply
  }

  private async settle(work: ChatWork): Promise<void> {
    if (work.intake.delivered) return
    const wait = waitForBatch(work.intake.arrival)
    await wait.done
    wait.cancel()
    if (this.stale(work.generation, work.bridge, work.signal)) throw cancelled()
  }

  private publish(work: ChatWork, affected: unknown): void {
    assertReceipt({ batch: work.intake.batch, channelId: work.channelId, input: work.input, affected, event: work.attempt.event })
    if (this.stale(work.generation, work.bridge, work.signal)) throw cancelled()
    this.view.accept(work.intake.batch, work.channelId)
  }

  private forget(fingerprint: string | null): void {
    if (fingerprint) this.attempts.delete(fingerprint)
  }

  private intentFor(channelId: string, input: Record<string, unknown> | null): Attempt {
    if (!input) return { fingerprint: null, event: null }
    const fingerprint = JSON.stringify([channelId, input])
    const existing = this.attempts.get(fingerprint)
    const event = existing ?? crypto.randomUUID()
    this.rememberAttempt(fingerprint, event)
    return { fingerprint, event }
  }

  // An explicit retry reuses this public intent. Nothing is replayed automatically.
  private rememberAttempt(fingerprint: string, event: string): void {
    const capped = this.attempts.size >= 64
    if (capped && !this.attempts.has(fingerprint)) throw new Error('Native chat unresolved operations limit')
    this.attempts.set(fingerprint, event)
  }

  private async listMessages(bridge: NativeTypedChatBridge, channelId: string, params: URLSearchParams, signal?: AbortSignal): Promise<ApiReply> {
    const { reply } = await this.request(bridge, channelId, null, signal)
    if (reply.status !== 200) return reply
    const rows = pageOf(this.view, this.view.messages(channelId).filter(row => !row.parent_id), params)
    if (!rows) return { status: 404, body: { error: { code: 'NOT_FOUND' } } }
    return { status: 200, body: rows }
  }

  private async postMessage(bridge: NativeTypedChatBridge, channelId: string, typed: Record<string, unknown>, signal?: AbortSignal): Promise<ApiReply> {
    const { reply, affected } = await this.request(bridge, channelId, typed, signal)
    if (reply.status !== 200) return reply
    const result = this.view.messages(channelId).find(row => row.id === affected)
    if (!result) throw new Error('Native created message unavailable')
    return { status: 201, body: result }
  }

  private async listChannel(bridge: NativeTypedChatBridge, channelId: string, query: string | undefined, signal: AbortSignal | undefined): Promise<ApiReply> {
    const params = new URLSearchParams(query ?? '')
    if (!listingParamsAllowed(params) || badAnchor(params)) return denied()
    return this.listMessages(bridge, channelId, params, signal)
  }

  private async handleChannelMessages(request: ApiRequest, bridge: NativeTypedChatBridge, channelId: string, query: string | undefined): Promise<ApiReply> {
    if (request.method === 'GET' && request.json === undefined) return this.listChannel(bridge, channelId, query, request.signal)
    const typed = postBody(request, query)
    if (!typed) return denied()
    return this.postMessage(bridge, channelId, typed, request.signal)
  }

  private async readThread(bridge: NativeTypedChatBridge, channel: string, target: string, signal?: AbortSignal): Promise<ApiReply> {
    const { reply } = await this.request(bridge, channel, null, signal)
    if (reply.status !== 200) return reply
    return threadBody(this.view.messages(channel), target) ?? denied()
  }

  private async openThread(request: ApiRequest, bridge: NativeTypedChatBridge, channel: string, target: string): Promise<ApiReply> {
    if (request.method !== 'GET' || request.json !== undefined) return denied()
    return this.readThread(bridge, channel, target, request.signal)
  }

  private admittedTarget(route: TargetRoute): Selected | null {
    const selected = this.view.lookup(route.target)
    if (!selected || selected.row.deleted) return null
    if (route.kind !== 'message') return selected
    if (route.channelId === selected.channel) return selected
    return null
  }

  private async openTarget(request: ApiRequest, bridge: NativeTypedChatBridge, route: TargetRoute): Promise<ApiReply> {
    const selected = this.admittedTarget(route)
    if (!selected) return denied()
    if (route.kind === 'thread') return this.openThread(request, bridge, selected.channel, route.target)
    return this.mutateTarget(request, bridge, route, selected)
  }

  private async mutateTarget(request: ApiRequest, bridge: NativeTypedChatBridge, route: Exclude<TargetRoute, { kind: 'thread' }>, selected: Selected): Promise<ApiReply> {
    const input = messageActionInput({ request, edit: route.kind === 'message', reaction: route.kind === 'reaction', target: route.target, selected })
    if (!input) return denied()
    const { reply } = await this.request(bridge, selected.channel, input, request.signal)
    if (reply.status !== 200) return reply
    return mutationResult(this.view, selected.channel, route.target, input)
  }

  private async openTimeline(request: ApiRequest, bridge: NativeTypedChatBridge, route: TimelineRoute): Promise<ApiReply> {
    if (!nativeReceiptId(route.channelId)) return denied()
    return this.handleChannelMessages(request, bridge, route.channelId, route.query)
  }

  async handle(path: string, request: ApiRequest, bridge: NativeTypedChatBridge): Promise<ApiReply | null> {
    const route = chatRoute(path)
    if (!route) return null
    if (request.form) return denied()
    if (route.kind === 'timeline') return this.openTimeline(request, bridge, route)
    return this.openTarget(request, bridge, route)
  }
}
