<script setup lang="ts">
import type { PropType } from 'vue'
import type { MediaAttachment } from '../types/domain'
// Attachments under a message. In chat an image shows on its own at its
// natural aspect ratio (no card, no file row) and opens the lightbox; a very
// wide image is cropped to a minimum height instead of shrinking to a strip.
// Videos and files keep the card with the file name and size.
import { computed } from 'vue'
import { FileText } from '@lucide/vue'
import { formatFileSize } from '../lib/fileSize'
import { locale } from '../i18n'

const props = defineProps({
  attachments: { type: Array as PropType<MediaAttachment[]>, default: () => [] },
  // 'chat': channel and Talk chat, 'reply': thread replies, 'root': thread root card.
  variant: { type: String as PropType<'chat' | 'reply' | 'root'>, default: 'chat' }
})
const emit = defineEmits<{ 'open-image': [src: string] }>()

const STYLES = {
  chat: {
    list: 'mt-2 space-y-2',
    card: 'max-w-md rounded-lg overflow-hidden border border-mnema-border bg-mnema-elevated shadow-xs',
    bareImage: 'w-fit max-w-md',
    imageButton: 'block max-w-full rounded-lg overflow-hidden',
    image: 'block h-auto max-h-80 min-h-24 w-auto max-w-full object-cover cursor-pointer hover:opacity-95 transition',
    video: 'max-h-80 w-full rounded-t',
    footer: 'p-2.5 flex items-center justify-between text-sm bg-mnema-raised border-t border-mnema-hairline',
    name: 'flex items-center gap-2 truncate',
    icon: 'w-4 h-4 text-mnema-tertiary shrink-0',
    link: 'text-mnema-text hover:text-mnema-accent hover:underline truncate text-sm',
    size: 'text-xs font-mono text-mnema-tertiary pl-2 shrink-0'
  },
  reply: {
    list: 'mt-1.5 space-y-1.5',
    card: 'rounded-lg overflow-hidden border border-mnema-border bg-mnema-elevated',
    bareImage: '',
    imageButton: 'block w-full',
    image: 'max-h-40 w-full object-cover cursor-pointer hover:opacity-90 transition',
    video: 'max-h-40 w-full',
    footer: 'p-1.5 flex items-center justify-between text-xs bg-mnema-raised border-t border-mnema-hairline',
    name: 'flex items-center gap-1.5 truncate',
    icon: 'w-3.5 h-3.5 text-mnema-tertiary shrink-0',
    link: 'text-mnema-text hover:text-mnema-accent hover:underline truncate',
    size: 'text-xs font-mono text-mnema-tertiary'
  },
  root: {
    list: 'space-y-1.5 pt-1',
    card: 'rounded-lg overflow-hidden border border-mnema-border bg-mnema-surface/40',
    bareImage: '',
    imageButton: 'block w-full',
    image: 'max-h-48 w-full object-cover cursor-pointer hover:opacity-90 transition',
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
const sizeText = (att: MediaAttachment) => formatFileSize(att.size_bytes, locale.value)
</script>

<template>
  <div v-if="attachments?.length" :class="s.list">
    <div
      v-for="att in attachments"
      :key="att.id"
      :data-attachment="kind(att)"
      :class="bare(att) ? s.bareImage : s.card"
    >
      <!-- The image's alt text (file name) names the button. -->
      <button
        v-if="kind(att) === 'image'"
        type="button"
        :class="[s.imageButton, 'focus:outline-hidden focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-mnema-accent']"
        @click="emit('open-image', att.url)"
      >
        <img :src="att.url" :alt="att.original_filename" :title="att.original_filename" :class="s.image" loading="lazy" />
      </button>
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
