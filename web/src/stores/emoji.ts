import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api } from '../lib/api'
import { decodeEmojiEmoji, decodeEmojiList } from '../types/rest'
import { setEmojiSource } from '../lib/customEmoji'
import type { CustomEmoji } from '../lib/customEmoji'

/** True when the member asked the system for less motion. */
export function prefersReducedMotion(): boolean {
  return typeof window !== 'undefined' && typeof window.matchMedia === 'function'
    && window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

// The instance's custom emoji. The server tells every client when the list
// changes (emoji_changed), so it is only ever fetched, never patched locally.
export const useEmojiStore = defineStore('emoji', () => {
  const items = ref<CustomEmoji[]>([])
  const limit = ref(0)
  const byId = computed(() => new Map(items.value.map(e => [e.id, e])))
  const byName = computed(() => new Map(items.value.map(e => [e.name, e])))

  setEmojiSource(id => byId.value.get(id), prefersReducedMotion)

  async function load() {
    try {
      const list = await api('/api/emoji', { decode: decodeEmojiList })
      items.value = list.emoji.map(e => ({ id: e.id, name: e.name, animated: e.animated, url: e.url, ...(e.still_url ? { still_url: e.still_url } : {}) }))
      limit.value = list.limit
    } catch {
      // The list on screen stays; the next emoji_changed or reconnect asks again.
    }
  }

  async function upload(name: string, file: File) {
    const form = new FormData()
    form.append('name', name)
    form.append('file', file)
    await api('/api/admin/emoji', { method: 'POST', form, decode: decodeEmojiEmoji })
    await load()
  }

  async function rename(id: string, name: string) {
    await api(`/api/admin/emoji/${id}`, { method: 'PATCH', json: { name }, decode: decodeEmojiEmoji })
    await load()
  }

  async function remove(id: string) {
    await api(`/api/admin/emoji/${id}`, { method: 'DELETE' })
    await load()
  }

  return { items, limit, byId, byName, load, upload, rename, remove }
})
