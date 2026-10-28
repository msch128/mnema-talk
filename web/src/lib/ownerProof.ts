import { onMounted, ref } from 'vue'
import { api } from './api'
import { decodeAuthAccountInfo } from '../types/rest'

/** The confirmation the server asks of the Owner before taking over another account. */
export type OwnerProof = { current_password: string; code?: string }

export const ownerProof = (password: string, code: string): OwnerProof => ({ current_password: password, ...(code ? { code } : {}) })

/**
 * Whether the signed-in Owner has two-factor sign-in on, i.e. whether the
 * confirmation needs a code. Unknown (not loaded, or the lookup failed) counts
 * as on: asking for a code the server ignores costs less than refusing the
 * request.
 */
export function useOwnerNeedsCode() {
  const needsCode = ref(true)
  onMounted(async () => {
    try {
      const info = await api('/api/users/me/account', { decode: decodeAuthAccountInfo })
      if (info) needsCode.value = !!info.two_factor_enabled
    } catch (err) {
      void err
    }
  })
  return needsCode
}
