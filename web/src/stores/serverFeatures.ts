import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api } from '../lib/api'
import { decodeServerHealth } from '../types/rest'

// The optional features of the server this app talks to (GET /api/health
// `features`). A 1.0.x server sends no list: every newer feature is then
// absent and its UI stays hidden. Names never change meaning
// (doc/api-stability.md).
export const useServerFeaturesStore = defineStore('serverFeatures', () => {
  const list = ref<string[]>([])
  // False until the first answer: a feature is only "missing" once the list is known.
  const loaded = ref(false)
  const names = computed(() => new Set(list.value))
  let pending: Promise<void> | null = null

  function has(name: string): boolean {
    return names.value.has(name)
  }

  /** Asks the server once; later calls share the answer. */
  function load(): Promise<void> {
    pending ??= api('/api/health', { decode: decodeServerHealth }).then(
      health => {
        list.value = health.features ?? []
        loaded.value = true
      },
      () => {
        // The next call asks again; nothing is shown that was not confirmed.
        pending = null
      }
    )
    return pending
  }

  return {
    list, loaded, has, load,
    dm: computed(() => has('dm')),
    blocks: computed(() => has('blocks')),
    reports: computed(() => has('reports'))
  }
})
