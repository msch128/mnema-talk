<script setup lang="ts">
import type { PropType } from 'vue'
import type { TrayItem } from '../lib/attachmentTray'
import { computed } from 'vue'
import { Eye, FileText, RotateCw, X } from '@lucide/vue'
import { MAX_ATTACHMENTS } from '../lib/attachmentTray'
import { formatFileSize } from '../lib/fileSize'
import { locale, t } from '../i18n'

// The files waiting to go out with the message, above the composer. Each card
// shows the picture (or a video's first frame, or a file icon), the name and
// size, how far its upload is, and what went wrong with a way to retry.
const props = defineProps({
  items: { type: Array as PropType<TrayItem[]>, required: true }
})
const emit = defineEmits<{ remove: [key: string]; retry: [key: string]; view: [key: string] }>()

const percent = (item: TrayItem) => Math.round(item.progress * 100)
const sizeText = (item: TrayItem) => formatFileSize(item.file.size, locale.value)
// A video shows its first frame: the fragment asks for it, no playback.
const videoSrc = (item: TrayItem) => (item.previewUrl ? `${item.previewUrl}#t=0.1` : undefined)

const summary = computed(() => {
  if (props.items.some(i => i.status === 'error')) return t('chat.attachStatusFailed')
  if (props.items.some(i => i.status === 'queued' || i.status === 'uploading')) return t('chat.attachStatusUploading')
  return t('chat.attachStatusReady')
})
</script>

<template>
  <section
    v-if="items.length"
    data-testid="attachment-tray"
    :aria-label="$t('chat.attachTray')"
    class="border border-b-0 border-mnema-border bg-mnema-raised rounded-t-lg"
  >
    <div class="flex items-center justify-between gap-2 px-3 pt-2 text-xs text-mnema-tertiary">
      <span>{{ $t('chat.attachCount', { count: items.length, max: MAX_ATTACHMENTS }) }}</span>
      <span role="status" aria-live="polite" class="truncate">{{ summary }}</span>
    </div>
    <ul class="flex gap-2 overflow-x-auto px-3 pb-2 pt-1.5" role="list">
      <li
        v-for="item in items"
        :key="item.key"
        :data-attachment-status="item.status"
        :data-attachment-kind="item.kind"
        class="group w-28 shrink-0"
      >
        <div
          :class="[
            'relative h-20 overflow-hidden rounded-md border bg-mnema-canvas',
            item.status === 'error' ? 'border-mnema-danger/40' : 'border-mnema-hairline'
          ]"
        >
          <img v-if="item.kind === 'image' && item.previewUrl" :src="item.previewUrl" alt="" class="h-full w-full object-cover" />
          <video
            v-else-if="item.kind === 'video' && item.previewUrl"
            :src="videoSrc(item)"
            preload="metadata"
            muted
            playsinline
            tabindex="-1"
            aria-hidden="true"
            class="h-full w-full object-cover"
          ></video>
          <div v-else class="flex h-full w-full items-center justify-center text-mnema-tertiary">
            <FileText class="h-7 w-7" />
          </div>

          <div
            v-if="item.status === 'queued' || item.status === 'uploading'"
            class="absolute inset-x-0 bottom-0 h-1 bg-mnema-hairline"
          >
            <div
              role="progressbar"
              :aria-label="$t('chat.attachUploading', { name: item.file.name })"
              aria-valuemin="0"
              aria-valuemax="100"
              :aria-valuenow="percent(item)"
              class="h-full bg-mnema-accent transition-[width] duration-150 motion-reduce:transition-none"
              :style="{ width: percent(item) + '%' }"
            ></div>
          </div>

          <div
            v-if="item.status === 'error'"
            class="absolute inset-0 flex flex-col items-center justify-center gap-1 bg-mnema-canvas/90 p-1.5 text-center"
          >
            <p class="line-clamp-3 text-[11px] leading-tight text-mnema-danger" data-testid="attachment-error">{{ item.error }}</p>
            <button
              type="button"
              class="focus-ring flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] font-semibold text-mnema-text hover:bg-mnema-hover"
              @click="emit('retry', item.key)"
            >
              <RotateCw class="h-3 w-3" />
              {{ $t('chat.attachRetry') }}
            </button>
          </div>

          <div class="absolute right-1 top-1 flex gap-1 opacity-0 transition group-hover:opacity-100 group-focus-within:opacity-100 max-md:opacity-100">
            <button
              v-if="item.kind === 'image'"
              type="button"
              class="focus-ring flex h-6 w-6 items-center justify-center rounded-full bg-black/60 text-white hover:bg-black/80"
              :aria-label="$t('chat.attachView', { name: item.file.name })"
              v-tooltip="$t('chat.attachView', { name: item.file.name })"
              @click="emit('view', item.key)"
            >
              <Eye class="h-3.5 w-3.5" />
            </button>
            <button
              type="button"
              class="focus-ring flex h-6 w-6 items-center justify-center rounded-full bg-black/60 text-white hover:bg-black/80"
              :aria-label="$t('chat.attachRemove', { name: item.file.name })"
              v-tooltip="$t('chat.attachRemove', { name: item.file.name })"
              @click="emit('remove', item.key)"
            >
              <X class="h-3.5 w-3.5" />
            </button>
          </div>
        </div>
        <p class="mt-1 truncate text-xs text-mnema-text" :title="item.file.name">{{ item.file.name }}</p>
        <p class="text-[11px] font-mono text-mnema-tertiary">{{ sizeText(item) }}</p>
      </li>
    </ul>
  </section>
</template>
