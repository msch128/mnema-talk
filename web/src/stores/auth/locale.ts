import type { Ref } from 'vue'
import { api } from '../../lib/api'
import { decodeUser, type User, type Locale } from '../../types/domain'
import { setLocale, browserLocale, explicitLocale, SUPPORTED, isSupportedLocale } from '../../i18n'
import type { SessionCore } from './session'

function isCompatibleStoredLocale(stored: unknown, chosen: string | null): stored is Locale {
  if (typeof stored !== 'string' || !isSupportedLocale(stored)) return false
  return !chosen || chosen === stored
}

function shouldRollbackLocale(
  core: SessionCore,
  session: ReturnType<SessionCore['sessionSnapshot']>,
  revision: number,
  localeRevision: number,
  previous: unknown,
): previous is Locale {
  if (!core.isCurrentSession(session) || revision !== localeRevision) return false
  return typeof previous === 'string' && isSupportedLocale(previous)
}

/**
 * The UI language belongs to the account: applied after sign-in, saved to the
 * server and switched from the account menu.
 */
export function createAccountLocale(core: SessionCore, user: Ref<User | null>) {
  const { sessionSnapshot, isCurrentSession } = core
  let localeRevision = 0

  async function saveLocale(l: Locale, revision = ++localeRevision) {
    const session = sessionSnapshot()
    const currentUser = await api('/api/users/me/locale', { method: 'PUT', json: { locale: l }, decode: decodeUser })
    if (isCurrentSession(session) && currentUser.id === session.userId && revision === localeRevision) user.value = currentUser
    return currentUser
  }

  /**
   * A stored language wins; otherwise the browser decides (English for en-*,
   * else German) and the choice is saved so it follows the account to other devices.
   */
  function applyAccountLocale() {
    const stored = user.value?.locale
    // A language picked on the login screen beats the stored one and the browser.
    const chosen = explicitLocale()
    if (isCompatibleStoredLocale(stored, chosen)) {
      setLocale(stored)
      return
    }
    const picked = chosen || browserLocale()
    setLocale(picked)
    saveLocale(picked).catch(() => {
      // Not saved this time; the next session picks again.
    })
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
      if (shouldRollbackLocale(core, session, revision, localeRevision, previous)) setLocale(previous)
      throw err
    }
  }

  return { applyAccountLocale, changeLocale }
}
