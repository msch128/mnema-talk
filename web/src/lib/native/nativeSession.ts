import { t } from '../../i18n'
import type { ApiReply } from '../api'
import type { StatusChannel } from '../trustPort'
import { NativeTypedChatApi } from './nativeChatApi'

export interface NativeReply { context: string; status: number; body: unknown }
export type NativeInvoke = (command: string, args?: Record<string, unknown>) => Promise<unknown>
export interface NativeNotice { context: string; handle: string; sequence: number; kind: 'opened' | 'message' | 'closed'; payload: unknown }
export interface NativeChannel { onmessage: (notice: NativeNotice) => void }
export interface NativePort { invoke: NativeInvoke; channel: () => NativeChannel; trustChannel?: () => StatusChannel; chatChannel?: () => StatusChannel }

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
const NIL_UUID = '00000000-0000-0000-0000-000000000000'

export const nativeSession = {
  context: null as string | null,
  profileIntent: null as string | null,
  authenticationIntent: null as string | null,
  authenticationUncertain: false,
  profileGeneration: 0,
  authenticationRevision: 0,
  typedChat: new NativeTypedChatApi(),
}

let port!: NativePort
let retireSockets: () => void = function retireNothing(): void {}

export function bindNativePort(next: NativePort): void {
  port = next
}

export function readPort(): NativePort {
  return port
}

export function samePort(expected: NativePort): boolean {
  return port === expected
}

export interface CapturedSelector {
  owner: string | null
  profile: string | null
  authentication: string | null
  generation: number
  revision: number
  port: NativePort
}

export function captureSelector(): CapturedSelector {
  return {
    owner: nativeSession.context,
    profile: nativeSession.profileIntent,
    authentication: nativeSession.authenticationIntent,
    generation: nativeSession.profileGeneration,
    revision: nativeSession.authenticationRevision,
    port: readPort(),
  }
}

export function selectorCurrent(selector: CapturedSelector): boolean {
  if (nativeSession.context !== selector.owner) return false
  if (nativeSession.profileIntent !== selector.profile) return false
  if (nativeSession.authenticationIntent !== selector.authentication) return false
  if (nativeSession.profileGeneration !== selector.generation) return false
  if (nativeSession.authenticationRevision !== selector.revision) return false
  return samePort(selector.port)
}

export function onRetireSockets(retire: () => void): void {
  retireSockets = retire
}

export function retireNativeSockets(): void {
  retireSockets()
}

export function clearAuthenticationLine(): void {
  nativeSession.typedChat.clear()
  nativeSession.authenticationRevision++
  retireNativeSockets()
}

export function installNativePort(next: NativePort): void {
  nativeSession.typedChat.clear()
  retireNativeSockets()
  nativeSession.profileGeneration++
  nativeSession.context = null
  nativeSession.profileIntent = null
  nativeSession.authenticationIntent = null
  nativeSession.authenticationUncertain = false
  port = next
}

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

export function canonicalId(value: unknown): value is string {
  return typeof value === 'string' && UUID.test(value) && value !== NIL_UUID
}

/** UUID text, including the nil id. Socket handles use this, not canonicalId. */
export function uuidText(value: unknown): value is string {
  return typeof value === 'string' && UUID.test(value)
}

export function stringOrNull(value: unknown): string | null {
  if (typeof value !== 'string') return null
  return value
}

export function invalidOptionalId(value: unknown): boolean {
  return value !== null && !canonicalId(value)
}

export function cancelled(): DOMException {
  return new DOMException('Native operation cancelled', 'AbortError')
}

export function unavailable(): ApiReply {
  return { status: 503, body: { error: { code: 'UNAVAILABLE', message: t('nativeDesktop.featureUnavailable') } } }
}

export function encodedLength(value: string): number {
  return new TextEncoder().encode(value).length
}

export function validReply(value: unknown, expected: string | null): value is NativeReply {
  if (!isRecord(value) || value['context'] !== expected) return false
  if (!Number.isInteger(value['status'])) return false
  const status = Number(value['status'])
  return status >= 100 && status <= 599
}
