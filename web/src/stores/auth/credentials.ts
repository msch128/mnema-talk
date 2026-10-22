import type { Ref } from 'vue'
import { api, ApiError } from '../../lib/api'
import { releaseThisDevice } from '../../lib/webPush'
import { decodeUser, decodeUserEnvelope, type User } from '../../types/domain'
import { isRecord } from '../../types/validation'
import { t } from '../../i18n'
import type { SessionCore } from './session'

interface AuthDeps {
  core: SessionCore
  user: Ref<User | null>
  applyAccountLocale: () => void
}

function isUnauthorizedSession(s: SessionCore['s'], session: ReturnType<SessionCore['sessionSnapshot']>): boolean {
  return s.unauthorizedGeneration === session.generation && s.generation === session.generation + 1
}

function handleCheckAuthError(err: unknown, s: SessionCore['s'], session: ReturnType<SessionCore['sessionSnapshot']>): boolean | null {
  if (isRecord(err) && err['status'] === 401) {
    // api already invalidated the current generation before rejecting.
    return isUnauthorizedSession(s, session) ? false : null
  }
  return null
}

function applyCurrentUser(
  currentUser: User,
  session: ReturnType<SessionCore['sessionSnapshot']>,
  deps: AuthDeps
): void {
  const { core, user, applyAccountLocale } = deps
  core.s.cookieAccountId = currentUser.id
  if (currentUser.id !== session.userId) core.advanceSession()
  user.value = currentUser
  applyAccountLocale()
}

/**
 * Asks the server who is logged in. Returns true (signed in), false (no or
 * revoked session: the user is cleared) or null when the server could not
 * answer (offline, restarting): the user is kept so a redeploy or a network
 * blip never logs anyone out.
 */
async function checkAuth(deps: AuthDeps) {
  const { core } = deps
  const session = core.sessionSnapshot()
  try {
    await core.s.cookieRequests
    if (!core.isCurrentSession(session)) return null
    const currentUser = await api('/api/auth/me', {
      decode: decodeUser,
      shouldNotifyUnauthorized: () => core.isCurrentSession(session)
    })
    if (!core.isCurrentSession(session)) return null
    applyCurrentUser(currentUser, session, deps)
    return true
  } catch (err) {
    return handleCheckAuthError(err, core.s, session)
  }
}

/** Login and registration: both set the session cookie and make the answered user the signed-in one. */
async function establishSession({ core, user, applyAccountLocale }: AuthDeps, path: string, json: object, ownNotification: boolean) {
  const { s, advanceSession, sessionSnapshot, isCurrentSession, clearMismatchedCookieAccount, queueCookieRequest } = core
  advanceSession()
  const session = sessionSnapshot()
  return queueCookieRequest(async () => {
    try {
      const data = await api(path, {
        method: 'POST',
        decode: decodeUserEnvelope,
        ...(ownNotification ? { shouldNotifyUnauthorized: () => isCurrentSession(session) } : {}),
        json
      })
      s.cookieAccountId = data.user.id
      if (isCurrentSession(session)) {
        advanceSession()
        user.value = data.user
        applyAccountLocale()
      }
      return data.user
    } catch (err) {
      clearMismatchedCookieAccount(session)
      throw err
    }
  })
}

async function changePassword({ core }: AuthDeps, currentPassword: string, newPassword: string) {
  const { s, advanceSession, sessionSnapshot, isCurrentSession, queueCookieRequest } = core
  const requestedSession = sessionSnapshot()
  await queueCookieRequest(async () => {
    // An earlier queued login may have changed the cookie's account. Never
    // send this account's password change against a replacement session.
    if (!isCurrentSession(requestedSession) || (s.cookieAccountId !== undefined && s.cookieAccountId !== requestedSession.userId)) {
      throw new ApiError(401, 'UNAUTHORIZED', t('errors.code.UNAUTHORIZED'))
    }
    advanceSession()
    const session = sessionSnapshot()
    await api('/api/auth/password', {
      method: 'PUT',
      shouldNotifyUnauthorized: () => isCurrentSession(session),
      json: { current_password: currentPassword, new_password: newPassword }
    })
    if (isCurrentSession(session)) advanceSession()
  })
}

async function logout({ core, user }: AuthDeps) {
  const { s, advanceSession, sessionSnapshot, isCurrentSession, queueCookieRequest } = core
  advanceSession()
  user.value = null
  const session = sessionSnapshot()
  await queueCookieRequest(async () => {
    // This browser must not keep receiving the account's messages once someone else may sign in here.
    await releaseThisDevice()
    await api('/api/auth/logout', {
      method: 'POST', shouldNotifyUnauthorized: () => isCurrentSession(session)
    })
    s.cookieAccountId = null
  })
}

/** Sign-in, sign-out and the session check, bound to the store's session. */
export function createAuthActions(deps: AuthDeps) {
  const { core, user } = deps
  return {
    checkAuth: () => checkAuth(deps),
    login: (username: string, password: string) => establishSession(deps, '/api/auth/login', { username, password }, false),
    register: (username: string, displayName: string, password: string, inviteCode: string) =>
      establishSession(deps, '/api/auth/register', { username, display_name: displayName, password, invite_code: inviteCode }, true),
    changePassword: (currentPassword: string, newPassword: string) => changePassword(deps, currentPassword, newPassword),
    logout: () => logout(deps),
    resetLocalSession() {
      core.advanceSession()
      user.value = null
      core.s.cookieAccountId = undefined
    }
  }
}
