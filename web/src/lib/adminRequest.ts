import { ref } from 'vue'

/** An admin console tab some other part of the UI asks for; App.vue opens it and clears the request. */
export const adminTabRequest = ref<string | null>(null)

export function requestAdminTab(tab: string) {
  adminTabRequest.value = tab
}
