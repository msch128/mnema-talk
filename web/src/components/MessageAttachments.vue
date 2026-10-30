<script setup lang="ts">
import type { PropType } from 'vue'
import type { MediaAttachment, Message } from '../types/domain'
// Attachments under a message. In chat a single image shows on its own at its
// natural aspect ratio (no card, no file row); two or more form a mosaic of
// fixed shape. Images open the media viewer with all of the message's images.
// Order: images, then videos, then files, each in server order. Videos and
// files keep the card with the file name and size.
import { computed } from 'vue'
import { FileText, Maximize2 } from '@lucide/vue'
import AttachmentImage from './AttachmentImage.vue'
import { formatFileSize } from '../lib/fileSize'
import { locale } from '../i18n'
import { openViewer, viewerMessageFrom } from '../composables/useMediaViewer'
import { useChatStore } from '../stores/chat'
import { galleryRows, GALLERY_CELL } from '../lib/galleryLayout'

const props = defineProps({
  attachments: { type: Array as PropType<MediaAttachment[]>, default: () => [] },
  // 'chat': channel and Talk chat, 'reply': thread replies, 'root': thread root card.
  variant: { type: String as PropType<'chat' | 'reply' | 'root'>, default: 'chat' },
  // The message the files belong to; the viewer shows who sent it and can jump to it.
  message: { type: Object as PropType<Message>, default: undefined }
})

const STYLES = {
  chat: {
    list: 'mt-2 space-y-2',
    gallery: 'w-full max-w-md',
    card: 'max-w-md rounded-lg overflow-hidden border border-mnema-border bg-mnema-elevated shadow-xs',
    bareImage: 'w-fit max-w-md',
    imageButton: 'block max-w-full rounded-lg overflow-hidden',
    image: 'block h-auto max-h-80 min-h-24 w-auto max-w-full object-cover cursor-pointer hover:opacity-95 transition',
    gone: 'h-40 w-72 max-w-full',
    video: 'max-h-80 w-full rounded-t',
    footer: 'p-2.5 flex items-center justify-between text-sm bg-mnema-raised border-t border-mnema-hairline',
    name: 'flex items-center gap-2 truncate',
    icon: 'w-4 h-4 text-mnema-tertiary shrink-0',
    link: 'text-mnema-text hover:text-mnema-accent hover:underline truncate text-sm',
    size: 'text-xs font-mono text-mnema-tertiary pl-2 shrink-0'
  },
  reply: {
    list: 'mt-1.5 space-y-1.5',
    gallery: 'w-full max-w-xs',
    card: 'rounded-lg overflow-hidden border border-mnema-border bg-mnema-elevated',
    bareImage: '',
    imageButton: 'block w-full',
    image: 'max-h-40 w-full object-cover cursor-pointer hover:opacity-90 transition',
    gone: 'h-24 w-full',
    video: 'max-h-40 w-full',
    footer: 'p-1.5 flex items-center justify-between text-xs bg-mnema-raised border-t border-mnema-hairline',
    name: 'flex items-center gap-1.5 truncate',
    icon: 'w-3.5 h-3.5 text-mnema-tertiary shrink-0',
    link: 'text-mnema-text hover:text-mnema-accent hover:underline truncate',
    size: 'text-xs font-mono text-mnema-tertiary'
  },
  root: {
    list: 'space-y-1.5 pt-1',
    gallery: 'w-full',
    card: 'rounded-lg overflow-hidden border border-mnema-border bg-mnema-surface/40',
    bareImage: '',
    imageButton: 'block w-full',
    image: 'max-h-48 w-full object-cover cursor-pointer hover:opacity-90 transition',
    gone: 'h-28 w-full',
    video: 'max-h-48 w-full',
    footer: 'p-2 flex items-center justify-between text-xs',
    name: 'flex items-center gap-1.5 truncate',
    icon: 'w-3.5 h-3.5 text-mnema-tertiary shrink-0',
    link: 'text-mnema-text hover:text-mnema-accent hover:underline truncate',
    size: 'text-xs font-mono text-mnema-tertiary'
  }
}

const s = computed(() => STYLES[props.variant] || STYLES.chat)

const kind = (att: MediaAttachment) => (att.mime_type?.startsWith('image/') ? 'image' : att.mime_type?.startsWith('video/') ? 'video' : 'file')
// Only the chat layout shows images without the card and file row.
const bare = (att: MediaAttachment) => kind(att) === 'image' && s.value.bareImage !== ''
const images = computed(() => props.attachments.filter(a => kind(a) === 'image'))
// From two images on they form the mosaic; the rest (a lone image, videos, files) are listed one by one.
const mosaic = computed(() => (images.value.length >= 2 ? images.value : []))
const rows = computed(() => galleryRows(mosaic.value))
const listed = computed(() => {
  const by = (k: string) => props.attachments.filter(a => kind(a) === k)
  return [...(mosaic.value.length ? [] : images.value), ...by('video'), ...by('file')]
})

function openImage(att: MediaAttachment) {
  const chat = props.message ? useChatStore() : null
  const channelName = props.message ? chat?.allChannels.find(c => c.id === props.message?.channel_id)?.name : undefined
  openViewer({
    items: images.value.map(a => ({ src: a.url, name: a.original_filename, sizeBytes: a.size_bytes, mimeType: a.mime_type })),
    index: Math.max(0, images.value.indexOf(att)),
    ...(props.message ? { message: viewerMessageFrom(props.message, channelName) } : {})
  })
}
const sizeText = (att: MediaAttachment) => formatFileSize(att.size_bytes, locale.value)
</script>

<template>
  <div v-if="attachments?.length" :class="s.list">
    <div v-if="mosaic.length" :class="[s.gallery, 'flex flex-col gap-0.5 overflow-hidden rounded-lg']" :data-gallery="mosaic.length">
      <div v-for="(row, r) in rows" :key="r" :class="[row.rowClass, 'flex gap-0.5']" data-gallery-row>
        <div v-for="(cell, ci) in row.cells" :key="cell[0]!.id" :style="{ flex: `${row.weights[ci]} 1 0%` }" :class="[cell.length > 1 ? 'flex flex-col gap-0.5' : 'flex', 'min-w-0']" :data-stack="cell.length > 1 || undefined">
          <AttachmentImage
            v-for="att in cell"
            :key="att.id"
            :attachment="att"
            data-gallery-cell
            :button-class="[GALLERY_CELL.button, 'min-h-0 min-w-0 flex-1']"
            :image-class="GALLERY_CELL.image"
            placeholder-class="min-h-0 min-w-0 flex-1"
            compact
            decoding="async"
            @open="openImage(att)"
          >
            <span :class="GALLERY_CELL.chip" aria-hidden="true"><Maximize2 class="h-3.5 w-3.5" /></span>
          </AttachmentImage>
        </div>
      </div>
    </div>
    <div
      v-for="att in listed"
      :key="att.id"
      :data-attachment="kind(att)"
      :class="bare(att) ? s.bareImage : s.card"
    >
      <AttachmentImage
        v-if="kind(att) === 'image'"
        :attachment="att"
        :button-class="[s.imageButton, 'group relative']"
        :image-class="s.image"
        :placeholder-class="s.gone"
        @open="openImage(att)"
      >
        <span :class="GALLERY_CELL.chip" aria-hidden="true"><Maximize2 class="h-3.5 w-3.5" /></span>
      </AttachmentImage>
      <video v-else-if="kind(att) === 'video'" :src="att.url" controls :class="s.video"></video>
      <div v-if="!bare(att)" :class="s.footer">
        <div :class="s.name">
          <FileText :class="s.icon" />
          <a :href="att.url" target="_blank" :class="s.link">
            {{ att.original_filename }}
          </a>
        </div>
        <span :class="s.size">
          {{ sizeText(att) }}
        </span>
      </div>
    </div>
  </div>
</template>
