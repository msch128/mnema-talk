// Promise-based replacement for window.confirm(). A single <ConfirmDialog>
// host (mounted in App.vue) renders whatever is pending.
import { shallowRef } from 'vue'

export interface ConfirmRequest {
  title: string
  body?: string
  excerpt?: string
  confirmLabel?: string
  cancelLabel?: string
  danger?: boolean
}
export interface PendingConfirm extends ConfirmRequest { resolve: (result: boolean) => void }
export const pendingConfirm = shallowRef<PendingConfirm | null>(null)

/**
 * confirm({ title, body, confirmLabel, cancelLabel, danger }) → Promise<boolean>
 * Destructive dialogs pass danger: true and name the object in the title.
 */
export function confirm(options: ConfirmRequest): Promise<boolean> {
  // A second request while one is open cancels the first.
  pendingConfirm.value?.resolve(false)
  return new Promise<boolean>(resolve => {
    const entry: PendingConfirm = {
      ...options,
      resolve: result => {
        if (pendingConfirm.value === entry) pendingConfirm.value = null
        resolve(result)
      }
    }
    pendingConfirm.value = entry
  })
}
