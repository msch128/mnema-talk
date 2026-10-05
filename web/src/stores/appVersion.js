import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { CLIENT_VERSION, isNewServerVersion, normalizeVersion } from '../lib/appVersion'

// What the server says about its version (server_info on every WebSocket
// connection). When it differs from the version this page was built as, the
// app offers a reload; it never reloads on its own.
export const useAppVersionStore = defineStore('appVersion', () => {
  const clientVersion = CLIENT_VERSION
  const serverVersion = ref('')
  // The user closed the banner for this server version.
  const dismissedVersion = ref('')

  const reloadAvailable = computed(() => isNewServerVersion(serverVersion.value, clientVersion))
  const showReloadBanner = computed(() => reloadAvailable.value && dismissedVersion.value !== serverVersion.value)

  function setServerVersion(v) {
    serverVersion.value = normalizeVersion(v)
  }

  function dismiss() {
    dismissedVersion.value = serverVersion.value
  }

  return { clientVersion, serverVersion, reloadAvailable, showReloadBanner, setServerVersion, dismiss }
})
