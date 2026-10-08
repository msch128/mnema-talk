<script setup lang="ts">
import type { Component } from 'vue'
import type { Toast } from '../stores/toast'
import { AlertCircle, Check, Info, X } from '@lucide/vue'
import { useToastStore } from '../stores/toast'

const toastStore = useToastStore()

const icons: Record<Toast['type'], Component> = { error: AlertCircle, success: Check, info: Info }
const tones: Record<Toast['type'], string> = { error: 'text-mnema-danger', success: 'text-mnema-accent', info: 'text-mnema-mint' }

function runAction(toast: Toast) {
  toast.action?.onClick?.()
  toastStore.dismiss(toast.id)
}
</script>

<template>
  <div
    class="pointer-events-none fixed bottom-5 right-5 z-[60] flex w-[360px] max-w-[calc(100vw-2.5rem)] flex-col gap-2 max-md:bottom-auto max-md:top-3 max-md:right-3 max-md:left-3 max-md:w-auto max-md:max-w-none"
    aria-live="polite"
  >
    <div
      v-for="toast in toastStore.toasts"
      :key="toast.id"
      :role="toast.type === 'error' ? 'alert' : 'status'"
      data-toast
      :data-type="toast.type"
      class="pointer-events-none grid grid-cols-[1rem_minmax(0,1fr)_1.75rem] items-start gap-x-2.5 gap-y-2 rounded-[10px] bg-mnema-elevated py-3 pl-3.5 pr-2 shadow-[inset_0_0_0_1px_#2B2F2D,0_10px_28px_rgba(0,0,0,0.5)]"
    >
      <component :is="icons[toast.type] || Info" :class="['col-start-1 row-start-1 mt-0.5 h-4 w-4', tones[toast.type] || tones.info]" />
      <div class="col-start-2 row-start-1 min-w-0 text-sm">
        <div class="break-words text-mnema-text">{{ toast.text }}</div>
        <div v-if="toast.detail" class="break-words text-[13px] text-mnema-tertiary">{{ toast.detail }}</div>
      </div>
      <button
        v-if="toast.action"
        type="button"
        class="pointer-events-auto col-start-2 row-start-2 min-h-7 max-w-full justify-self-start break-words rounded-md bg-mnema-elevated px-3 py-1 text-left text-[13px] font-semibold text-mnema-text shadow-[inset_0_0_0_1px_#2B2F2D] transition hover:bg-mnema-hover"
        @click="runAction(toast)"
      >{{ toast.action.label }}</button>
      <button
        v-if="toast.type === 'error' || toast.action"
        type="button"
        :aria-label="$t('common.close')"
        v-tooltip="$t('common.close')"
        class="pointer-events-auto col-start-3 row-start-1 -mt-1 flex h-7 w-7 items-center justify-center rounded-md text-mnema-tertiary transition hover:bg-mnema-hover hover:text-mnema-text"
        @click="toastStore.dismiss(toast.id)"
      >
        <X class="h-3.5 w-3.5" />
      </button>
    </div>
  </div>
</template>
