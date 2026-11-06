// The cover of an event form: a picked file only shows a preview, Remove only marks the
// stored cover for removal. Both are sent on Save; `marker` is what the form compares.
import { computed, onBeforeUnmount, ref } from 'vue'

export function useEventCover(url: () => string | null | undefined) {
  const file = ref<File | null>(null)
  const preview = ref<string | null>(url() || null)
  const removeStored = ref(false)
  const marker = computed(() => (file.value ? `file:${file.value.name}:${file.value.size}:${file.value.lastModified}` : removeStored.value ? 'remove' : ''))

  // The preview of a picked file is an object URL that must be released again.
  let objectUrl: string | null = null
  function release() {
    if (objectUrl) URL.revokeObjectURL(objectUrl)
    objectUrl = null
  }
  onBeforeUnmount(release)

  function pick(e: Event) {
    const picked = (e.target as HTMLInputElement).files?.[0]
    if (!picked) return
    release()
    file.value = picked
    objectUrl = URL.createObjectURL(picked)
    preview.value = objectUrl
    removeStored.value = false
  }

  function remove() {
    release()
    file.value = null
    preview.value = null
    if (url()) removeStored.value = true
  }

  function reset() {
    release()
    file.value = null
    removeStored.value = false
    preview.value = url() || null
  }

  return { file, preview, removeStored, marker, pick, remove, reset }
}
