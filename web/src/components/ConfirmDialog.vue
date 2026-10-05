<script setup>
// Host for confirm() from lib/confirm.js. Safe default: focus starts on Cancel.
import { computed, useId } from 'vue'
import BaseDialog from './BaseDialog.vue'
import { pendingConfirm } from '../lib/confirm'
import { t } from '../i18n'

const request = computed(() => pendingConfirm.value)
const descId = useId()

function answer(result) {
  request.value?.resolve(result)
}
</script>

<template>
  <BaseDialog
    v-if="request"
    :key="request"
    role="alertdialog"
    :describedby="request.body ? descId : ''"
    panel-class="max-w-[440px]"
    @close="answer(false)"
  >
    <template #default="{ titleId }">
      <div class="px-6 pb-2 pt-5">
        <h2 :id="titleId" class="mb-1.5 break-words text-lg font-semibold text-mnema-text">{{ request.title }}</h2>
        <p v-if="request.body" :id="descId" class="break-words text-sm text-mnema-muted">{{ request.body }}</p>
      </div>
      <div v-if="request.excerpt" class="mx-6 mb-1 mt-3 line-clamp-3 break-words rounded-md bg-mnema-raised px-3 py-2.5 text-sm text-mnema-body-ink">
        {{ request.excerpt }}
      </div>
      <div class="mt-4 flex h-[60px] flex-shrink-0 items-center justify-end gap-2 border-t border-mnema-hairline bg-mnema-raised px-4">
        <button
          type="button"
          data-autofocus
          class="h-8 rounded-md bg-mnema-elevated px-3 text-sm font-semibold text-mnema-text shadow-[inset_0_0_0_1px_#2B2F2D] transition hover:bg-mnema-hover focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-mnema-accent"
          @click="answer(false)"
        >{{ request.cancelLabel || t('common.cancel') }}</button>
        <button
          type="button"
          :class="[
            'h-8 rounded-md px-3 text-sm font-semibold transition focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-mnema-accent',
            request.danger
              ? 'bg-mnema-danger text-mnema-accent-ink hover:brightness-110'
              : 'bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover'
          ]"
          @click="answer(true)"
        >{{ request.confirmLabel || t('common.confirm') }}</button>
      </div>
    </template>
  </BaseDialog>
</template>
