<script setup lang="ts">
// Host for confirm() from lib/confirm.js. Safe default: focus starts on Cancel.
import { computed, useId, onBeforeUnmount } from 'vue'
import BaseDialog from './BaseDialog.vue'
import { pendingConfirm } from '../lib/confirm'
import { t } from '../i18n'

const request = computed(() => pendingConfirm.value)
const descId = useId()
// A new request remounts the shell so its safe initial focus is restored.
const requestKey = computed(() => Symbol(request.value?.title ?? 'confirm'))

function answer(result: boolean) {
  request.value?.resolve(result)
}
// A dialog must not carry an old instance's text or approval into a new host.
onBeforeUnmount(() => request.value?.resolve(false))
</script>

<template>
  <BaseDialog
    v-if="request"
    :key="requestKey"
    role="alertdialog"
    :describedby="request.body ? descId : ''"
    panel-class="max-w-[440px]"
    @close="answer(false)"
  >
    <template #default="{ titleId }">
      <div class="px-6 pb-2 pt-5">
        <h2 :id="titleId" class="mb-1.5 wrap-break-word text-lg font-semibold text-mnema-text">{{ request.title }}</h2>
        <p v-if="request.body" :id="descId" class="wrap-break-word text-sm text-mnema-muted">{{ request.body }}</p>
      </div>
      <div v-if="request.excerpt" class="mx-6 mb-1 mt-3 line-clamp-3 wrap-break-word rounded-md bg-mnema-raised px-3 py-2.5 text-sm text-mnema-body-ink">
        {{ request.excerpt }}
      </div>
      <div class="mt-4 flex h-[60px] shrink-0 items-center justify-end gap-2 border-t border-mnema-hairline bg-mnema-raised px-4">
        <button
          type="button"
          data-autofocus
          class="btn btn-secondary"
          @click="answer(false)"
        >{{ request.cancelLabel || t('common.cancel') }}</button>
        <button
          type="button"
          :class="['btn', request.danger ? 'btn-danger-solid' : 'btn-primary']"
          @click="answer(true)"
        >{{ request.confirmLabel || t('common.confirm') }}</button>
      </div>
    </template>
  </BaseDialog>
</template>
