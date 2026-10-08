// Public presentation DTOs only. No value here authorizes a native crypto operation.
export interface TrustPreview {
  version: 1
  operation_id: string
  operation_kind: 'first_root' | 'pin_root' | 'approve_device' | 'replace_root'
  expires_at: string
  scope: { origin: string; community_id: string; channel_id: string; account_id: string; device_id: string; group_id: string }
  root_fingerprint_hex: string
  root_public_key_hex: string
  device_public_key_hex: string
}
export interface TrustStatus { operation_id: string; state: 'pending' | 'cancelled' | 'expired' | 'root_saved' | 'device_saved' | 'failed' }
export interface NativeTrustPresentationPort {
  beginFirstRoot(channelId: string): Promise<unknown>
  requestConfirmation(operationId: string): Promise<unknown>
  cancel(operationId: string): Promise<void>
  // The actor adapter owns subscription context/fences; callbacks are public status only.
  subscribe(listener: (status: unknown) => void): () => void
}
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
const HEX32 = /^[0-9a-f]{64}$/
const kinds = new Set(['first_root', 'pin_root', 'approve_device', 'replace_root'])
const states = new Set(['pending', 'cancelled', 'expired', 'root_saved', 'device_saved', 'failed'])
function object(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value) }
function exact(value: Record<string, unknown>, names: readonly string[]) { return Object.keys(value).length === names.length && names.every(name => Object.hasOwn(value, name)) }
function bounded(value: unknown, maximum: number): value is string { return typeof value === 'string' && value.length > 0 && new TextEncoder().encode(value).length <= maximum && !Array.from(value).some(character => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127) }
export function trustOperationId(value: unknown): value is string { return typeof value === 'string' && UUID.test(value) }
export function decodeTrustPreview(value: unknown): TrustPreview {
  if (!object(value) || !exact(value, ['version', 'operation_id', 'operation_kind', 'expires_at', 'scope', 'root_fingerprint_hex', 'root_public_key_hex', 'device_public_key_hex']) || value['version'] !== 1 || !trustOperationId(value['operation_id']) || typeof value['operation_kind'] !== 'string' || !kinds.has(value['operation_kind']) || typeof value['expires_at'] !== 'string' || !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(value['expires_at']) || !Number.isFinite(Date.parse(value['expires_at']))) throw new Error('Invalid native trust preview')
  const scope = value['scope']
  if (!object(scope) || !exact(scope, ['origin', 'community_id', 'channel_id', 'account_id', 'device_id', 'group_id']) || !bounded(scope['origin'], 2048) || !bounded(scope['community_id'], 128) || !trustOperationId(scope['channel_id']) || !trustOperationId(scope['account_id']) || !trustOperationId(scope['device_id']) || !bounded(scope['group_id'], 172) || !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(scope['group_id'])) throw new Error('Invalid native trust scope')
  const origin = new URL(scope['origin'])
  if (origin.protocol !== 'https:' || origin.origin !== scope['origin'] || origin.username || origin.password || btoa(atob(scope['group_id'])) !== scope['group_id']) throw new Error('Invalid native trust scope')
  for (const name of ['root_fingerprint_hex', 'root_public_key_hex', 'device_public_key_hex']) if (typeof value[name] !== 'string' || !HEX32.test(value[name])) throw new Error('Invalid native trust public key')
  // Construct a new closed object so callers cannot later mutate the received wire object.
  return { version: 1, operation_id: value['operation_id'], operation_kind: value['operation_kind'] as TrustPreview['operation_kind'], expires_at: value['expires_at'], scope: { origin: scope['origin'], community_id: scope['community_id'], channel_id: scope['channel_id'], account_id: scope['account_id'], device_id: scope['device_id'], group_id: scope['group_id'] }, root_fingerprint_hex: value['root_fingerprint_hex'] as string, root_public_key_hex: value['root_public_key_hex'] as string, device_public_key_hex: value['device_public_key_hex'] as string }
}
export function decodeTrustStatus(value: unknown): TrustStatus {
  if (!object(value) || !exact(value, ['operation_id', 'state']) || !trustOperationId(value['operation_id']) || typeof value['state'] !== 'string' || !states.has(value['state'])) throw new Error('Invalid native trust status')
  return { operation_id: value['operation_id'], state: value['state'] as TrustStatus['state'] }
}
