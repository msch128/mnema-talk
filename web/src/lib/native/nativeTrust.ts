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
const EXPIRES_AT = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/
const GROUP_ID = /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/
const PREVIEW_KEYS = ['version', 'operation_id', 'operation_kind', 'expires_at', 'scope', 'root_fingerprint_hex', 'root_public_key_hex', 'device_public_key_hex']
const SCOPE_KEYS = ['origin', 'community_id', 'channel_id', 'account_id', 'device_id', 'group_id']
const KEY_NAMES = ['root_fingerprint_hex', 'root_public_key_hex', 'device_public_key_hex']
const kinds = new Set(['first_root', 'pin_root', 'approve_device', 'replace_root'])
const states = new Set(['pending', 'cancelled', 'expired', 'root_saved', 'device_saved', 'failed'])
function object(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value) }
function exact(value: Record<string, unknown>, names: readonly string[]) { return Object.keys(value).length === names.length && names.every(name => Object.hasOwn(value, name)) }
function bounded(value: unknown, maximum: number): value is string { return typeof value === 'string' && value.length > 0 && new TextEncoder().encode(value).length <= maximum && !Array.from(value).some(character => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127) }
export function trustOperationId(value: unknown): value is string { return typeof value === 'string' && UUID.test(value) }

function expiresAt(value: unknown): value is string {
  if (typeof value !== 'string' || !EXPIRES_AT.test(value)) return false
  return Number.isFinite(Date.parse(value))
}

function previewEnvelope(value: Record<string, unknown>): boolean {
  if (!exact(value, PREVIEW_KEYS) || value['version'] !== 1) return false
  if (!trustOperationId(value['operation_id'])) return false
  if (typeof value['operation_kind'] !== 'string' || !kinds.has(value['operation_kind'])) return false
  return expiresAt(value['expires_at'])
}

function scopeIds(scope: Record<string, unknown>): boolean {
  if (!trustOperationId(scope['channel_id'])) return false
  if (!trustOperationId(scope['account_id'])) return false
  return trustOperationId(scope['device_id'])
}

function groupIdOK(value: unknown): value is string {
  if (!bounded(value, 172)) return false
  return GROUP_ID.test(value)
}

function scopeFields(scope: Record<string, unknown>): boolean {
  if (!exact(scope, SCOPE_KEYS)) return false
  if (!bounded(scope['origin'], 2048) || !bounded(scope['community_id'], 128)) return false
  if (!scopeIds(scope)) return false
  return groupIdOK(scope['group_id'])
}

function scopeOrigin(originValue: unknown): boolean {
  if (typeof originValue !== 'string') return false
  const origin = new URL(originValue)
  if (origin.protocol !== 'https:' || origin.origin !== originValue) return false
  return !origin.username && !origin.password
}

function canonicalGroup(groupId: unknown): boolean {
  if (typeof groupId !== 'string') return false
  return btoa(atob(groupId)) === groupId
}

function hexKeys(value: Record<string, unknown>): boolean {
  for (const name of KEY_NAMES) {
    const field = value[name]
    if (typeof field !== 'string' || !HEX32.test(field)) return false
  }
  return true
}

export function decodeTrustPreview(value: unknown): TrustPreview {
  if (!object(value) || !previewEnvelope(value)) throw new Error('Invalid native trust preview')
  const scope = value['scope']
  if (!object(scope) || !scopeFields(scope)) throw new Error('Invalid native trust scope')
  if (!scopeOrigin(scope['origin']) || !canonicalGroup(scope['group_id'])) throw new Error('Invalid native trust scope')
  if (!hexKeys(value)) throw new Error('Invalid native trust public key')
  // Construct a new closed object so callers cannot later mutate the received wire object.
  return {
    version: 1,
    operation_id: value['operation_id'] as string,
    operation_kind: value['operation_kind'] as TrustPreview['operation_kind'],
    expires_at: value['expires_at'] as string,
    scope: trustScope(scope),
    root_fingerprint_hex: value['root_fingerprint_hex'] as string,
    root_public_key_hex: value['root_public_key_hex'] as string,
    device_public_key_hex: value['device_public_key_hex'] as string,
  }
}

function trustScope(scope: Record<string, unknown>): TrustPreview['scope'] {
  return {
    origin: scope['origin'] as string,
    community_id: scope['community_id'] as string,
    channel_id: scope['channel_id'] as string,
    account_id: scope['account_id'] as string,
    device_id: scope['device_id'] as string,
    group_id: scope['group_id'] as string,
  }
}

function statusFields(value: Record<string, unknown>): boolean {
  if (!exact(value, ['operation_id', 'state'])) return false
  if (!trustOperationId(value['operation_id'])) return false
  return typeof value['state'] === 'string' && states.has(value['state'])
}

export function decodeTrustStatus(value: unknown): TrustStatus {
  if (!object(value) || !statusFields(value)) throw new Error('Invalid native trust status')
  return { operation_id: value['operation_id'] as string, state: value['state'] as TrustStatus['state'] }
}
