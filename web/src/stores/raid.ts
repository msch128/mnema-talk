import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api } from '../lib/api'
import { decodeRaidSettings } from '../types/rest'
import type { RaidInput, RaidLockdownInput, RaidSettings } from '../types/rest'

// Raid protection as the Owner and Administrators see it (GET /api/admin/raid):
// the sign-up limit, the waiting times of new members and the lockdown. The
// admin tab edits it and the banner under the header shows a running lockdown.
export const useRaidStore = defineStore('raid', () => {
  const settings = ref<RaidSettings | null>(null)
  const failed = ref(false)

  const lockdown = computed(() => settings.value?.lockdown ?? null)
  const lockdownActive = computed(() => lockdown.value?.active === true)

  async function load() {
    try {
      settings.value = await api('/api/admin/raid', { decode: decodeRaidSettings })
      failed.value = false
    } catch {
      failed.value = true
    }
  }

  async function save(input: RaidInput) {
    settings.value = await api('/api/admin/raid', { method: 'PUT', json: input, decode: decodeRaidSettings })
  }

  async function setLockdown(input: RaidLockdownInput) {
    settings.value = await api('/api/admin/raid/lockdown', { method: 'PUT', json: input, decode: decodeRaidSettings })
  }

  function reset() {
    settings.value = null
    failed.value = false
  }

  return { settings, failed, lockdown, lockdownActive, load, save, setLockdown, reset }
})
