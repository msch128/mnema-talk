import { api } from './api'
import {
  decodeAuthExportStatus, decodeAuthRecoveryCodes, decodeAuthTwoFactorSetup,
  type AuthExportStatus, type AuthTwoFactorSetup
} from '../types/rest'

// Calls for the sensitive account actions. Each one proves who is asking with
// the current password and, where it applies, a current code; none keeps
// anything in the browser.

const withCode = (currentPassword: string, code: string) => ({ current_password: currentPassword, ...(code ? { code } : {}) })

/** Starts the setup: a new pending secret, nothing changes at sign-in yet. */
export function setupTwoFactor(currentPassword: string): Promise<AuthTwoFactorSetup> {
  return api('/api/users/me/2fa/setup', { method: 'POST', decode: decodeAuthTwoFactorSetup, json: { current_password: currentPassword } })
}

/** Confirms the first code and turns two-factor sign-in on; returns the recovery codes (shown once). */
export async function enableTwoFactor(code: string): Promise<string[]> {
  const out = await api('/api/users/me/2fa/enable', { method: 'POST', decode: decodeAuthRecoveryCodes, json: { code } })
  return out.recovery_codes
}

export async function disableTwoFactor(currentPassword: string, code: string): Promise<void> {
  await api('/api/users/me/2fa/disable', { method: 'POST', json: withCode(currentPassword, code) })
}

export async function newRecoveryCodes(currentPassword: string, code: string): Promise<string[]> {
  const out = await api('/api/users/me/2fa/recovery-codes', { method: 'POST', decode: decodeAuthRecoveryCodes, json: withCode(currentPassword, code) })
  return out.recovery_codes
}

/** Asks for an export of the member's own data (built in the background, one per day). */
export function requestExport(currentPassword: string, code: string): Promise<AuthExportStatus> {
  return api('/api/users/me/export', { method: 'POST', decode: decodeAuthExportStatus, json: withCode(currentPassword, code) })
}

export function exportStatus(): Promise<AuthExportStatus> {
  return api('/api/users/me/export', { decode: decodeAuthExportStatus })
}

/** Erases the account for good; the session ends with it. */
export async function deleteAccount(currentPassword: string, code: string): Promise<void> {
  await api('/api/users/me/delete', { method: 'POST', json: withCode(currentPassword, code) })
}
