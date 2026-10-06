import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { api } from '../lib/api'
import { CLIENT_VERSION, isNewServerVersion, normalizeVersion } from '../lib/appVersion'

// What the server says about its version (server_info on every WebSocket
// connection). When it differs from the version this page was built as, the
// app offers a reload; it never reloads on its own.
//
// For admins it also holds the release check (GET /api/admin/system/update),
// which drives the "update available" badge on the admin console entry.
export const ADMIN_UPDATE_POLL_MS = 30 * 60 * 1000

export const useAppVersionStore = defineStore('appVersion', () => {
  const clientVersion = CLIENT_VERSION
  const serverVersion = ref('')
  // The user closed the banner for this server version.
  const dismissedVersion = ref('')
  const adminUpdate = ref(null)

  const reloadAvailable = computed(() => isNewServerVersion(serverVersion.value, clientVersion))
  const showReloadBanner = computed(() => reloadAvailable.value && dismissedVersion.value !== serverVersion.value)
  const adminUpdateAvailable = computed(() => adminUpdate.value?.update_available === true)

  function setServerVersion(v) {
    serverVersion.value = normalizeVersion(v)
  }

  function dismiss() {
    dismissedVersion.value = serverVersion.value
  }

  function setAdminUpdate(status) {
    adminUpdate.value = status && typeof status === 'object' ? status : null
  }

  /** Admins only: reads what the server's last release check found (no GitHub request). */
  async function refreshAdminUpdate() {
    try {
      setAdminUpdate(await api('/api/admin/system/update'))
    } catch {
      // Not an admin any more, or the server is restarting: keep the last state.
    }
  }

  // The server checks GitHub every 30 minutes; reading its result is cheap,
  // so admins follow it at the same pace (and on every reconnect).
  let poll = null
  function followAdminUpdates(isAdmin) {
    clearInterval(poll)
    poll = null
    if (!isAdmin()) {
      setAdminUpdate(null)
      return
    }
    refreshAdminUpdate()
    poll = setInterval(() => {
      if (isAdmin()) refreshAdminUpdate()
      else followAdminUpdates(isAdmin)
    }, ADMIN_UPDATE_POLL_MS)
  }

  return {
    clientVersion, serverVersion, reloadAvailable, showReloadBanner, setServerVersion, dismiss,
    adminUpdate, adminUpdateAvailable, setAdminUpdate, refreshAdminUpdate, followAdminUpdates
  }
})
