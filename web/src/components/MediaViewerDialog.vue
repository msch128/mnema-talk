<script setup lang="ts">
// The media viewer's dialog: the pictures of a message (or of staged files),
// with navigation, zoom and pan, the details of the file and its message, and
// the usual actions. MediaViewer mounts it only while the viewer is open, so
// the dialog's focus handling starts with it. Every message detail is optional.
import { computed, nextTick, ref, watch } from 'vue'
import { ChevronLeft, ChevronRight, Copy, Download, ExternalLink, MessageSquare, Minus, Plus, X } from '@lucide/vue'
import UserAvatar from './UserAvatar.vue'
import { useDialog } from '../composables/useDialog'
import { useZoomPan } from '../composables/useZoomPan'
import { closeViewer, setViewerIndex, viewerState } from '../composables/useMediaViewer'
import { useChatStore } from '../stores/chat'
import { copyToClipboard } from '../lib/clipboard'
import { formatFileSize } from '../lib/fileSize'
import { dateTime } from '../lib/time'
import { locale, t } from '../i18n'

const chat = useChatStore()
const root = ref<HTMLElement | null>(null)
const stage = ref<HTMLElement | null>(null)
useDialog(root, { onClose: closeViewer, initialFocus: '[data-autofocus]' })
const zoom = useZoomPan(stage)

const items = computed(() => viewerState.items)
const index = computed(() => viewerState.index)
const item = computed(() => items.value[index.value])
const message = computed(() => viewerState.message)
const total = computed(() => items.value.length)
const canPrev = computed(() => index.value > 0)
const canNext = computed(() => index.value < total.value - 1)

// Pixel size: the file's own, else read from the loaded picture.
const loaded = ref<{ w: number; h: number } | null>(null)
const pixels = computed(() => {
  const it = item.value
  if (it?.width && it.height) return { w: it.width, h: it.height }
  return loaded.value
})
const details = computed(() => {
  const parts: string[] = []
  const it = item.value
  if (it?.sizeBytes !== undefined) parts.push(formatFileSize(it.sizeBytes, locale.value))
  if (pixels.value) parts.push(t('viewer.dimensions', { w: pixels.value.w, h: pixels.value.h }))
  return parts.join(' · ')
})
const sentAt = computed(() => (message.value ? dateTime(message.value.sentAt, locale.value) : ''))
const absoluteUrl = computed(() => (item.value ? new URL(item.value.src, window.location.href).href : ''))
const counter = computed(() => t('viewer.counter', { n: index.value + 1, total: total.value }))
const announcement = computed(() => (item.value ? t('viewer.announce', { name: item.value.name, n: index.value + 1, total: total.value }) : ''))
const zoomPercent = computed(() => Math.round(zoom.scale.value * 100))

function go(delta: number) {
  const next = index.value + delta
  if (next < 0 || next >= total.value) return
  setViewerIndex(next)
}

// A new file starts unzoomed and unmeasured; focus must not stay on a button that just got disabled.
watch(index, async () => {
  zoom.reset()
  loaded.value = null
  await nextTick()
  const active = document.activeElement
  if (active instanceof HTMLButtonElement && active.disabled) root.value?.focus()
})

function onLoad(e: Event) {
  const img = e.target as HTMLImageElement
  if (img.naturalWidth) loaded.value = { w: img.naturalWidth, h: img.naturalHeight }
}

function onKeydown(e: KeyboardEvent) {
  if (e.altKey || e.ctrlKey || e.metaKey) return
  const handled = (fn: () => void) => { e.preventDefault(); fn() }
  switch (e.key) {
    case 'ArrowLeft': return handled(() => go(-1))
    case 'ArrowRight': return handled(() => go(1))
    case '+': case '=': return handled(zoom.zoomIn)
    case '-': return handled(zoom.zoomOut)
    case '0': return handled(zoom.reset)
  }
}

// A click on the backdrop (not on a control or the picture) closes, unless it ends a drag.
function onBackdrop(e: MouseEvent) {
  if (e.target !== root.value && e.target !== stage.value) return
  if (zoom.wasDragged()) return
  closeViewer()
}

const copyLink = () => copyToClipboard(absoluteUrl.value, t('viewer.linkCopied'))
async function jump() {
  if (!message.value) return
  const { id, channelId, parentId } = message.value
  closeViewer()
  // A reply is found through its thread, like a search result.
  chat.goToMessage({ id, channel_id: channelId, ...(parentId ? { parent_id: parentId } : {}) })
}

const btn = 'flex h-9 w-9 items-center justify-center rounded-md text-mnema-text hover:bg-mnema-hover focus-visible:outline-2 focus-visible:outline-mnema-accent disabled:opacity-30 disabled:hover:bg-transparent'
const textBtn = 'flex h-9 items-center gap-2 rounded-md px-2.5 text-sm text-mnema-text hover:bg-mnema-hover focus-visible:outline-2 focus-visible:outline-mnema-accent'
</script>

<template>
  <div
    v-if="item"
    ref="root"
    role="dialog"
    aria-modal="true"
    :aria-label="$t('viewer.label')"
    tabindex="-1"
    data-testid="media-viewer"
    class="fixed inset-0 z-50 flex flex-col bg-black/95 focus:outline-hidden"
    @keydown="onKeydown"
    @click="onBackdrop"
  >
    <p class="sr-only" role="status" aria-live="polite" data-testid="viewer-live">{{ announcement }}</p>

    <header class="flex items-start justify-between gap-3 px-4 py-3">
      <div class="flex min-w-0 items-center gap-3">
        <UserAvatar v-if="message" :user="{ display_name: message.senderName, avatar_url: message.senderAvatarUrl }" size="sm" />
        <div class="min-w-0">
          <p class="truncate text-sm font-medium text-mnema-text" data-testid="viewer-name">{{ item.name }}</p>
          <p v-if="message" class="truncate text-xs text-mnema-secondary" data-testid="viewer-sender">
            <span class="text-mnema-text">{{ message.senderName }}</span>
            · {{ sentAt }}<template v-if="message.channelName"> · {{ $t('viewer.inChannel', { channel: message.channelName }) }}</template>
          </p>
          <p v-if="details" class="truncate font-mono text-xs text-mnema-tertiary" data-testid="viewer-details">{{ details }}</p>
        </div>
      </div>
      <div class="flex shrink-0 items-center gap-1">
        <button v-if="message" type="button" :class="textBtn" data-testid="viewer-jump" @click="jump">
          <MessageSquare class="h-4 w-4" /><span class="hidden sm:inline">{{ $t('viewer.jumpToMessage') }}</span>
        </button>
        <a :href="item.src" target="_blank" rel="noopener noreferrer" :class="btn" v-tooltip="$t('viewer.openOriginal')" :aria-label="$t('viewer.openOriginal')" data-testid="viewer-original">
          <ExternalLink class="h-4 w-4" />
        </a>
        <a :href="item.src" :download="item.name" :class="btn" v-tooltip="$t('viewer.download')" :aria-label="$t('viewer.download')" data-testid="viewer-download">
          <Download class="h-4 w-4" />
        </a>
        <button type="button" :class="btn" v-tooltip="$t('viewer.copyLink')" :aria-label="$t('viewer.copyLink')" data-testid="viewer-copy" @click="copyLink">
          <Copy class="h-4 w-4" />
        </button>
        <button type="button" data-autofocus data-dialog-close :class="btn" v-tooltip="$t('common.close')" :aria-label="$t('common.close')" data-testid="viewer-close" @click="closeViewer">
          <X class="h-5 w-5" />
        </button>
      </div>
    </header>

    <div
      ref="stage"
      class="relative flex min-h-0 flex-1 items-center justify-center overflow-hidden"
      :class="zoom.zoomed.value ? (zoom.dragging.value ? 'cursor-grabbing' : 'cursor-grab') : ''"
      data-testid="viewer-stage"
      @wheel.prevent="zoom.onWheel"
      @pointerdown="zoom.onPointerDown"
      @pointermove="zoom.onPointerMove"
      @pointerup="zoom.onPointerEnd"
      @pointercancel="zoom.onPointerEnd"
    >
      <button
        v-if="total > 1"
        type="button"
        :class="[btn, 'absolute left-3 z-10 h-11 w-11 bg-black/40']"
        :disabled="!canPrev"
        :aria-label="$t('viewer.previous')"
        data-testid="viewer-prev"
        @click.stop="go(-1)"
      ><ChevronLeft class="h-6 w-6" /></button>
      <img
        :key="item.src"
        :src="item.src"
        :alt="item.name"
        draggable="false"
        class="max-h-full max-w-[90vw] select-none rounded-lg object-contain shadow-2xl will-change-transform"
        :class="zoom.dragging.value ? '' : 'transition-transform duration-150 motion-reduce:transition-none'"
        :style="zoom.style.value"
        data-testid="viewer-image"
        @load="onLoad"
        @click.stop
        @dblclick.stop="zoom.onDoubleClick"
      />
      <button
        v-if="total > 1"
        type="button"
        :class="[btn, 'absolute right-3 z-10 h-11 w-11 bg-black/40']"
        :disabled="!canNext"
        :aria-label="$t('viewer.next')"
        data-testid="viewer-next"
        @click.stop="go(1)"
      ><ChevronRight class="h-6 w-6" /></button>
    </div>

    <footer class="flex items-center justify-center gap-3 px-4 py-3">
      <span v-if="total > 1" class="font-mono text-sm text-mnema-secondary" data-testid="viewer-counter">{{ counter }}</span>
      <div class="flex items-center gap-1" role="group" :aria-label="$t('viewer.zoomLevel', { percent: zoomPercent })">
        <button type="button" :class="btn" :aria-label="$t('viewer.zoomOut')" v-tooltip="$t('viewer.zoomOut')" data-testid="viewer-zoom-out" @click="zoom.zoomOut"><Minus class="h-4 w-4" /></button>
        <button type="button" class="h-9 min-w-14 rounded-md px-2 font-mono text-xs text-mnema-secondary hover:bg-mnema-hover" :aria-label="$t('viewer.zoomReset')" data-testid="viewer-zoom-reset" @click="zoom.reset">{{ zoomPercent }}%</button>
        <button type="button" :class="btn" :aria-label="$t('viewer.zoomIn')" v-tooltip="$t('viewer.zoomIn')" data-testid="viewer-zoom-in" @click="zoom.zoomIn"><Plus class="h-4 w-4" /></button>
      </div>
    </footer>
  </div>
</template>
