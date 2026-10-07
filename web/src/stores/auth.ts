import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { api, onUnauthorized } from '../lib/api'
import { decodeUser, decodeUserEnvelope, type User, type Locale } from '../types/domain'
import { isRecord } from '../types/validation'
import { setLocale, browserLocale, explicitLocale, SUPPORTED, isSupportedLocale } from '../i18n'

// The session is an HttpOnly cookie set by the server; this store only keeps
// the current user. Nothing secret is ever stored in localStorage.
export const useAuthStore = defineStore('auth', () => {
  const user = ref<User | null>(null)
  const isAuthenticated = computed(() => !!user.value)
  const isAdmin = computed(() => user.value?.role === 'admin')

  // Any 401 (expired or revoked session) drops back to the login screen.
  onUnauthorized(() => {
    user.value = null
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
    try {
      user.value = await api('/api/auth/me', { decode: decodeUser })
      applyAccountLocale()
      return true
    } catch (err) {
      if (isRecord(err) && err['status'] === 401) {
        user.value = null
        return false
      }
      return null
    }
  }

  async function login(username: string, password: string) {
    const data = await api('/api/auth/login', { method: 'POST', json: { username, password }, decode: decodeUserEnvelope })
    user.value = data.user
    applyAccountLocale()
    return data.user
  }

  async function register(username: string, displayName: string, password: string, inviteCode: string) {
    const data = await api('/api/auth/register', {
      method: 'POST',
      decode: decodeUserEnvelope,
      json: { username, display_name: displayName, password, invite_code: inviteCode }
    })
    user.value = data.user
    applyAccountLocale()
    return data.user
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

  async function saveLocale(l: Locale) {
    user.value = await api('/api/users/me/locale', { method: 'PUT', json: { locale: l }, decode: decodeUser })
    return user.value
  }

  /** Language switcher in the account menu. */
  async function changeLocale(l: Locale) {
    if (!SUPPORTED.includes(l)) return
    const previous = user.value?.locale
    setLocale(l)
    try {
      await saveLocale(l)
    } catch (err) {
      if (typeof previous === 'string' && isSupportedLocale(previous)) setLocale(previous)
      throw err
    }
  }

  async function uploadAvatar(file: File) {
    const form = new FormData()
    form.append('avatar', file)
    user.value = await api('/api/users/me/avatar', { method: 'POST', form, decode: decodeUser })
    return user.value
  }

  async function updateProfile({ displayName, bio }: { displayName: string; bio: string }) {
    user.value = await api('/api/users/me/profile', {
      method: 'PUT',
      decode: decodeUser,
      json: { display_name: displayName, bio }
    })
    return user.value
  }

  async function changePassword(currentPassword: string, newPassword: string) {
    await api('/api/auth/password', {
      method: 'PUT',
      json: { current_password: currentPassword, new_password: newPassword }
    })
  }

  async function logout() {
    try {
      await api('/api/auth/logout', { method: 'POST' })
    } finally {
      user.value = null
    }
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
