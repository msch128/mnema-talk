/** An acknowledged account creation is not an authenticated native session. */
export interface RegistrationInput {
  readonly username: string
  readonly display_name: string
  readonly password: string
  readonly invite_code: string
}
export interface Reply { readonly status: number; readonly body: unknown }
/** All callbacks must belong to the same captured document/profile operation.
 * retireCaptured must seal only that capture, never a replacement profile.
 * call validates native context/reply and cancellation before resolving.
 */
export interface CapturedPort {
  isCurrent(): boolean
  call(command: 'native_auth_register' | 'native_auth_login', input: Record<string, string>): Promise<Reply>
  retireCaptured(): Promise<void>
  userId(body: unknown): string
}
export type RegistrationResult =
  | { kind: 'authenticated'; reply: Reply }
  | { kind: 'rejected'; reply: Reply }
  | { kind: 'login_required'; reason: 'ack_unknown' | 'invalid_ack' | 'login_failed' | 'scope_retired' | 'identity_mismatch' }

export async function registerThenLogin(port: CapturedPort, input: RegistrationInput): Promise<RegistrationResult> {
  if (!port.isCurrent()) return { kind: 'login_required', reason: 'scope_retired' }
  let registration: Reply
  try {
    registration = await port.call('native_auth_register', {
      username: input.username, display_name: input.display_name,
      password: input.password, invite_code: input.invite_code
    })
  } catch {
    // The invite may already be consumed: never retry or infer an acknowledgement.
    return { kind: 'login_required', reason: 'ack_unknown' }
  }
  if (!port.isCurrent()) return { kind: 'login_required', reason: 'scope_retired' }
  if (registration.status !== 201) return { kind: 'rejected', reply: registration }
  let createdUser: string
  try { createdUser = port.userId(registration.body) }
  catch { return { kind: 'login_required', reason: 'invalid_ack' } }
  if (!port.isCurrent()) return { kind: 'login_required', reason: 'scope_retired' }
  let login: Reply
  try { login = await port.call('native_auth_login', { username: input.username, password: input.password }) }
  catch { return { kind: 'login_required', reason: 'login_failed' } }
  if (!port.isCurrent()) return { kind: 'login_required', reason: 'scope_retired' }
  if (login.status !== 200) return { kind: 'login_required', reason: 'login_failed' }
  try {
    if (port.userId(login.body) !== createdUser) throw new Error('identity mismatch')
  } catch {
    // A native session might exist; seal its captured owner before reporting failure.
    await port.retireCaptured()
    return { kind: 'login_required', reason: 'identity_mismatch' }
  }
  if (!port.isCurrent()) return { kind: 'login_required', reason: 'scope_retired' }
  return { kind: 'authenticated', reply: login }
}
