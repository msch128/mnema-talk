// An image (icon, avatar) that is one field of a draft (doc/settings-ux.md):
// picking a file only previews it and Remove only marks it; the screen's Save
// uploads or deletes it. Nothing is written here.
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { IMAGE_UPLOAD_MAX_MB, pickImage } from '../lib/pickImage'
import { useToastStore } from '../../stores/toast'
import { t } from '../../i18n'

/** The staged change: a new file, 'remove' for "no image", or null for "keep what is saved". */
export type ImageChange = File | 'remove' | null

/** Compares staged images by what they are, so the same choice counts as unchanged. */
export function normaliseImage(value: ImageChange): string | null {
  return value instanceof File ? `file:${value.name}:${value.size}:${value.lastModified}` : value
}

export interface StagedImageOptions {
  /** The draft field. */
  get: () => ImageChange
  set: (value: ImageChange) => void
  /** The URL of the saved image, '' when there is none. */
  savedUrl: () => string
  /** Shown when a file is over the upload limit. */
  tooBigMessage?: () => string
}

export function useStagedImage(options: StagedImageOptions) {
  const toasts = useToastStore()
  const objectUrl = ref('')

  const revoke = () => {
    if (objectUrl.value) URL.revokeObjectURL(objectUrl.value)
    objectUrl.value = ''
  }
  watch(options.get, value => {
    revoke()
    if (value instanceof File) objectUrl.value = URL.createObjectURL(value)
  }, { immediate: true })
  onBeforeUnmount(revoke)

  /** What the screen shows: the picked file, nothing once removed, else the saved image. */
  const previewUrl = computed(() => {
    const value = options.get()
    if (value instanceof File) return objectUrl.value
    return value === 'remove' ? '' : options.savedUrl()
  })

  /** The change handler of the file input. */
  function pick(event: Event) {
    const { file, tooBig } = pickImage(event.target as HTMLInputElement)
    if (tooBig) toasts.error(options.tooBigMessage?.() ?? t('profile.avatarTooBig', { size: IMAGE_UPLOAD_MAX_MB }))
    if (file) options.set(file)
  }

  /** Marks the image for removal; dropping a picked file with nothing saved is simply no change. */
  function remove() {
    options.set(options.savedUrl() ? 'remove' : null)
  }

  return { previewUrl, pick, remove }
}
