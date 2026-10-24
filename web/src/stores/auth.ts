import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { api } from '../lib/api'
import { decodeUser, type User } from '../types/domain'
import { createSessionCore } from './auth/session'
import { createAccountLocale } from './auth/locale'
import { createAuthActions } from './auth/credentials'

// The session is an HttpOnly cookie set by the server; this store only keeps
// the current user. Nothing secret is ever stored in localStorage.
export const useAuthStore = defineStore('auth', () => {
  const user = ref<User | null>(null)
  const isAuthenticated = computed(() => !!user.value)
  const isAdmin = computed(() => user.value?.role === 'admin')

  const core = createSessionCore(user)
  const { applyAccountLocale, changeLocale } = createAccountLocale(core, user)
  const actions = createAuthActions({ core, user, applyAccountLocale })

  // Remove the token kept by earlier versions of the app.
  try {
    localStorage.removeItem('mnema_token')
  } catch (error) {
    void error
    // storage unavailable
  }

  async function uploadAvatar(file: File) {
    const session = core.sessionSnapshot()
    const form = new FormData()
    form.append('avatar', file)
    const currentUser = await api('/api/users/me/avatar', { method: 'POST', form, decode: decodeUser })
    if (core.isCurrentSession(session) && currentUser.id === session.userId) user.value = currentUser
    return currentUser
  }

  async function removeAvatar() {
    const session = core.sessionSnapshot()
    const currentUser = await api('/api/users/me/avatar', { method: 'DELETE', decode: decodeUser })
    if (core.isCurrentSession(session) && currentUser.id === session.userId) user.value = currentUser
    return currentUser
  }

  async function uploadBanner(file: File) {
    const session = core.sessionSnapshot()
    const form = new FormData()
    form.append('banner', file)
    const currentUser = await api('/api/users/me/banner', { method: 'PUT', form, decode: decodeUser })
    if (core.isCurrentSession(session) && currentUser.id === session.userId) user.value = currentUser
    return currentUser
  }

  async function removeBanner() {
    const session = core.sessionSnapshot()
    const currentUser = await api('/api/users/me/banner', { method: 'DELETE', decode: decodeUser })
    if (core.isCurrentSession(session) && currentUser.id === session.userId) user.value = currentUser
    return currentUser
  }

  async function updateProfile({ displayName, bio }: { displayName: string; bio: string }) {
    const session = core.sessionSnapshot()
    const currentUser = await api('/api/users/me/profile', {
      method: 'PUT',
      decode: decodeUser,
      json: { display_name: displayName, bio }
    })
    if (core.isCurrentSession(session) && currentUser.id === session.userId) user.value = currentUser
    return currentUser
  }

  return {
    user,
    isAuthenticated,
    isAdmin,
    checkAuth: actions.checkAuth,
    changeLocale,
    login: actions.login,
    secondStep: actions.secondStep,
    register: actions.register,
    uploadAvatar,
    removeAvatar,
    uploadBanner,
    removeBanner,
    updateProfile,
    changePassword: actions.changePassword,
    logout: actions.logout,
    resetLocalSession: actions.resetLocalSession
  }
})
