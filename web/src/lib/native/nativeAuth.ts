import { t } from '../../i18n'
import type { ApiReply, ApiRequest } from '../api'
import { call, recoverAuthenticationForExplicitLogin } from './nativeFlow'
import { registerThenLogin, type RegistrationResult } from './nativeRegistration'
import {
  cancelled, canonicalId, captureSelector, clearAuthenticationLine, isRecord, nativeSession, readPort, samePort, selectorCurrent, unavailable, validReply,
  type NativePort, type NativeReply,
} from './nativeSession'

interface RegistrationAttempt {
  acceptedLoginIntent: string | null
  loginAttempted: boolean
}

interface RegistrationRun {
  owner: string
  selected: string
  lineage: string | null
  generation: number
  revision: number
  captured: NativePort
  signal?: AbortSignal | undefined
  attempt: RegistrationAttempt
}

interface RegistrationFields {
  username: string
  display_name: string
  password: string
  invite_code: string
}

function registrationFields(input: Record<string, unknown>): RegistrationFields | null {
  if (Object.keys(input).length !== 4) return null
  const username = input['username']
  const displayName = input['display_name']
  const password = input['password']
  const inviteCode = input['invite_code']
  if (typeof username !== 'string' || typeof displayName !== 'string') return null
  if (typeof password !== 'string' || typeof inviteCode !== 'string') return null
  return { username, display_name: displayName, password, invite_code: inviteCode }
}

function beginRegistration(signal?: AbortSignal): RegistrationRun {
  clearAuthenticationLine()
  const owner = nativeSession.context
  const selected = nativeSession.profileIntent
  if (!owner || !selected) throw new Error('Native profile unavailable')
  return {
    owner,
    selected,
    lineage: nativeSession.authenticationIntent,
    generation: nativeSession.profileGeneration,
    revision: nativeSession.authenticationRevision,
    captured: readPort(),
    signal,
    attempt: { acceptedLoginIntent: null, loginAttempted: false },
  }
}

function registrationStillCurrent(run: RegistrationRun): boolean {
  if (nativeSession.context !== run.owner || nativeSession.profileIntent !== run.selected) return false
  if (nativeSession.authenticationIntent !== run.lineage || nativeSession.profileGeneration !== run.generation) return false
  if (nativeSession.authenticationRevision !== run.revision || !samePort(run.captured)) return false
  return run.signal?.aborted !== true
}

function registrationPayload(command: string, values: Record<string, string>): Record<string, unknown> {
  if (command !== 'native_auth_register') return values
  return {
    username: values['username'],
    displayName: values['display_name'],
    password: values['password'],
    inviteCode: values['invite_code'],
  }
}

function loginAcknowledgement(body: unknown): string {
  if (!isRecord(body) || !canonicalId(body['authentication_intent'])) throw new Error('Invalid native login acknowledgement')
  return body['authentication_intent']
}

function acceptRegistrationLogin(reply: NativeReply, attempt: RegistrationAttempt): void {
  attempt.acceptedLoginIntent = loginAcknowledgement(reply.body)
}

async function registrationCall(run: RegistrationRun, command: 'native_auth_register' | 'native_auth_login', values: Record<string, string>): Promise<NativeReply> {
  if (!registrationStillCurrent(run)) throw cancelled()
  if (command === 'native_auth_login') run.attempt.loginAttempted = true
  const reply = await call(command, registrationPayload(command, values), run.signal)
  if (command === 'native_auth_login' && reply.status === 200) acceptRegistrationLogin(reply, run.attempt)
  return reply
}

async function retireRegistration(run: RegistrationRun): Promise<void> {
  if (!registrationStillCurrent(run)) throw cancelled()
  const intent = run.attempt.acceptedLoginIntent
  if (!intent) throw new Error('Native captured session unavailable')
  // Identity-mismatch cleanup targets exactly the just-acknowledged native
  // login family, not the old unauthenticated selector or a later login.
  const value = await run.captured.invoke('native_auth_logout', {
    context: run.owner,
    profileIntent: run.selected,
    authenticationIntent: intent,
    requestId: crypto.randomUUID(),
  })
  if (!validReply(value, run.owner) || value.status !== 204) throw new Error('Native captured logout unavailable')
}

function nativeUserId(body: unknown): string {
  if (!isRecord(body) || !isRecord(body['user'])) throw new Error('Invalid native user acknowledgement')
  const id = body['user']['id']
  if (!canonicalId(id)) throw new Error('Invalid native user acknowledgement')
  return id
}

function markAuthenticationUncertain(): void {
  nativeSession.authenticationIntent = null
  nativeSession.authenticationUncertain = true
}

function loginRequired(): ApiReply {
  return { status: 503, body: { error: { code: 'LOGIN_REQUIRED', message: t('nativeDesktop.loginRequired') } } }
}

function authenticatedRegistration(reply: { status: number; body: unknown }): ApiReply {
  nativeSession.authenticationIntent = loginAcknowledgement(reply.body)
  return { status: reply.status, body: reply.body }
}

function finishRegistration(run: RegistrationRun, result: RegistrationResult): ApiReply {
  if (!registrationStillCurrent(run)) throw cancelled()
  if (result.kind === 'authenticated') return authenticatedRegistration(result.reply)
  if (result.kind === 'rejected') return { status: result.reply.status, body: result.reply.body }
  if (run.attempt.loginAttempted) markAuthenticationUncertain()
  return loginRequired()
}

export async function nativeRegister(request: ApiRequest, input: Record<string, unknown>): Promise<ApiReply> {
  const fields = registrationFields(input)
  if (!fields) return unavailable()
  const run = beginRegistration(request.signal)
  const result = await registerThenLogin({
    isCurrent: () => registrationStillCurrent(run),
    call: (command, values) => registrationCall(run, command, values),
    retireCaptured: () => retireRegistration(run),
    userId: nativeUserId,
  }, fields)
  return finishRegistration(run, result)
}

function passwordInput(input: Record<string, unknown>): Record<string, unknown> | null {
  if (Object.keys(input).length !== 2) return null
  const currentPassword = input['current_password']
  const newPassword = input['new_password']
  if (typeof currentPassword !== 'string' || typeof newPassword !== 'string') return null
  return { currentPassword, newPassword }
}

function passwordAcknowledgement(body: unknown): string {
  if (!isRecord(body) || Object.keys(body).length !== 1) throw new Error('Invalid native password acknowledgement')
  const intent = body['authentication_intent']
  if (!canonicalId(intent)) throw new Error('Invalid native password acknowledgement')
  return intent
}

function passwordResult(reply: NativeReply): ApiReply {
  if (reply.status !== 200) {
    nativeSession.authenticationIntent = null
    return { status: reply.status, body: reply.body }
  }
  nativeSession.authenticationIntent = passwordAcknowledgement(reply.body)
  nativeSession.authenticationUncertain = false
  return { status: 204, body: null }
}

interface AuthLine {
  intent: string | null
  profile: string | null
  context: string | null
  generation: number
  revision: number
}

function captureAuthLine(): AuthLine {
  return {
    intent: nativeSession.authenticationIntent,
    profile: nativeSession.profileIntent,
    context: nativeSession.context,
    generation: nativeSession.profileGeneration,
    revision: nativeSession.authenticationRevision,
  }
}

function authLineUntouched(line: AuthLine): boolean {
  if (nativeSession.context !== line.context || nativeSession.profileIntent !== line.profile) return false
  if (nativeSession.profileGeneration !== line.generation || nativeSession.authenticationRevision !== line.revision) return false
  return nativeSession.authenticationIntent === line.intent
}

function burnUncertainAuthentication(line: AuthLine): void {
  if (!authLineUntouched(line)) return
  markAuthenticationUncertain()
}

export async function nativePassword(request: ApiRequest, input: Record<string, unknown>): Promise<ApiReply> {
  const passwords = passwordInput(input)
  if (!passwords) return unavailable()
  clearAuthenticationLine()
  const line = captureAuthLine()
  try {
    return passwordResult(await call('native_auth_password', passwords, request.signal))
  } catch (error) {
    burnUncertainAuthentication(line)
    throw error
  }
}

function loginInput(input: Record<string, unknown>): Record<string, unknown> | null {
  const username = input['username']
  const password = input['password']
  if (typeof username !== 'string' || typeof password !== 'string') return null
  if (Object.keys(input).length !== 2) return null
  return { username, password }
}

interface LoginSelection {
  context: string | null
  profile: string | null
  generation: number
  revision: number
  port: NativePort
}

function captureLoginSelection(): LoginSelection {
  return {
    context: nativeSession.context,
    profile: nativeSession.profileIntent,
    generation: nativeSession.profileGeneration,
    revision: nativeSession.authenticationRevision,
    port: readPort(),
  }
}

function loginSelectionMoved(selected: LoginSelection): boolean {
  if (nativeSession.context !== selected.context || nativeSession.profileIntent !== selected.profile) return true
  if (nativeSession.profileGeneration !== selected.generation || nativeSession.authenticationRevision !== selected.revision) return true
  return !samePort(selected.port)
}

function loginResult(reply: NativeReply): ApiReply {
  if (reply.status !== 200) {
    nativeSession.authenticationIntent = null
    return { status: reply.status, body: reply.body }
  }
  nativeSession.authenticationIntent = loginAcknowledgement(reply.body)
  nativeSession.authenticationUncertain = false
  return { status: reply.status, body: reply.body }
}

export async function nativeLogin(request: ApiRequest, input: Record<string, unknown>): Promise<ApiReply> {
  const credentials = loginInput(input)
  if (!credentials) return unavailable()
  clearAuthenticationLine()
  const selected = captureLoginSelection()
  if (nativeSession.authenticationUncertain) await recoverAuthenticationForExplicitLogin(request.signal)
  if (loginSelectionMoved(selected)) throw cancelled()
  const line = captureAuthLine()
  try {
    return loginResult(await call('native_auth_login', credentials, request.signal))
  } catch (error) {
    burnUncertainAuthentication(line)
    throw error
  }
}

function noteLogout(reply: NativeReply): void {
  nativeSession.authenticationIntent = null
  nativeSession.authenticationUncertain = reply.status !== 204
}

export async function nativeLogout(request: ApiRequest): Promise<ApiReply> {
  clearAuthenticationLine()
  const snapshot = captureSelector()
  try {
    const reply = await call('native_auth_logout', {}, request.signal)
    if (selectorCurrent(snapshot)) noteLogout(reply)
    return { status: reply.status, body: reply.body }
  } catch (error) {
    // An unknown ACK may follow a committed native logout. Burn only this
    // captured comparison selector; a later explicit Login reads context.
    // Never replay Logout or clear a newer accepted account/session.
    if (selectorCurrent(snapshot)) markAuthenticationUncertain()
    throw error
  }
}
