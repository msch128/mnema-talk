import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { api, onUnauthorized } from '../lib/api'

// The session is an HttpOnly cookie set by the server; this store only keeps
// the current user. Nothing secret is ever stored in localStorage.
export const useAuthStore = defineStore('auth', () => {
  const user = ref(null)
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

  async function checkAuth() {
    try {
      user.value = await api('/api/auth/me')
      return true
    } catch {
      user.value = null
      return false
    }
  }

  async function login(username, password) {
    const data = await api('/api/auth/login', { method: 'POST', json: { username, password } })
    user.value = data.user
    return data.user
  }

  async function register(username, displayName, password, inviteCode) {
    const data = await api('/api/auth/register', {
      method: 'POST',
      json: { username, display_name: displayName, password, invite_code: inviteCode }
    })
    user.value = data.user
    return data.user
  }

  async function uploadAvatar(file) {
    const form = new FormData()
    form.append('avatar', file)
    user.value = await api('/api/users/me/avatar', { method: 'POST', form })
    return user.value
  }

  async function updateProfile({ displayName, bio }) {
    user.value = await api('/api/users/me/profile', {
      method: 'PUT',
      json: { display_name: displayName, bio }
    })
    return user.value
  }

  async function changePassword(currentPassword, newPassword) {
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
    login,
    register,
    uploadAvatar,
    updateProfile,
    changePassword,
    logout
  }
})
