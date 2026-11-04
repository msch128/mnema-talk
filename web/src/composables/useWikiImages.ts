// Pictures in the wiki editor: picked, pasted or dropped files are uploaded and
// their Markdown goes into the text at the cursor. The server redraws every
// picture (no metadata, at most 1600 pixels) and decides who may upload.
import { nextTick, ref, type Ref } from 'vue'
import { caughtErrorMessage } from '../lib/api'
import { uploadWikiImage } from '../lib/wikiApi'
import { t } from '../i18n'
import { useToastStore } from '../stores/toast'

const MAX_ALT = 60

/** A short description from the file name: no extension, no brackets. */
export function altFromName(name: string): string {
  return name.replace(/\.[^.]*$/, '').replace(/[[\]\n\r]/g, ' ').replace(/\s+/g, ' ').trim().slice(0, MAX_ALT)
}

const isPicture = (file: File) => /^image\/(png|jpeg|gif|webp)$/.test(file.type)

export function useWikiImages(area: Ref<HTMLTextAreaElement | null>, content: Ref<string>) {
  const toasts = useToastStore()
  const uploading = ref(0)

  async function insert(markdown: string) {
    const el = area.value
    const at = el?.selectionEnd ?? content.value.length
    content.value = content.value.slice(0, at) + markdown + content.value.slice(at)
    await nextTick()
    el?.focus()
    el?.setSelectionRange(at + markdown.length, at + markdown.length)
  }

  async function addFiles(files: Iterable<File>) {
    for (const file of files) {
      if (!isPicture(file)) {
        toasts.error(t('wiki.notAPicture'))
        continue
      }
      uploading.value++
      try {
        const image = await uploadWikiImage(file)
        await insert(`![${altFromName(file.name)}](${image.url})`)
      } catch (error) {
        toasts.error(caughtErrorMessage(error, t('wiki.imageFailed')))
      } finally {
        uploading.value--
      }
    }
  }

  function takeFiles(event: ClipboardEvent | DragEvent, transfer: DataTransfer | null) {
    const files = [...(transfer?.files ?? [])]
    if (files.length === 0) return
    event.preventDefault()
    void addFiles(files)
  }

  return {
    uploading,
    addFiles,
    onPaste: (event: ClipboardEvent) => takeFiles(event, event.clipboardData),
    onDrop: (event: DragEvent) => takeFiles(event, event.dataTransfer)
  }
}
