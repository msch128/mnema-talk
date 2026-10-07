import { defineStore } from 'pinia'
import { ref, computed, onScopeDispose } from 'vue'
import { api, ApiError, onUnauthorized } from '../lib/api'
import { decodeUser, decodeUserEnvelope, type User, type Locale } from '../types/domain'
import { isRecord } from '../types/validation'
import { t, setLocale, browserLocale, explicitLocale, SUPPORTED, isSupportedLocale } from '../i18n'

// The session is an HttpOnly cookie set by the server; this store only keeps
// the current user. Nothing secret is ever stored in localStorage.
export const useAuthStore = defineStore('auth', () => {
  const user = ref<User | null>(null)
  const isAuthenticated = computed(() => !!user.value)
  const isAdmin = computed(() => user.value?.role === 'admin')

  // A response belongs to the session that started its request. Rotating
  // listeners also makes old requests' captured 401 callbacks harmless.
  let sessionGeneration = 0
  let unauthorizedGeneration: number | null = null
  let localeRevision = 0
  let cookieRequests: Promise<void> = Promise.resolve()
  let cookieAccountId: string | null | undefined
  let removeUnauthorized: () => boolean = () => false

  function advanceSession() {
    sessionGeneration++
    unauthorizedGeneration = null
    removeUnauthorized()
    const generation = sessionGeneration
    removeUnauthorized = onUnauthorized(() => {
      // api invokes a request snapshot only while this exact callback is
      // still registered; every generation change removes it synchronously.
      advanceSession()
      unauthorizedGeneration = generation
      user.value = null
    })
    return sessionGeneration
  }

  function sessionSnapshot() {
    return { generation: sessionGeneration, userId: user.value?.id ?? null }
  }

  function isCurrentSession(snapshot: ReturnType<typeof sessionSnapshot>) {
    return snapshot.generation === sessionGeneration && snapshot.userId === (user.value?.id ?? null)
  }

  function clearMismatchedCookieAccount(session: ReturnType<typeof sessionSnapshot>) {
    // An earlier queued sign-in may have set a different cookie while its
    // UI result was superseded. A failed newest intent must not retain the
    // previous account's UI; a fresh /me can establish the actual session.
    if (isCurrentSession(session) && cookieAccountId !== undefined && cookieAccountId !== session.userId) {
      advanceSession()
      user.value = null
    }
  }

  // Cookie-changing responses must arrive in invocation order too: a late
  // logout must not clear a newer login's HttpOnly cookie.
  function queueCookieRequest<T>(request: () => Promise<T>): Promise<T> {
    const pending = cookieRequests.then(request)
    cookieRequests = pending.then(() => {}, () => {})
    return pending
  }

  advanceSession()
  onScopeDispose(() => {
    sessionGeneration++
    removeUnauthorized()
  })

  // Remove the token kept by earlier versions of the app.
  try {
    localStorage.removeItem('mnema_token')
  } catch {
    // storage unavailable
  }

  /**
   * Asks the server who is logged in. Returns true (signed in), false (no or
   * revoked session: the user is cleared) or null when the server could not
   * answer (offline, restarting): the user is kept so a redeploy or a network
   * blip never logs anyone out.
   */
  async function checkAuth() {
    const session = sessionSnapshot()
    try {
      await cookieRequests
      if (!isCurrentSession(session)) return null
      const currentUser = await api('/api/auth/me', { decode: decodeUser, shouldNotifyUnauthorized: () => isCurrentSession(session) })
      if (!isCurrentSession(session)) return null
      cookieAccountId = currentUser.id
      if (currentUser.id !== session.userId) advanceSession()
      user.value = currentUser
      applyAccountLocale()
      return true
    } catch (err) {
      if (isRecord(err) && err['status'] === 401) {
        // api already invalidated the current generation before rejecting.
        return unauthorizedGeneration === session.generation && sessionGeneration === session.generation + 1
          ? false : null
      }
      return null
    }
  }

  async function login(username: string, password: string) {
    advanceSession()
    const session = sessionSnapshot()
    return queueCookieRequest(async () => {
      try {
        const data = await api('/api/auth/login', {
          method: 'POST', json: { username, password }, decode: decodeUserEnvelope
        })
        cookieAccountId = data.user.id
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

  async function register(username: string, displayName: string, password: string, inviteCode: string) {
    advanceSession()
    const session = sessionSnapshot()
    return queueCookieRequest(async () => {
      try {
        const data = await api('/api/auth/register', {
          method: 'POST',
          decode: decodeUserEnvelope,
          shouldNotifyUnauthorized: () => isCurrentSession(session),
          json: { username, display_name: displayName, password, invite_code: inviteCode }
        })
        cookieAccountId = data.user.id
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

  /**
   * The UI language belongs to the account. A stored language wins; otherwise
   * the browser decides (English for en-*, else German) and the choice is
   * saved so it follows the account to other devices.
   */
  function applyAccountLocale() {
    const stored = user.value?.locale
    // A language picked on the login screen beats the stored one and the browser.
    const chosen = explicitLocale()
    if (typeof stored === 'string' && isSupportedLocale(stored) && (!chosen || chosen === stored)) {
      setLocale(stored)
      return
    }
    const picked = chosen || browserLocale()
    setLocale(picked)
    saveLocale(picked).catch(() => {
      // Not saved this time; the next session picks again.
    })
  }

  async function saveLocale(l: Locale, revision = ++localeRevision) {
    const session = sessionSnapshot()
    const currentUser = await api('/api/users/me/locale', { method: 'PUT', json: { locale: l }, decode: decodeUser })
    if (isCurrentSession(session) && currentUser.id === session.userId && revision === localeRevision) user.value = currentUser
    return currentUser
  }

  /** Language switcher in the account menu. */
  async function changeLocale(l: Locale) {
    if (!SUPPORTED.includes(l)) return
    const session = sessionSnapshot()
    const revision = ++localeRevision
    const previous = user.value?.locale
    setLocale(l)
    try {
      await saveLocale(l, revision)
    } catch (err) {
      if (isCurrentSession(session) && revision === localeRevision && typeof previous === 'string' && isSupportedLocale(previous)) setLocale(previous)
      throw err
    }
  }

  async function uploadAvatar(file: File) {
    const session = sessionSnapshot()
    const form = new FormData()
    form.append('avatar', file)
    const currentUser = await api('/api/users/me/avatar', { method: 'POST', form, decode: decodeUser })
    if (isCurrentSession(session) && currentUser.id === session.userId) user.value = currentUser
    return currentUser
  }

  async function updateProfile({ displayName, bio }: { displayName: string; bio: string }) {
    const session = sessionSnapshot()
    const currentUser = await api('/api/users/me/profile', {
      method: 'PUT',
      decode: decodeUser,
      json: { display_name: displayName, bio }
    })
    if (isCurrentSession(session) && currentUser.id === session.userId) user.value = currentUser
    return currentUser
  }

  async function changePassword(currentPassword: string, newPassword: string) {
    const requestedSession = sessionSnapshot()
    await queueCookieRequest(async () => {
      // An earlier queued login may have changed the cookie's account. Never
      // send this account's password change against a replacement session.
      if (!isCurrentSession(requestedSession) || (cookieAccountId !== undefined && cookieAccountId !== requestedSession.userId)) {
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

  async function logout() {
    advanceSession()
    user.value = null
    const session = sessionSnapshot()
    await queueCookieRequest(async () => {
      await api('/api/auth/logout', {
        method: 'POST', shouldNotifyUnauthorized: () => isCurrentSession(session)
      })
      cookieAccountId = null
    })
  }

  return {
    user,
    isAuthenticated,
    isAdmin,
    checkAuth,
    changeLocale,
    login,
    register,
    uploadAvatar,
    updateProfile,
    changePassword,
    logout
  }
})
