import type { ApiReply, ApiRequest } from '../api'
import { NativeChatView, nativeReceiptId, type NativeTextRow } from './nativeChatView'

const uuid = '([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})'
const timeline = new RegExp(`^/api/channels/${uuid}/messages(?:\\?([^#]*))?$`)
const mutation = new RegExp(`^/api/channels/${uuid}/messages/${uuid}$`)
const threadPath = new RegExp(`^/api/messages/${uuid}/thread$`)
const reactionPath = new RegExp(`^/api/messages/${uuid}/reactions$`)
const LISTING_KEYS = ['limit', 'before', 'after', 'around']
const ANCHORS = ['before', 'after', 'around']
const MESSAGE_KEYS = ['content', 'parent_id', 'reply_to_id']

export type TimelineRoute = { kind: 'timeline'; channelId: string; query: string | undefined }
export type MessageRoute = { kind: 'message'; channelId: string; target: string }
export type ThreadRoute = { kind: 'thread'; target: string }
export type ReactionRoute = { kind: 'reaction'; target: string }
export type ChatRoute = TimelineRoute | MessageRoute | ThreadRoute | ReactionRoute
export type TargetRoute = MessageRoute | ThreadRoute | ReactionRoute

type ChatRows = ReturnType<NativeChatView['messages']>
type Selected = NonNullable<ReturnType<NativeChatView['lookup']>>
interface MessageAction {
  request: ApiRequest
  edit: boolean
  reaction: boolean
  target: string
  selected: Selected
}
interface ReceiptCheck {
  batch: unknown
  channelId: string
  input: Record<string, unknown> | null
  affected: unknown
  event: string | null
}

function object(value: unknown): value is Record<string, unknown> {
  if (typeof value !== 'object' || value === null) return false
  return !Array.isArray(value)
}

function capture(match: RegExpExecArray, index: number): string {
  return match[index] ?? ''
}

export function chatRoute(path: string): ChatRoute | null {
  const listing = timeline.exec(path)
  if (listing) return { kind: 'timeline', channelId: capture(listing, 1), query: listing[2] }
  return targetRoute(path)
}

function targetRoute(path: string): ChatRoute | null {
  const edit = mutation.exec(path)
  if (edit) return { kind: 'message', channelId: capture(edit, 1), target: capture(edit, 2) }
  const replies = threadPath.exec(path)
  if (replies) return { kind: 'thread', target: capture(replies, 1) }
  const reactions = reactionPath.exec(path)
  if (!reactions) return null
  return { kind: 'reaction', target: capture(reactions, 1) }
}

function unknownListingKey(params: URLSearchParams): boolean {
  return [...params.keys()].some(key => !LISTING_KEYS.includes(key))
}

function repeatedListingKey(params: URLSearchParams): boolean {
  return [...params.keys()].some(key => params.getAll(key).length !== 1)
}

function rejectedLimit(params: URLSearchParams): boolean {
  if (!params.has('limit')) return false
  return params.get('limit') !== '50'
}

function anchorCount(params: URLSearchParams): number {
  let count = 0
  for (const key of ANCHORS) if (params.has(key)) count += 1
  return count
}

export function listingParamsAllowed(params: URLSearchParams): boolean {
  if (unknownListingKey(params) || repeatedListingKey(params)) return false
  return !rejectedLimit(params) && !tooManyAnchors(params)
}

function tooManyAnchors(params: URLSearchParams): boolean {
  return anchorCount(params) > 1
}

export function badAnchor(params: URLSearchParams): boolean {
  for (const key of ANCHORS) {
    if (!params.has(key)) continue
    if (!nativeReceiptId(params.get(key))) return true
  }
  return false
}

function visibleAnchor(anchor: NativeTextRow | undefined): anchor is NativeTextRow {
  if (!anchor || anchor.deleted) return false
  return !anchor.parent_id
}

function sliceAround(rows: ChatRows, id: string): ChatRows {
  const index = rows.findIndex(row => row.id === id)
  return rows.slice(Math.max(0, index - 25), index + 26)
}

function rowsForAnchor(rows: ChatRows, kind: string, anchor: NativeTextRow, id: string): ChatRows {
  if (kind === 'before') return rows.filter(row => row.number < anchor.number).slice(-50)
  if (kind === 'after') return rows.filter(row => row.number > anchor.number).slice(0, 50)
  return sliceAround(rows, id)
}

function applyAnchor(view: NativeChatView, rows: ChatRows, kind: string, params: URLSearchParams): ChatRows | null {
  const id = params.get(kind)
  if (!id) return rows
  const anchor = view.lookup(id)?.row
  if (!visibleAnchor(anchor)) return null
  return rowsForAnchor(rows, kind, anchor, id)
}

function unanchored(params: URLSearchParams): boolean {
  return anchorCount(params) === 0
}

export function pageOf(view: NativeChatView, all: ChatRows, params: URLSearchParams): ChatRows | null {
  let rows = all
  for (const kind of ANCHORS) {
    const next = applyAnchor(view, rows, kind, params)
    if (next === null) return null
    rows = next
  }
  if (!unanchored(params)) return rows
  return rows.slice(-50)
}

function knownMessageFields(input: Record<string, unknown>): boolean {
  return Object.keys(input).every(key => MESSAGE_KEYS.includes(key))
}

function boundedText(content: string): boolean {
  if (!content.trim()) return false
  return content.length <= 4000
}

function messageContent(input: Record<string, unknown>): string | null {
  const content = input['content']
  if (typeof content !== 'string') return null
  if (!boundedText(content)) return null
  return content
}

function presentReceipt(input: Record<string, unknown>, key: string): boolean {
  const value = input[key]
  if (value === undefined) return true
  return nativeReceiptId(value)
}

function receiptsAccepted(input: Record<string, unknown>): boolean {
  if (!presentReceipt(input, 'parent_id')) return false
  return presentReceipt(input, 'reply_to_id')
}

function replyKind(input: Record<string, unknown>): boolean {
  if (input['parent_id']) return true
  return Boolean(input['reply_to_id'])
}

function withClaim(body: Record<string, unknown>, key: string, value: unknown): Record<string, unknown> {
  if (!value) return body
  return { ...body, [key]: value }
}

function typedNewMessage(input: Record<string, unknown>, content: string): Record<string, unknown> {
  const kind = replyKind(input) ? 'reply' : 'create'
  const withParent = withClaim({ kind, body: content }, 'parent_receipt_id', input['parent_id'])
  return withClaim(withParent, 'quoted_receipt_id', input['reply_to_id'])
}

function newMessageInput(input: Record<string, unknown>): Record<string, unknown> | null {
  const content = messageContent(input)
  if (!content || !knownMessageFields(input)) return null
  if (!receiptsAccepted(input)) return null
  return typedNewMessage(input, content)
}

export function postBody(request: ApiRequest, query: string | undefined): Record<string, unknown> | null {
  if (request.method !== 'POST' || query) return null
  if (!object(request.json)) return null
  return newMessageInput(request.json)
}

function soleField(request: ApiRequest, method: string, field: string): unknown {
  if (request.method !== method || !object(request.json)) return null
  if (Object.keys(request.json).length !== 1) return null
  return request.json[field]
}

function editBody(request: ApiRequest): string | null {
  const content = soleField(request, 'PUT', 'content')
  if (typeof content !== 'string') return null
  if (!boundedText(content)) return null
  return content
}

function deleteInput(action: MessageAction): Record<string, unknown> | null {
  if (!action.edit || action.request.method !== 'DELETE') return null
  if (action.request.json !== undefined) return null
  return { kind: 'delete', target_receipt_id: action.target, expected_revision: action.selected.row.revision_id }
}

function editRecord(action: MessageAction): Record<string, unknown> | null {
  if (!action.edit) return null
  const body = editBody(action.request)
  if (!body) return null
  return { kind: 'edit', target_receipt_id: action.target, expected_revision: action.selected.row.revision_id, body }
}

// Native toggling derives the account and ledger. The renderer supplies only an emoji.
function reactionEmoji(request: ApiRequest): string | null {
  const emoji = soleField(request, 'POST', 'emoji')
  if (typeof emoji !== 'string') return null
  return emoji
}

function reactionRecord(action: MessageAction): Record<string, unknown> | null {
  if (!action.reaction) return null
  const emoji = reactionEmoji(action.request)
  if (emoji === null) return null
  return { kind: 'reaction', target_receipt_id: action.target, emoji, action: 'toggle' }
}

export function messageActionInput(action: MessageAction): Record<string, unknown> | null {
  const reaction = reactionRecord(action)
  if (reaction) return reaction
  const removed = deleteInput(action)
  if (removed) return removed
  return editRecord(action)
}

function unavailable(): never {
  throw new Error('Native chat completion unavailable')
}

function completedBody(body: Record<string, unknown>, input: Record<string, unknown> | null): boolean {
  if (body['state'] !== 'completed') return false
  const expected = input ? 2 : 1
  return Object.keys(body).length === expected
}

function acknowledgedId(body: Record<string, unknown>, input: Record<string, unknown> | null): unknown {
  if (!input) return null
  const affected = body['message_id']
  if (nativeReceiptId(affected)) return affected
  throw new Error('Invalid native chat receipt acknowledgement')
}

export function completedMessageId(body: unknown, input: Record<string, unknown> | null, intact: boolean): unknown {
  if (!object(body) || !intact) unavailable()
  if (!completedBody(body, input)) unavailable()
  return acknowledgedId(body, input)
}

function sameClaim(actual: string | null, claimed: unknown): boolean {
  if (claimed === undefined || claimed === null) return actual === null
  return actual === claimed
}

function freshTextMismatch(result: NativeTextRow, input: Record<string, unknown>, event: string | null): boolean {
  if (result.client_event_id !== event) return true
  if (result.body !== input['body']) return true
  if (!sameClaim(result.parent_id, input['parent_receipt_id'])) return true
  return !sameClaim(result.reply_to_id, input['quoted_receipt_id'])
}

function isCreateOrReply(kind: unknown): boolean {
  const name = String(kind)
  if (name === 'create') return true
  return name === 'reply'
}

function editMismatch(result: NativeTextRow, input: Record<string, unknown>): boolean {
  if (input['kind'] !== 'edit') return false
  if (result.deleted) return true
  return result.body !== input['body']
}

function deleteMismatch(result: NativeTextRow, input: Record<string, unknown>): boolean {
  if (input['kind'] !== 'delete') return false
  return !result.deleted
}

function wrongTarget(input: Record<string, unknown>, affected: unknown): boolean {
  const target = input['target_receipt_id']
  if (!target) return false
  return affected !== target
}

function kindMismatch(result: NativeTextRow, input: Record<string, unknown>, event: string | null): boolean {
  if (isCreateOrReply(input['kind']) && freshTextMismatch(result, input, event)) return true
  if (editMismatch(result, input)) return true
  return deleteMismatch(result, input)
}

function receiptMatches(result: NativeTextRow | undefined, affected: unknown, input: Record<string, unknown>, event: string | null): boolean {
  if (!result || wrongTarget(input, affected)) return false
  return !kindMismatch(result, input, event)
}

// A temporary view keeps a bad acknowledgement from mutating the display cache.
export function assertReceipt(check: ReceiptCheck): void {
  const validated = new NativeChatView()
  validated.accept(check.batch, check.channelId)
  if (!check.input) return
  const row = validated.lookup(String(check.affected))?.row
  if (receiptMatches(row, check.affected, check.input, check.event)) return
  throw new Error('Wrong native chat mutation receipt')
}

export function threadBody(rows: ChatRows, target: string | undefined): ApiReply | null {
  const root = rows.find(row => row.id === target)
  if (!root || root.parent_id) return null
  return { status: 200, body: { root, replies: rows.filter(row => row.parent_id === target) } }
}

export function mutationResult(view: NativeChatView, channel: string, target: string, input: Record<string, unknown>): ApiReply {
  if (input['kind'] === 'delete') return { status: 204, body: null }
  const changed = view.messages(channel).find(row => row.id === target)
  if (!changed) throw new Error('Native changed message unavailable')
  if (input['kind'] !== 'reaction') return { status: 200, body: changed }
  return { status: 200, body: { message_id: target, reactions: changed.reactions } }
}
