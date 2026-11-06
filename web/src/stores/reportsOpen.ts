import { defineStore } from 'pinia'
import { ref, computed, watch } from 'vue'
import { api } from '../lib/api'
import { decodeChatOpenCountResponse } from '../types/rest'
import { usePermissionsStore } from './permissions'
import { useServerFeaturesStore } from './serverFeatures'

// How many reports are open, for the badges outside the admin console. Only a
// member who holds Manage reports may ask (anyone else would get a 403), so
// the count is read once when that permission is held, again after a socket
// reconnect, and then follows the reports_open event the server sends to such
// members only. Losing the permission or the reports feature clears it.
export const useReportsOpenStore = defineStore('reportsOpen', () => {
  const permissions = usePermissionsStore()
  const features = useServerFeaturesStore()
  const count = ref(0)
  let generation = 0

  const allowed = computed(() => features.reports && permissions.can('manage_reports'))
  /** What the badges show: nothing unless the member may act on it and something is open. */
  const visible = computed(() => (allowed.value ? count.value : 0))

  function set(open: number) {
    generation++
    count.value = Number.isFinite(open) && open > 0 ? Math.floor(open) : 0
  }

  async function load() {
    if (!allowed.value) return
    const mine = ++generation
    try {
      const res = await api('/api/admin/reports/open-count', { decode: decodeChatOpenCountResponse })
      if (mine === generation) count.value = res.open
    } catch {
      // The badge is a hint: keep what is shown, the next event or reconnect corrects it.
    }
  }

  watch(allowed, now => {
    if (now) void load()
    else set(0)
  }, { immediate: true })

  return { count, visible, set, load }
})
