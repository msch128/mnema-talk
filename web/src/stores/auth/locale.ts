import type { Ref } from 'vue'
import { api } from '../../lib/api'
import { decodeUser, type User, type Locale } from '../../types/domain'
import { setLocale, browserLocale, explicitLocale, SUPPORTED, isSupportedLocale } from '../../i18n'
import type { SessionCore } from './session'

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

  return { applyAccountLocale, changeLocale }
}
