import { ref, watch } from 'vue'

/** An admin console tab some other part of the UI asks for; App.vue opens it and clears the request. */
export const adminTabRequest = ref<string | null>(null)

export function requestAdminTab(tab: string) {
  adminTabRequest.value = tab
}

/** Hands each request to `open` once and clears it. */
export function watchAdminRequest(open: (tab: string) => void) {
  watch(adminTabRequest, tab => {
    if (!tab) return
    adminTabRequest.value = null
    open(tab)
  })
}
