import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '../lib/api'
import { decodeAuthAccountInfo, type AuthAccountInfo } from '../types/rest'

// The signed-in member's own sign-in security (GET /api/users/me/account):
// e-mail address and its confirmation. It is loaded when the dialog opens and
// dropped when it closes, so nothing outlives the dialog or the session.
export const useAccountStore = defineStore('account', () => {
  const info = ref<AuthAccountInfo | null>(null)
  const dialogOpen = ref(false)

  async function load() {
    info.value = await api('/api/users/me/account', { decode: decodeAuthAccountInfo })
  }

  async function setEmail(email: string, currentPassword: string) {
    info.value = await api('/api/users/me/email', {
      method: 'PUT', decode: decodeAuthAccountInfo, json: { email, current_password: currentPassword }
    })
  }

  async function removeEmail(currentPassword: string) {
    info.value = await api('/api/users/me/email/remove', {
      method: 'POST', decode: decodeAuthAccountInfo, json: { current_password: currentPassword }
    })
  }

  async function resendConfirmation() {
    await api('/api/users/me/email/resend', { method: 'POST' })
  }

  function open() {
    dialogOpen.value = true
  }

  function close() {
    dialogOpen.value = false
    info.value = null
  }

  return { info, dialogOpen, load, setEmail, removeEmail, resendConfirmation, open, close }
})
