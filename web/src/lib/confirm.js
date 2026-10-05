// Promise-based replacement for window.confirm(). A single <ConfirmDialog>
// host (mounted in App.vue) renders whatever is pending.
import { shallowRef } from 'vue'

export const pendingConfirm = shallowRef(null)

/**
 * confirm({ title, body, confirmLabel, cancelLabel, danger }) → Promise<boolean>
 * Destructive dialogs pass danger: true and name the object in the title.
 */
export function confirm(options) {
  // A second request while one is open cancels the first.
  pendingConfirm.value?.resolve(false)
  return new Promise(resolve => {
    const entry = {
      ...options,
      resolve: result => {
        if (pendingConfirm.value === entry) pendingConfirm.value = null
        resolve(result)
      }
    }
    pendingConfirm.value = entry
  })
}

export function useConfirm() {
  return { confirm }
}
