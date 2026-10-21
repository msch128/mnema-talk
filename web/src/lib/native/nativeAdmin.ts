// Closed browser-to-native input conversion. Native current server authorization
// and document/session publication checks remain mandatory in the actor.
export interface UiAdminRequest { readonly method: string; readonly json?: unknown; readonly form?: unknown }
interface CategoryOrder { id: string; sort_order: number }
interface ChannelPlacement extends CategoryOrder { category_id: string | null }
export type AdminOperation =
  | { operation: 'invites' | 'users' | 'system' | 'system_update' }
  | { operation: 'delete_invite' | 'disable_user' | 'enable_user' | 'revoke_user_sessions' | 'kick_user' | 'delete_category' | 'delete_channel' | 'duplicate_channel'; id: string }
  | { operation: 'create_invite'; code: string; max_uses: number | null; expires_in_hours: number | null }
  | { operation: 'user_status'; id: string; status_text: string }
  | { operation: 'create_category'; name: string; sort_order: number }
  | { operation: 'rename_category'; id: string; name: string }
  | { operation: 'create_channel'; category_id: string | null; name: string; kind: 'text' | 'voice'; topic: string; sort_order: number }
  | { operation: 'update_channel'; id: string; name: string | null; topic: string | null; user_limit: number | null }
  | { operation: 'layout'; categories: CategoryOrder[]; channels: ChannelPlacement[] }
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
function id(v: unknown): v is string { return typeof v === 'string' && UUID.test(v) && v !== '00000000-0000-0000-0000-000000000000' }
function record(v: unknown): v is Record<string, unknown> { return typeof v === 'object' && v !== null && !Array.isArray(v) }
function keys(v: Record<string, unknown>, allowed: string[], required: string[] = []) { return Object.keys(v).every(k => allowed.includes(k)) && required.every(k => Object.hasOwn(v, k)) }
function text(v: unknown, max: number, required = false): v is string { return typeof v === 'string' && !v.includes('\0') && Array.from(v.trim()).length <= max && (!required || v.trim().length > 0) }
function integer(v: unknown, min: number, max: number): v is number { return typeof v === 'number' && Number.isInteger(v) && v >= min && v <= max }
function order(v: unknown): v is number { return integer(v, -2147483648, 2147483647) }
const empty = (r: UiAdminRequest) => r.json === undefined
type Resource = 'invites' | 'users' | 'categories' | 'channels'

const LIST_OPERATIONS: Readonly<Record<string, 'invites' | 'users' | 'system' | 'system_update'>> = { '/api/admin/invites': 'invites', '/api/admin/users': 'users', '/api/admin/system': 'system', '/api/admin/system/update': 'system_update' }
const DELETE_OPERATIONS: Readonly<Record<string, 'delete_invite' | 'delete_category' | 'delete_channel'>> = { invites: 'delete_invite', categories: 'delete_category', channels: 'delete_channel' }
const USER_OPERATIONS: Readonly<Record<string, 'disable_user' | 'enable_user' | 'revoke_user_sessions' | 'kick_user'>> = { disable: 'disable_user', enable: 'enable_user', 'sessions/revoke': 'revoke_user_sessions', kick: 'kick_user' }

/** GET of a fixed admin listing. */
function listOperation(path: string, request: UiAdminRequest): AdminOperation | null {
  if (request.method !== 'GET' || !empty(request)) return null
  const operation = Object.hasOwn(LIST_OPERATIONS, path) ? LIST_OPERATIONS[path] : undefined
  return operation ? { operation } : null
}

function deleteTarget(request: UiAdminRequest, resource: Resource, suffix: string | undefined, targetId: string): AdminOperation | null {
  if (request.method !== 'DELETE' || suffix !== undefined || !empty(request)) return null
  const operation = DELETE_OPERATIONS[resource]
  return operation ? { operation, id: targetId } : null
}

function postTarget(request: UiAdminRequest, resource: Resource, suffix: string | undefined, targetId: string): AdminOperation | null {
  if (request.method !== 'POST' || !empty(request)) return null
  if (resource === 'users') {
    const operation = suffix ? USER_OPERATIONS[suffix] : undefined
    if (operation) return { operation, id: targetId }
  }
  if (resource === 'channels' && suffix === 'duplicate') return { operation: 'duplicate_channel', id: targetId }
  return null
}

function optionalText(value: unknown, max: number, required = false): value is string | null {
  if (value === null) return true
  return text(value, max, required)
}

function optionalInteger(value: unknown, min: number, max: number): value is number | null {
  if (value === null) return true
  return integer(value, min, max)
}

function updateChannel(targetId: string, input: Record<string, unknown>): AdminOperation | null {
  const name = input['name'] ?? null
  const topic = input['topic'] ?? null
  const limit = input['user_limit'] ?? null
  if (!optionalText(name, 64, true) || !optionalText(topic, 255)) return null
  if (!optionalInteger(limit, 0, 999)) return null
  return { operation: 'update_channel', id: targetId, name, topic, user_limit: limit }
}

function userStatus(targetId: string, request: UiAdminRequest, suffix: string | undefined, input: Record<string, unknown>): AdminOperation | null {
  if (request.method !== 'PUT' || suffix !== 'status') return null
  if (!keys(input, ['status_text'], ['status_text'])) return null
  const status = input['status_text']
  if (!text(status, 32)) return null
  return { operation: 'user_status', id: targetId, status_text: status }
}

function renameCategory(targetId: string, request: UiAdminRequest, suffix: string | undefined, input: Record<string, unknown>): AdminOperation | null {
  if (request.method !== 'PATCH' || suffix !== undefined) return null
  if (!keys(input, ['name'], ['name'])) return null
  const name = input['name']
  if (!text(name, 64, true)) return null
  return { operation: 'rename_category', id: targetId, name }
}

/** A user status, a category rename or a channel update, by the resource it targets. */
function updateTarget(request: UiAdminRequest, resource: Resource, suffix: string | undefined, targetId: string): AdminOperation | null {
  const input = request.json
  if (!record(input)) return null
  if (resource === 'users') return userStatus(targetId, request, suffix, input)
  if (resource === 'categories') return renameCategory(targetId, request, suffix, input)
  if (resource !== 'channels' || suffix !== undefined) return null
  if (request.method !== 'PATCH') return null
  if (!keys(input, ['name', 'topic', 'user_limit'])) return null
  return updateChannel(targetId, input)
}

function inviteCode(value: unknown): value is string {
  if (typeof value !== 'string') return false
  const trimmed = value.trim()
  if (trimmed === '') return true
  return /^[A-Za-z0-9_-]{6,64}$/.test(trimmed)
}

function createInvite(input: Record<string, unknown>): AdminOperation | null {
  if (!keys(input, ['code', 'max_uses', 'expires_in_hours'])) return null
  const code = input['code'] ?? ''
  const uses = input['max_uses'] ?? null
  const hours = input['expires_in_hours'] ?? null
  if (!inviteCode(code) || !optionalInteger(uses, 1, 1000)) return null
  if (!optionalInteger(hours, 1, 8760)) return null
  return { operation: 'create_invite', code, max_uses: uses, expires_in_hours: hours }
}

function createCategory(input: Record<string, unknown>): AdminOperation | null {
  if (!keys(input, ['name', 'sort_order'], ['name']) || !text(input['name'], 64, true)) return null
  const position = input['sort_order'] ?? 0
  return order(position) ? { operation: 'create_category', name: input['name'], sort_order: position } : null
}

function optionalId(value: unknown): value is string | null {
  if (value === null) return true
  return id(value)
}

function channelKind(value: unknown): value is 'text' | 'voice' {
  if (value === 'text') return true
  return value === 'voice'
}

function createChannel(input: Record<string, unknown>): AdminOperation | null {
  if (!keys(input, ['category_id', 'name', 'type', 'topic', 'sort_order'], ['name'])) return null
  const name = input['name']
  if (!text(name, 64, true)) return null
  const category = input['category_id'] ?? null
  const kind = input['type'] ?? 'text'
  const topic = input['topic'] ?? ''
  const position = input['sort_order'] ?? 0
  if (!optionalId(category) || !channelKind(kind)) return null
  if (!text(topic, 255) || !order(position)) return null
  return { operation: 'create_channel', category_id: category, name, kind, topic, sort_order: position }
}

function layoutReady(input: Record<string, unknown>): input is Record<string, unknown> & { categories: unknown[]; channels: unknown[] } {
  if (!keys(input, ['categories', 'channels'], ['categories', 'channels'])) return false
  const categories = input['categories']
  const channels = input['channels']
  if (!Array.isArray(categories) || !Array.isArray(channels)) return false
  return categories.length + channels.length <= 500
}

function categoryOrder(row: unknown, seen: Set<string>): CategoryOrder | null {
  if (!record(row) || !keys(row, ['id', 'sort_order'], ['id', 'sort_order'])) return null
  const rowId = row['id']
  const position = row['sort_order']
  if (!id(rowId) || !order(position)) return null
  if (seen.has(rowId)) return null
  seen.add(rowId)
  return { id: rowId, sort_order: position }
}

function channelPlacement(row: unknown, seen: Set<string>): ChannelPlacement | null {
  if (!record(row) || !keys(row, ['id', 'category_id', 'sort_order'], ['id', 'sort_order'])) return null
  const rowId = row['id']
  const position = row['sort_order']
  if (!id(rowId) || !order(position)) return null
  if (seen.has(rowId)) return null
  const category = row['category_id'] ?? null
  if (category !== null && !id(category)) return null
  seen.add(rowId)
  return { id: rowId, category_id: category, sort_order: position }
}

function takeRows<T>(rows: readonly unknown[], parse: (row: unknown, seen: Set<string>) => T | null): T[] | null {
  const seen = new Set<string>()
  const parsed: T[] = []
  for (const row of rows) {
    const item = parse(row, seen)
    if (item === null) return null
    parsed.push(item)
  }
  return parsed
}

/** The whole sidebar layout: every row exactly once, at most 500 rows. */
function replaceLayout(input: Record<string, unknown>): AdminOperation | null {
  if (!layoutReady(input)) return null
  const categories = takeRows(input.categories, categoryOrder)
  if (!categories) return null
  const channels = takeRows(input.channels, channelPlacement)
  if (!channels) return null
  return { operation: 'layout', categories, channels }
}

/** Creating or rearranging by body, for the collection paths. */
function collectionOperation(path: string, method: string, input: Record<string, unknown>): AdminOperation | null {
  if (path === '/api/admin/invites' && method === 'POST') return createInvite(input)
  if (path === '/api/admin/categories' && method === 'POST') return createCategory(input)
  if (path === '/api/admin/channels' && method === 'POST') return createChannel(input)
  if (path === '/api/admin/layout' && method === 'PUT') return replaceLayout(input)
  return null
}

export function mapAdminOperation(path: string, request: UiAdminRequest): AdminOperation | null {
  if (request.form !== undefined) return null
  const listed = listOperation(path, request)
  if (listed) return listed
  const target = /^\/api\/admin\/(invites|users|categories|channels)\/([^/]+)(?:\/(disable|enable|sessions\/revoke|kick|status|duplicate))?$/.exec(path)
  const targetId = target?.[2]
  if (target && id(targetId)) {
    const resource = target[1] as Resource; const suffix = target[3]
    const mapped = deleteTarget(request, resource, suffix, targetId) ?? postTarget(request, resource, suffix, targetId) ?? updateTarget(request, resource, suffix, targetId)
    if (mapped) return mapped
  }
  const input = request.json
  if (!record(input)) return null
  return collectionOperation(path, request.method, input)
}
