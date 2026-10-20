import { t } from '../../i18n'
import {
  cancelled, canonicalId, clearAuthenticationLine, encodedLength, invalidOptionalId, isRecord,
  nativeSession, readPort, retireNativeSockets, samePort, stringOrNull, validReply,
  type NativePort, type NativeReply,
} from './nativeSession'

interface ReadyCall {
  owner: string
  expectedProfile: string | null
  expectedAuthentication: string | null
  generation: number
  revision: number
  currentPort: NativePort
  selected: boolean
  signal?: AbortSignal | undefined
}

function captureCall(signal: AbortSignal | undefined, selected: boolean): ReadyCall {
  const owner = nativeSession.context
  const expectedProfile = nativeSession.profileIntent
  const expectedAuthentication = nativeSession.authenticationIntent
  const generation = nativeSession.profileGeneration
  const revision = nativeSession.authenticationRevision
  const currentPort = readPort()
  if (!owner) throw new Error('Native context unavailable')
  if (selected && !expectedProfile) throw new Error('Native profile unavailable')
  if (signal?.aborted) throw cancelled()
  return { owner, expectedProfile, expectedAuthentication, generation, revision, currentPort, selected, signal }
}

function cancelCall(capture: ReadyCall, requestId: string): void {
  void capture.currentPort.invoke('native_request_cancel', {
    context: capture.owner,
    profileIntent: capture.expectedProfile,
    authenticationIntent: capture.expectedAuthentication,
    requestId,
  }).catch(() => {})
}

function invokeArgs(capture: ReadyCall, requestId: string, payload: Record<string, unknown>): Record<string, unknown> {
  const args: Record<string, unknown> = { context: capture.owner, requestId, ...payload }
  if (!capture.selected) return args
  args['profileIntent'] = capture.expectedProfile
  args['authenticationIntent'] = capture.expectedAuthentication
  return args
}

function callStale(capture: ReadyCall): boolean {
  if (capture.signal?.aborted || nativeSession.context !== capture.owner) return true
  if (nativeSession.profileGeneration !== capture.generation || !samePort(capture.currentPort)) return true
  if (nativeSession.authenticationRevision !== capture.revision) return true
  if (!capture.selected) return false
  if (nativeSession.profileIntent !== capture.expectedProfile) return true
  return nativeSession.authenticationIntent !== capture.expectedAuthentication
}

function acceptedReply(capture: ReadyCall, value: unknown): NativeReply {
  if (callStale(capture)) throw cancelled()
  if (!validReply(value, capture.owner)) throw new Error('Invalid native reply')
  if (value.status === 499) throw cancelled()
  return value
}

export async function call(command: string, payload: Record<string, unknown> = {}, signal?: AbortSignal, selected = true): Promise<NativeReply> {
  const capture = captureCall(signal, selected)
  const requestId = crypto.randomUUID()
  const abort = () => { cancelCall(capture, requestId) }
  capture.signal?.addEventListener('abort', abort, { once: true })
  try {
    const value = await capture.currentPort.invoke(command, invokeArgs(capture, requestId, payload))
    return acceptedReply(capture, value)
  } finally {
    capture.signal?.removeEventListener('abort', abort)
  }
}

function nativeContextRecord(reply: unknown): { reply: Record<string, unknown>; context: string } {
  if (!isRecord(reply)) throw new Error('Native context unavailable')
  const context = reply['context']
  if (!canonicalId(context)) throw new Error('Native context unavailable')
  if (reply['content_authorization'] !== 'unavailable' || reply['remembered_login'] !== false) throw new Error('Native context unavailable')
  return { reply, context }
}

function requireContextIntents(reply: Record<string, unknown>): void {
  if (invalidOptionalId(reply['profile_intent'])) throw new Error('Invalid native profile intent')
  if (invalidOptionalId(reply['authentication_intent'])) throw new Error('Invalid native authentication intent')
  if (reply['profile_intent'] === null && reply['authentication_intent'] !== null) throw new Error('Invalid native authentication profile')
}

function contextChanged(reply: Record<string, unknown>): boolean {
  if (nativeSession.context !== reply['context']) return true
  if (nativeSession.profileIntent !== reply['profile_intent']) return true
  return nativeSession.authenticationIntent !== reply['authentication_intent']
}

export async function initializeNativeContext(): Promise<void> {
  const generation = nativeSession.profileGeneration
  const value = await readPort().invoke('native_context')
  if (generation !== nativeSession.profileGeneration) throw cancelled()
  const descriptor = nativeContextRecord(value)
  requireContextIntents(descriptor.reply)
  if (contextChanged(descriptor.reply)) clearAuthenticationLine()
  nativeSession.authenticationIntent = stringOrNull(descriptor.reply['authentication_intent'])
  nativeSession.authenticationUncertain = false
  nativeSession.context = descriptor.context
  nativeSession.profileIntent = stringOrNull(descriptor.reply['profile_intent'])
}

function profileDescriptor(state: unknown, owner: string | null): Record<string, unknown> {
  if (!isRecord(state) || state['context'] !== owner) throw new Error('Native profile descriptor unavailable')
  if (state['content_authorization'] !== 'unavailable' || state['remembered_login'] !== false) throw new Error('Native profile descriptor unavailable')
  if (invalidOptionalId(state['profile_intent'])) throw new Error('Native profile descriptor unavailable')
  return state
}

function publicDescriptor(state: unknown, owner: string | null): Record<string, unknown> {
  const record = profileDescriptor(state, owner)
  if (invalidOptionalId(record['authentication_intent'])) throw new Error('Native authentication descriptor unavailable')
  return record
}

// A lost Connect response may have selected a native profile. Read its
// public comparator for a new explicit connection attempt, never for login.
async function adoptPublicDescriptor(): Promise<void> {
  const owner = nativeSession.context
  const generation = nativeSession.profileGeneration
  const captured = readPort()
  const state = await captured.invoke('native_context')
  if (nativeSession.context !== owner || generation !== nativeSession.profileGeneration || !samePort(captured)) throw cancelled()
  const descriptor = publicDescriptor(state, owner)
  nativeSession.profileIntent = stringOrNull(descriptor['profile_intent'])
  nativeSession.authenticationIntent = stringOrNull(descriptor['authentication_intent'])
}

function acceptedProfileIntent(body: unknown): string {
  if (!isRecord(body)) throw new Error('Invalid native profile intent')
  const intent = body['profile_intent']
  if (!canonicalId(intent)) throw new Error('Invalid native profile intent')
  return intent
}

function dropSelectedProfile(): void {
  nativeSession.profileIntent = null
  nativeSession.authenticationIntent = null
  nativeSession.authenticationUncertain = false
  nativeSession.typedChat.clear()
  nativeSession.profileGeneration++
  retireNativeSockets()
}

export async function connectNative(address: string, signal?: AbortSignal): Promise<NativeReply> {
  if (encodedLength(address) > 2048) throw new Error(t('nativeDesktop.addressTooLong'))
  if (nativeSession.profileIntent === null) await adoptPublicDescriptor()
  const previous = nativeSession.profileIntent
  const previousAuthentication = nativeSession.authenticationIntent
  dropSelectedProfile()
  const reply = await call('native_connect', { address, profileIntent: previous, authenticationIntent: previousAuthentication }, signal, false)
  if (reply.status === 200) nativeSession.profileIntent = acceptedProfileIntent(reply.body)
  return reply
}

export async function disconnectNative(): Promise<void> {
  const expectedProfile = nativeSession.profileIntent
  const expectedAuthentication = nativeSession.authenticationIntent
  dropSelectedProfile()
  const owner = nativeSession.context
  if (!owner) return
  const value = await readPort().invoke('native_disconnect', { context: owner, profileIntent: expectedProfile, authenticationIntent: expectedAuthentication })
  if (!validReply(value, owner) || value.status !== 204) throw new Error('Native disconnect unavailable')
}

interface ExplicitCapture {
  owner: string
  profile: string
  generation: number
  revision: number
  captured: NativePort
  signal?: AbortSignal | undefined
}

function explicitLoginCapture(signal?: AbortSignal): ExplicitCapture {
  const owner = nativeSession.context
  const profile = nativeSession.profileIntent
  if (!owner || !profile || signal?.aborted) throw cancelled()
  return { owner, profile, generation: nativeSession.profileGeneration, revision: nativeSession.authenticationRevision, captured: readPort(), signal }
}

function explicitLoginStale(capture: ExplicitCapture): boolean {
  if (nativeSession.context !== capture.owner || nativeSession.profileIntent !== capture.profile) return true
  if (nativeSession.profileGeneration !== capture.generation || nativeSession.authenticationRevision !== capture.revision) return true
  return !samePort(capture.captured) || capture.signal?.aborted === true
}

function authenticationDescriptor(value: unknown, owner: string, profile: string): Record<string, unknown> {
  if (!isRecord(value)) throw new Error('Native authentication descriptor unavailable')
  if (value['context'] !== owner || value['profile_intent'] !== profile) throw new Error('Native authentication descriptor unavailable')
  if (value['content_authorization'] !== 'unavailable' || value['remembered_login'] !== false) throw new Error('Native authentication descriptor unavailable')
  if (value['authentication_intent'] !== null && !canonicalId(value['authentication_intent'])) throw new Error('Native authentication descriptor unavailable')
  return value
}

// Only a new explicit login can recover a comparison selector. No password,
// protected operation, or grant from an uncertain request is replayed here.
export async function recoverAuthenticationForExplicitLogin(signal?: AbortSignal): Promise<void> {
  if (!nativeSession.authenticationUncertain) return
  const capture = explicitLoginCapture(signal)
  const value = await capture.captured.invoke('native_context')
  if (explicitLoginStale(capture)) throw cancelled()
  const descriptor = authenticationDescriptor(value, capture.owner, capture.profile)
  nativeSession.authenticationIntent = stringOrNull(descriptor['authentication_intent'])
  nativeSession.authenticationUncertain = false
}
