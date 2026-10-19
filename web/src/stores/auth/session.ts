import type { Ref } from 'vue'
import { onScopeDispose } from 'vue'
import { onUnauthorized } from '../../lib/api'
import type { User } from '../../types/domain'

/**
 * What the auth store tracks about the session. A response belongs to the
 * session that started its request: every generation change drops the 401
 * listener of the one before, so old requests' captured callbacks are harmless.
 */
export interface SessionState {
  generation: number
  unauthorizedGeneration: number | null
  cookieRequests: Promise<void>
  cookieAccountId: string | null | undefined
  removeUnauthorized: () => boolean
}

export type SessionSnapshot = { generation: number; userId: string | null }

export interface SessionCore {
  s: SessionState
  advanceSession(): number
  sessionSnapshot(): SessionSnapshot
  isCurrentSession(snapshot: SessionSnapshot): boolean
  clearMismatchedCookieAccount(session: SessionSnapshot): void
  queueCookieRequest<T>(request: () => Promise<T>): Promise<T>
}

/** Starts the first session generation and ends it with the store's scope. */
export function createSessionCore(user: Ref<User | null>): SessionCore {
  const s: SessionState = {
    generation: 0,
    unauthorizedGeneration: null,
    cookieRequests: Promise.resolve(),
    cookieAccountId: undefined,
    removeUnauthorized: () => false
  }

  function advanceSession() {
    s.generation++
    s.unauthorizedGeneration = null
    s.removeUnauthorized()
    const generation = s.generation
    s.removeUnauthorized = onUnauthorized(() => {
      // api invokes a request snapshot only while this exact callback is
      // still registered; every generation change removes it synchronously.
      advanceSession()
      s.unauthorizedGeneration = generation
      user.value = null
    })
    return s.generation
  }

  function sessionSnapshot(): SessionSnapshot {
    return { generation: s.generation, userId: user.value?.id ?? null }
  }

  function isCurrentSession(snapshot: SessionSnapshot) {
    return snapshot.generation === s.generation && snapshot.userId === (user.value?.id ?? null)
  }

  function clearMismatchedCookieAccount(session: SessionSnapshot) {
    // An earlier queued sign-in may have set a different cookie while its
    // UI result was superseded. A failed newest intent must not retain the
    // previous account's UI; a fresh /me can establish the actual session.
    if (isCurrentSession(session) && s.cookieAccountId !== undefined && s.cookieAccountId !== session.userId) {
      advanceSession()
      user.value = null
    }
  }

  // Cookie-changing responses must arrive in invocation order too: a late
  // logout must not clear a newer login's HttpOnly cookie.
  function queueCookieRequest<T>(request: () => Promise<T>): Promise<T> {
    const pending = s.cookieRequests.then(request)
    s.cookieRequests = pending.then(() => {}, () => {})
    return pending
  }

  advanceSession()
  onScopeDispose(() => {
    s.generation++
    s.removeUnauthorized()
  })

  return { s, advanceSession, sessionSnapshot, isCurrentSession, clearMismatchedCookieAccount, queueCookieRequest }
}
