import { defineStore } from 'pinia'
import { ref, computed } from 'vue'

export const useAuthStore = defineStore('auth', () => {
  const user = ref(null)
  const token = ref(localStorage.getItem('mnema_token') || '')
  const isAuthenticated = computed(() => !!token.value && !!user.value)
  const isAdmin = computed(() => user.value?.role === 'admin')

  async function checkAuth() {
    if (!token.value) return false
    try {
      const res = await fetch('/api/auth/me', {
        headers: { 'Authorization': `Bearer ${token.value}` }
      })
      if (res.ok) {
        user.value = await res.json()
        return true
      }
      logout()
      return false
    } catch {
      logout()
      return false
    }
  }

  async function login(username, password) {
    const res = await fetch('/api/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password })
    })

    if (!res.ok) {
      const err = await res.json().catch(() => ({}))
      throw new Error(err.error || 'Login fehlgeschlagen')
    }

    const data = await res.json()
    token.value = data.token
    user.value = data.user
    localStorage.setItem('mnema_token', data.token)
    return data.user
  }

  async function register(username, displayName, password, inviteCode) {
    const res = await fetch('/api/auth/register', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, display_name: displayName, password, invite_code: inviteCode })
    })

    if (!res.ok) {
      const err = await res.json().catch(() => ({}))
      throw new Error(err.error || 'Registrierung fehlgeschlagen')
    }

    const data = await res.json()
    token.value = data.token
    user.value = data.user
    localStorage.setItem('mnema_token', data.token)
    return data.user
  }

  function logout() {
    user.value = null
    token.value = ''
    localStorage.removeItem('mnema_token')
  }

  return {
    user,
    token,
    isAuthenticated,
    isAdmin,
    checkAuth,
    login,
    register,
    logout
  }
})
