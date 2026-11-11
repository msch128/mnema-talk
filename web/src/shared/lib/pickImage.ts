// The picture a person chose in a file input, for avatars and banners. The
// server accepts these up to 5 MB, so the limit is the same everywhere.
export const IMAGE_UPLOAD_MAX_MB = 5

export interface PickedImage {
  /** The chosen file; null when nothing was chosen or it is too large. */
  file: File | null
  tooBig: boolean
}

/**
 * Takes the first file of the input and clears the input, so choosing the
 * same file again fires `change` again (also after a refusal or a failure).
 */
export function pickImage(input: HTMLInputElement, maxMb: number = IMAGE_UPLOAD_MAX_MB): PickedImage {
  const file = input.files?.[0] ?? null
  input.value = ''
  if (!file) return { file: null, tooBig: false }
  if (file.size > maxMb * 1024 * 1024) return { file: null, tooBig: true }
  return { file, tooBig: false }
}
