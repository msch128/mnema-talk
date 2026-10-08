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
export function mapAdminOperation(path: string, request: UiAdminRequest): AdminOperation | null {
  if (request.form !== undefined) return null
  if (request.method === 'GET' && empty(request)) {
    const action: Readonly<Record<string, 'invites' | 'users' | 'system' | 'system_update'>> = { '/api/admin/invites': 'invites', '/api/admin/users': 'users', '/api/admin/system': 'system', '/api/admin/system/update': 'system_update' }
    const operation = Object.hasOwn(action, path) ? action[path] : undefined
    if (operation) return { operation }
  }
  const target = /^\/api\/admin\/(invites|users|categories|channels)\/([^/]+)(?:\/(disable|enable|sessions\/revoke|kick|status|duplicate))?$/.exec(path)
  const targetId = target?.[2]
  if (target && id(targetId)) {
    const resource = target[1]; const suffix = target[3]
    if (request.method === 'DELETE' && suffix === undefined && empty(request)) {
      if (resource === 'invites') return { operation: 'delete_invite', id: targetId }
      if (resource === 'categories') return { operation: 'delete_category', id: targetId }
      if (resource === 'channels') return { operation: 'delete_channel', id: targetId }
    }
    if (request.method === 'POST' && empty(request)) {
      if (resource === 'users') {
        const operations: Readonly<Record<string, 'disable_user' | 'enable_user' | 'revoke_user_sessions' | 'kick_user'>> = { disable: 'disable_user', enable: 'enable_user', 'sessions/revoke': 'revoke_user_sessions', kick: 'kick_user' }
        if (suffix && operations[suffix]) return { operation: operations[suffix], id: targetId }
      }
      if (resource === 'channels' && suffix === 'duplicate') return { operation: 'duplicate_channel', id: targetId }
    }
    const input = request.json
    if (record(input)) {
      if (resource === 'users' && suffix === 'status' && request.method === 'PUT' && keys(input, ['status_text'], ['status_text']) && text(input['status_text'], 32)) return { operation: 'user_status', id: targetId, status_text: input['status_text'] }
      if (resource === 'categories' && suffix === undefined && request.method === 'PATCH' && keys(input, ['name'], ['name']) && text(input['name'], 64, true)) return { operation: 'rename_category', id: targetId, name: input['name'] }
      if (resource === 'channels' && suffix === undefined && request.method === 'PATCH' && keys(input, ['name', 'topic', 'user_limit'])) {
        const name = input['name'] ?? null; const topic = input['topic'] ?? null; const limit = input['user_limit'] ?? null
        if ((name === null || text(name, 64, true)) && (topic === null || text(topic, 255)) && (limit === null || integer(limit, 0, 999))) return { operation: 'update_channel', id: targetId, name, topic, user_limit: limit }
      }
    }
  }
  const input = request.json
  if (!record(input)) return null
  if (path === '/api/admin/invites' && request.method === 'POST' && keys(input, ['code', 'max_uses', 'expires_in_hours'])) {
    const code = input['code'] ?? ''; const uses = input['max_uses'] ?? null; const hours = input['expires_in_hours'] ?? null
    if (typeof code === 'string' && (code.trim() === '' || /^[A-Za-z0-9_-]{6,64}$/.test(code.trim())) && (uses === null || integer(uses, 1, 1000)) && (hours === null || integer(hours, 1, 8760))) return { operation: 'create_invite', code, max_uses: uses, expires_in_hours: hours }
  }
  if (path === '/api/admin/categories' && request.method === 'POST' && keys(input, ['name', 'sort_order'], ['name']) && text(input['name'], 64, true)) {
    const position = input['sort_order'] ?? 0
    if (order(position)) return { operation: 'create_category', name: input['name'], sort_order: position }
  }
  if (path === '/api/admin/channels' && request.method === 'POST' && keys(input, ['category_id', 'name', 'type', 'topic', 'sort_order'], ['name']) && text(input['name'], 64, true)) {
    const category = input['category_id'] ?? null; const kind = input['type'] ?? 'text'; const topic = input['topic'] ?? ''; const position = input['sort_order'] ?? 0
    if ((category === null || id(category)) && (kind === 'text' || kind === 'voice') && text(topic, 255) && order(position)) return { operation: 'create_channel', category_id: category, name: input['name'], kind, topic, sort_order: position }
  }
  if (path === '/api/admin/layout' && request.method === 'PUT' && keys(input, ['categories', 'channels'], ['categories', 'channels']) && Array.isArray(input['categories']) && Array.isArray(input['channels']) && input['categories'].length + input['channels'].length <= 500) {
    const categories: CategoryOrder[] = []; const channels: ChannelPlacement[] = []; const seen = new Set<string>()
    for (const row of input['categories'] as unknown[]) {
      if (!record(row) || !keys(row, ['id', 'sort_order'], ['id', 'sort_order']) || !id(row['id']) || !order(row['sort_order']) || seen.has(row['id'])) return null
      seen.add(row['id']); categories.push({ id: row['id'], sort_order: row['sort_order'] })
    }
    seen.clear()
    for (const row of input['channels'] as unknown[]) {
      if (!record(row) || !keys(row, ['id', 'category_id', 'sort_order'], ['id', 'sort_order']) || !id(row['id']) || !order(row['sort_order']) || seen.has(row['id'])) return null
      const category = row['category_id'] ?? null
      if (category !== null && !id(category)) return null
      seen.add(row['id']); channels.push({ id: row['id'], category_id: category, sort_order: row['sort_order'] })
    }
    return { operation: 'layout', categories, channels }
  }
  return null
}
