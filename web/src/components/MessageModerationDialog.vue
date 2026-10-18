<script setup lang="ts">
// Host for moderateMessages() from lib/messageModeration: delete a message
// with a reason (the author is told), or delete the latest messages of a
// member in the channel.
import { ref, computed, watch, onBeforeUnmount } from 'vue'
import { api, caughtErrorMessage } from '../lib/api'
import { pendingMessageModeration, MAX_BULK_DELETE, DEFAULT_BULK_DELETE } from '../lib/messageModeration'
import { MAX_REASON_LENGTH } from '../lib/moderation'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'

const pending = computed(() => pendingMessageModeration.value)
const request = computed(() => pending.value?.request ?? null)
const chat = useChatStore()
const toasts = useToastStore()

const count = ref(String(DEFAULT_BULK_DELETE))
const reason = ref('')
const error = ref('')
const saving = ref(false)

const bulkCount = computed(() => Number(count.value))
const valid = computed(() => request.value?.kind !== 'bulk' || (Number.isInteger(bulkCount.value) && bulkCount.value >= 1 && bulkCount.value <= MAX_BULK_DELETE))

watch(pending, () => {
  count.value = String(DEFAULT_BULK_DELETE)
  reason.value = ''
  error.value = ''
  saving.value = false
}, { immediate: true })

function close(done: boolean) {
  pending.value?.resolve(done)
}

async function submit() {
  const req = request.value
  if (!req || !valid.value || saving.value) return
  error.value = ''
  saving.value = true
  try {
    if (req.kind === 'delete') {
      await chat.deleteMessage(req.channelId, req.messageId, reason.value.trim())
    } else {
      const result = await api(`/api/channels/${req.channelId}/messages/bulk-delete`, {
        method: 'POST',
        json: { user_id: req.userId, count: bulkCount.value, reason: reason.value.trim() }
      })
      const deleted = typeof result === 'object' && result !== null && 'deleted' in result && typeof result.deleted === 'number' ? result.deleted : 0
      toasts.success(t('moderation.bulkDeleted', { count: deleted, name: req.name }))
    }
    close(true)
  } catch (e) {
    error.value = caughtErrorMessage(e, t('moderation.failed'))
  } finally {
    saving.value = false
  }
}

onBeforeUnmount(() => pending.value?.resolve(false))

const inputClass = 'w-full bg-mnema-canvas border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary focus:outline-none focus:border-mnema-accent'
</script>

<template>
  <BaseDialog v-if="request" panel-class="max-w-[460px]" @close="close(false)">
    <template #default="{ titleId }">
      <form class="space-y-4 overflow-y-auto p-5" @submit.prevent="submit">
        <h2 :id="titleId" class="break-words text-lg font-semibold text-mnema-text">
          {{ request.kind === 'delete' ? $t('moderation.deleteTitle') : $t('moderation.bulkTitle', { name: request.name }) }}
        </h2>
        <p class="text-sm text-mnema-muted">{{ request.kind === 'delete' ? $t('moderation.deleteBody') : $t('moderation.bulkBody') }}</p>
        <div v-if="request.kind === 'delete' && request.excerpt" class="line-clamp-3 break-words rounded-md bg-mnema-raised px-3 py-2.5 text-sm text-mnema-body-ink">{{ request.excerpt }}</div>

        <div v-if="error" role="alert" class="rounded-lg border border-mnema-danger/30 bg-mnema-danger/10 p-2.5 text-sm text-mnema-danger">{{ error }}</div>

        <div v-if="request.kind === 'bulk'" class="space-y-1.5">
          <label for="bulk-count" class="font-mono text-xs font-medium uppercase tracking-wider text-mnema-tertiary">{{ $t('moderation.bulkCount') }}</label>
          <input id="bulk-count" v-model="count" type="number" min="1" :max="MAX_BULK_DELETE" inputmode="numeric" data-testid="bulk-count" :class="inputClass" />
          <p v-if="!valid" class="text-xs text-mnema-danger">{{ $t('moderation.bulkRange', { max: MAX_BULK_DELETE }) }}</p>
        </div>

        <div class="space-y-1.5">
          <label for="message-moderation-reason" class="font-mono text-xs font-medium uppercase tracking-wider text-mnema-tertiary">{{ $t('moderation.reason') }}</label>
          <input
            id="message-moderation-reason"
            v-model="reason"
            type="text"
            data-testid="reason"
            :maxlength="MAX_REASON_LENGTH"
            :placeholder="$t('moderation.reasonPlaceholder')"
            :class="inputClass"
          />
        </div>

        <div class="flex items-center justify-end gap-2 border-t border-mnema-hairline pt-3">
          <button
            type="button"
            class="h-8 rounded-md border border-mnema-border px-3 text-sm font-semibold text-mnema-text transition hover:bg-mnema-hover"
            @click="close(false)"
          >{{ $t('common.cancel') }}</button>
          <button
            type="submit"
            data-testid="message-moderation-submit"
            :disabled="!valid || saving"
            class="h-8 rounded-md bg-mnema-danger px-3 text-sm font-semibold text-mnema-accent-ink transition hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-30"
          >{{ $t('common.delete') }}</button>
        </div>
      </form>
    </template>
  </BaseDialog>
</template>
