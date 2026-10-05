<script setup>
// Attachment cards under a message: image (opens the lightbox), video
// player, and the file name with its size.
import { computed } from 'vue'
import { FileText } from '@lucide/vue'

const props = defineProps({
  attachments: { type: Array, default: () => [] },
  // 'chat': channel and Talk chat, 'reply': thread replies, 'root': thread root card.
  variant: { type: String, default: 'chat' }
})
const emit = defineEmits(['open-image'])

const STYLES = {
  chat: {
    list: 'mt-2 space-y-2',
    card: 'max-w-md rounded-lg overflow-hidden border border-mnema-border bg-mnema-elevated shadow-sm',
    imageButton: 'block max-w-full',
    image: 'max-h-80 w-auto max-w-full rounded-t object-cover cursor-pointer hover:opacity-95 transition',
    video: 'max-h-80 w-full rounded-t',
    footer: 'p-2.5 flex items-center justify-between text-sm bg-mnema-raised border-t border-mnema-hairline',
    name: 'flex items-center gap-2 truncate',
    icon: 'w-4 h-4 text-mnema-tertiary flex-shrink-0',
    link: 'text-mnema-text hover:text-mnema-accent hover:underline truncate text-sm',
    size: 'text-xs font-mono text-mnema-tertiary pl-2 flex-shrink-0'
  },
  reply: {
    list: 'mt-1.5 space-y-1.5',
    card: 'rounded-lg overflow-hidden border border-mnema-border bg-mnema-elevated',
    imageButton: 'block w-full',
    image: 'max-h-40 w-full object-cover cursor-pointer hover:opacity-90 transition',
    video: 'max-h-40 w-full',
    footer: 'p-1.5 flex items-center justify-between text-xs bg-mnema-raised border-t border-mnema-hairline',
    name: 'flex items-center gap-1.5 truncate',
    icon: 'w-3.5 h-3.5 text-mnema-tertiary flex-shrink-0',
    link: 'text-mnema-text hover:text-mnema-accent hover:underline truncate',
    size: 'text-xs font-mono text-mnema-tertiary'
  },
  root: {
    list: 'space-y-1.5 pt-1',
    card: 'rounded-lg overflow-hidden border border-mnema-border bg-mnema-surface/40',
    imageButton: 'block w-full',
    image: 'max-h-48 w-full object-cover cursor-pointer hover:opacity-90 transition',
    video: 'max-h-48 w-full',
    footer: 'p-2 flex items-center justify-between text-xs',
    name: 'flex items-center gap-1.5 truncate',
    icon: 'w-3.5 h-3.5 text-mnema-tertiary flex-shrink-0',
    link: 'text-mnema-text hover:text-mnema-accent hover:underline truncate',
    size: 'text-xs font-mono text-mnema-tertiary'
  }
}

const s = computed(() => STYLES[props.variant] || STYLES.chat)

const kind = att => (att.mime_type?.startsWith('image/') ? 'image' : att.mime_type?.startsWith('video/') ? 'video' : 'file')
const sizeMb = att => ((att.size_bytes || 0) / 1024 / 1024).toFixed(2)
</script>

<template>
  <div v-if="attachments?.length" :class="s.list">
    <div
      v-for="att in attachments"
      :key="att.id"
      :data-attachment="kind(att)"
      :class="s.card"
    >
      <!-- The image's alt text (file name) names the button. -->
      <button
        v-if="kind(att) === 'image'"
        type="button"
        :class="[s.imageButton, 'focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-mnema-accent']"
        @click="emit('open-image', att.url)"
      >
        <img :src="att.url" :alt="att.original_filename" :class="s.image" loading="lazy" />
      </button>
      <video v-else-if="kind(att) === 'video'" :src="att.url" controls :class="s.video"></video>
      <div :class="s.footer">
        <div :class="s.name">
          <FileText :class="s.icon" />
          <a :href="att.url" target="_blank" :class="s.link">
            {{ att.original_filename }}
          </a>
        </div>
        <span :class="s.size">
          {{ $t('media.sizeMb', { size: sizeMb(att) }) }}
        </span>
      </div>
    </div>
  </div>
</template>
