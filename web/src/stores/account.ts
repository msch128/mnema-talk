import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '../lib/api'
import { decodeAuthAccountInfo, type AuthAccountInfo } from '../types/rest'

// The signed-in member's own sign-in security (GET /api/users/me/account):
// e-mail address and two-factor state. It is loaded when the dialog opens and
// dropped when it closes, so nothing outlives the dialog or the session. The
// sensitive actions (two-factor, export, deletion) are in lib/accountApi.
export const useAccountStore = defineStore('account', () => {
  const info = ref<AuthAccountInfo | null>(null)
  const dialogOpen = ref(false)

  async function load() {
    info.value = await api('/api/users/me/account', { decode: decodeAuthAccountInfo })
  }

  // The password, and while two-factor sign-in is on a current code.
  const proof = (currentPassword: string, code: string) => ({ current_password: currentPassword, ...(code ? { code } : {}) })

  /** Saves the address; returns false when the confirmation mail was refused (the address is saved anyway). */
  async function setEmail(email: string, currentPassword: string, code = ''): Promise<boolean> {
    const next = await api('/api/users/me/email', {
      method: 'PUT', decode: decodeAuthAccountInfo, json: { email, ...proof(currentPassword, code) }
    })
    info.value = next
    return next.confirmation_sent !== false
  }

  async function removeEmail(currentPassword: string, code = '') {
    info.value = await api('/api/users/me/email/remove', {
      method: 'POST', decode: decodeAuthAccountInfo, json: proof(currentPassword, code)
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
