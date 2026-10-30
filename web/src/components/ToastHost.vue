<script setup lang="ts">
import type { Component } from 'vue'
import type { Toast } from '../stores/toast'
import { AlertCircle, Check, Info, X } from '@lucide/vue'
import { useToastStore } from '../stores/toast'

const toastStore = useToastStore()

const icons: Record<Toast['type'], Component> = { error: AlertCircle, success: Check, info: Info }
const tones: Record<Toast['type'], string> = { error: 'text-mnema-danger', success: 'text-mnema-accent', info: 'text-mnema-mint' }

// A plain toast closes with a click; errors and toasts with an action have their own button.
function dismissPlain(toast: Toast) {
  if (toast.type !== 'error' && !toast.action) toastStore.dismiss(toast.id)
}

function runAction(toast: Toast, which: 'action' | 'secondary' = 'action') {
  toast[which]?.onClick?.()
  toastStore.dismiss(toast.id)
}
</script>

<template>
  <div
    class="pointer-events-none fixed bottom-5 right-5 z-60 flex w-[360px] max-w-[calc(100vw-2.5rem)] flex-col gap-2 max-md:bottom-auto max-md:top-3 max-md:right-3 max-md:left-3 max-md:w-auto max-md:max-w-none"
    aria-live="polite"
  >
    <div
      v-for="toast in toastStore.toasts"
      :key="toast.id"
      :role="toast.type === 'error' ? 'alert' : 'status'"
      data-toast
      :data-type="toast.type"
      @mouseenter="toastStore.pause(toast.id)"
      @mouseleave="toastStore.resume(toast.id)"
      @focusin="toastStore.pause(toast.id)"
      @focusout="toastStore.resume(toast.id)"
      @click="dismissPlain(toast)"
      class="pointer-events-auto grid grid-cols-[1rem_minmax(0,1fr)_1.75rem] items-start gap-x-2.5 gap-y-2 rounded-lg bg-mnema-elevated py-3 pl-3.5 pr-2 shadow-popover"
    >
      <component :is="icons[toast.type] || Info" :class="['col-start-1 row-start-1 mt-0.5 h-4 w-4', tones[toast.type] || tones.info]" />
      <div class="col-start-2 row-start-1 min-w-0 text-sm">
        <div class="wrap-break-word text-mnema-text">{{ toast.text }}</div>
        <div v-if="toast.detail" class="wrap-break-word text-[13px] text-mnema-tertiary">{{ toast.detail }}</div>
      </div>
      <div v-if="toast.action" class="col-start-2 row-start-2 flex flex-wrap gap-2">
        <button
          type="button"
          class="pointer-events-auto min-h-7 max-w-full wrap-break-word rounded-md bg-mnema-elevated px-3 py-1 text-left text-[13px] font-semibold text-mnema-text shadow-ring transition hover:bg-mnema-hover"
          @click="runAction(toast)"
        >{{ toast.action.label }}</button>
        <button
          v-if="toast.secondary"
          type="button"
          class="pointer-events-auto min-h-7 max-w-full wrap-break-word rounded-md px-3 py-1 text-left text-[13px] text-mnema-muted transition hover:bg-mnema-hover hover:text-mnema-text"
          @click="runAction(toast, 'secondary')"
        >{{ toast.secondary.label }}</button>
      </div>
      <button
        v-if="toast.type === 'error' || toast.action"
        type="button"
        :aria-label="$t('common.close')"
        v-tooltip="$t('common.close')"
        class="btn btn-icon pointer-events-auto col-start-3 row-start-1 -mt-1 h-7 w-7"
        @click="toastStore.dismiss(toast.id)"
      >
        <X class="h-3.5 w-3.5" />
      </button>
    </div>
  </div>
</template>
