import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '../lib/api'
import {
  decodeAuthAccountInfo, decodeAuthRecoveryCodes, decodeAuthTwoFactorSetup,
  type AuthAccountInfo, type AuthTwoFactorSetup
} from '../types/rest'

// The signed-in member's own sign-in security (GET /api/users/me/account):
// e-mail address and two-factor sign-in. It is loaded when the dialog opens and
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

  /** Starts the setup: a new pending secret, nothing changes at sign-in yet. */
  function setupTwoFactor(currentPassword: string): Promise<AuthTwoFactorSetup> {
    return api('/api/users/me/2fa/setup', {
      method: 'POST', decode: decodeAuthTwoFactorSetup, json: { current_password: currentPassword }
    })
  }

  /** Confirms the first code, turns two-factor sign-in on and returns the recovery codes (shown once). */
  async function enableTwoFactor(code: string): Promise<string[]> {
    const out = await api('/api/users/me/2fa/enable', { method: 'POST', decode: decodeAuthRecoveryCodes, json: { code } })
    await load()
    return out.recovery_codes
  }

  async function disableTwoFactor(currentPassword: string, code: string) {
    await api('/api/users/me/2fa/disable', { method: 'POST', json: { current_password: currentPassword, code } })
    await load()
  }

  async function newRecoveryCodes(currentPassword: string, code: string): Promise<string[]> {
    const out = await api('/api/users/me/2fa/recovery-codes', {
      method: 'POST', decode: decodeAuthRecoveryCodes, json: { current_password: currentPassword, code }
    })
    await load()
    return out.recovery_codes
  }

  function open() {
    dialogOpen.value = true
  }

  function close() {
    dialogOpen.value = false
    info.value = null
  }

  return {
    info, dialogOpen, load, setEmail, removeEmail, resendConfirmation,
    setupTwoFactor, enableTwoFactor, disableTwoFactor, newRecoveryCodes, open, close
  }
})
