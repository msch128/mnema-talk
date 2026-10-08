import type { Message, ReactionSummary, ReplyPreview } from '../types/domain'
import { decodeMessage } from '../types/domain'

/** A native typed-event projection, enriched by the same native metadata owner.
 * This closed text schema cannot carry attachment, pin, or server plaintext data.
 * Neither author metadata nor the comparison revision authorizes a mutation.
 */
export interface NativeTextRow {
  id: string; number: number; channel_id: string; client_event_id: string; account_id: string; device_id: string
  body: string; parent_id: string | null; reply_to_id: string | null
  created_at: string; updated_at: string; revision_id: string
  revision_account_id: string; revision_device_id: string
  edited: boolean; deleted: boolean; reactions: Record<string, string[]>; cached_reply_count: number
  author: { id: string; username: string; display_name: string; avatar_url: string }
  attachments: []; is_pinned: false; mentions: []
}
export interface NativeTextSnapshot { channel_id: string; messages: NativeTextRow[] }
const object = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)
export const nativeReceiptId = (v: unknown): v is string => typeof v === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v) && v !== '00000000-0000-0000-0000-000000000000'
const exact = (v: Record<string, unknown>, names: readonly string[]) => Object.keys(v).length === names.length && names.every(k => Object.hasOwn(v, k))
const optionalId = (v: unknown) => v === null || nativeReceiptId(v)
const fail = (): never => { throw new Error('Invalid native typed chat projection') }
function row(value: unknown): NativeTextRow {
  const fields = ['id','number','channel_id','client_event_id','account_id','device_id','body','parent_id','reply_to_id','created_at','updated_at','revision_id','revision_account_id','revision_device_id','edited','deleted','reactions','cached_reply_count','author','attachments','is_pinned','mentions']
  if (!object(value) || !exact(value,fields)) return fail()
  for (const name of ['id','channel_id','client_event_id','account_id','device_id','revision_id','revision_account_id','revision_device_id']) if (!nativeReceiptId(value[name])) return fail()
  if (!Number.isSafeInteger(value['number']) || Number(value['number']) < 1 || !Number.isSafeInteger(value['cached_reply_count']) || Number(value['cached_reply_count']) < 0 || Number(value['cached_reply_count']) > 1024 || !optionalId(value['parent_id']) || !optionalId(value['reply_to_id']) || typeof value['edited'] !== 'boolean' || typeof value['deleted'] !== 'boolean' || typeof value['body'] !== 'string' || typeof value['created_at'] !== 'string' || typeof value['updated_at'] !== 'string') return fail()
  if (!Array.isArray(value['attachments']) || value['attachments'].length !== 0 || value['is_pinned'] !== false || !Array.isArray(value['mentions']) || value['mentions'].length !== 0) return fail()
  const author = value['author']
  if (!object(author) || !exact(author, ['id','username','display_name','avatar_url']) || author['id'] !== value['account_id'] || typeof author['username'] !== 'string' || !author['username'] || typeof author['display_name'] !== 'string' || typeof author['avatar_url'] !== 'string') return fail()
  const reactions = value['reactions']
  if (!object(reactions) || Object.keys(reactions).length > 64) return fail()
  for (const [emoji, users] of Object.entries(reactions)) if (!emoji || new TextEncoder().encode(emoji).length > 128 || !Array.isArray(users) || users.length > 1024 || !users.every(nativeReceiptId) || new Set(users).size !== users.length) return fail()
  // The actual shared decoder validates dates/body bounds and display fields.
  const typed = value as unknown as NativeTextRow
  baseMessage(typed, typed.channel_id)
  return structuredClone(typed)
}
function baseMessage(value: NativeTextRow, channel: string): Message {
  const reactions: ReactionSummary[] = Object.entries(value.reactions).map(([emoji, users]) => ({ emoji, users: [...users], count: users.length }))
  return decodeMessage({
    id:value.id,number:value.number,channel_id:channel,user_id:value.account_id,
    username:value.author.username,display_name:value.author.display_name,
    ...(value.author.avatar_url !== undefined ? {avatar_url:value.author.avatar_url} : {}),
    content:value.body,created_at:value.created_at,updated_at:value.updated_at,is_edited:value.edited,
    // These are absent by definition of this versioned native text domain.
    attachments:value.attachments,is_pinned:value.is_pinned,mentions:value.mentions,reactions,reply_count:value.cached_reply_count,
    ...(value.parent_id ? {parent_id:value.parent_id} : {}),
    ...(value.reply_to_id ? {reply_to_id:value.reply_to_id} : {}),
  })
}
export class NativeChatView {
  private channels = new Map<string, Map<string, NativeTextRow>>()
  clear() { this.channels.clear() }
  accept(value: unknown, expectedChannel: string): Message[] {
    if (!object(value) || !exact(value,['channel_id','messages']) || value['channel_id'] !== expectedChannel || !nativeReceiptId(expectedChannel) || !Array.isArray(value['messages']) || value['messages'].length > 1024 || new TextEncoder().encode(JSON.stringify(value)).length > 512*1024) return fail()
    const next = new Map(this.channels.get(expectedChannel) ?? [])
    const delivered = new Set<string>()
    for (const input of value['messages']) {
      const decoded = row(input)
      if (decoded.channel_id !== expectedChannel || delivered.has(decoded.id) || [...next.values()].some(old => old.id !== decoded.id && old.number === decoded.number)) return fail()
      next.set(decoded.id, decoded); delivered.add(decoded.id)
    }
    if(next.size > 1024)return fail()
    // Validate references against this exact admitted snapshot before replacing
    // anything. Missing native provenance never becomes an invented preview.
    for (const value of next.values()) {
      if (value.parent_id) {
        const root = next.get(value.parent_id)
        if (value.parent_id === value.id || (root && (root.parent_id || (!value.deleted && root.deleted)))) return fail()
      }
      if (value.reply_to_id === value.id) return fail()
    }
    const messages = [...next.values()].filter(v => !v.deleted).sort((a,b) => a.number-b.number).map(value => {
      const message = baseMessage(value, expectedChannel)
      if (value.reply_to_id && next.has(value.reply_to_id)) {
        const quoted = next.get(value.reply_to_id)!
        const preview: ReplyPreview = {id:quoted.id,user_id:quoted.account_id,username:quoted.author.username,display_name:quoted.author.display_name,deleted:quoted.deleted,has_attachments:false,...(quoted.deleted ? {} : {content:quoted.body}),...(quoted.author.avatar_url !== undefined ? {avatar_url:quoted.author.avatar_url} : {})}
        message.reply_to = preview
      }
      return decodeMessage(message)
    })
    // Native owns durable history/cursors. Renderer keeps one current admitted
    // channel snapshot; this is neither MLS recovery nor an old-epoch replay.
    this.channels = new Map([[expectedChannel,next]])
    return messages
  }
  lookup(id: string): {channel: string; row: NativeTextRow} | null {
    for (const [channel,rows] of this.channels) { const value = rows.get(id); if (value) return {channel,row:structuredClone(value)} }
    return null
  }
  messages(channel: string): Message[] {
    const rows = this.channels.get(channel)
    if (!rows) return []
    return this.accept({channel_id:channel,messages:[...rows.values()]},channel)
  }
}
