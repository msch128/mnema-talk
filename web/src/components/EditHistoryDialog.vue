<script setup lang="ts">
// Host for openEditHistory() from lib/editHistory: the earlier versions of an
// edited message, for members with Manage messages (GET .../edits).
import { ref, computed, watch } from 'vue'
import { api, caughtErrorMessage } from '../lib/api'
import { pendingEditHistory, closeEditHistory } from '../lib/editHistory'
import { decodeChatMessageEditHistory, type ChatMessageEditHistory } from '../types/rest'
import { t, locale } from '../i18n'
import { shortcodesForEditing } from '../lib/customEmoji'
import BaseDialog from './BaseDialog.vue'

const request = computed(() => pendingEditHistory.value)
const edits = ref<ChatMessageEditHistory['edits']>([])
const loading = ref(false)
const error = ref('')
let seq = 0

function formatStamp(iso: string) {
  return new Date(iso).toLocaleString([locale.value], { dateStyle: 'medium', timeStyle: 'medium' })
}

async function load() {
  const req = request.value
  const mine = ++seq
  edits.value = []
  error.value = ''
  if (!req) return
  loading.value = true
  try {
    const history = await api(`/api/channels/${req.channelId}/messages/${req.messageId}/edits`, { decode: decodeChatMessageEditHistory })
    if (mine === seq) edits.value = history.edits
  } catch (e) {
    if (mine === seq) error.value = caughtErrorMessage(e, t('editHistory.failed'))
  } finally {
    if (mine === seq) loading.value = false
  }
}

watch(request, load, { immediate: true })
</script>

<template>
  <BaseDialog v-if="request" panel-class="max-w-[520px]" @close="closeEditHistory">
    <template #default="{ titleId }">
      <div class="flex max-h-[80vh] flex-col space-y-4 p-5">
        <h2 :id="titleId" class="text-lg font-semibold text-mnema-text">{{ $t('editHistory.title') }}</h2>
        <p class="text-sm text-mnema-muted">{{ $t('editHistory.body') }}</p>

        <ol class="min-h-0 flex-1 space-y-3 overflow-y-auto" data-testid="edit-history-list">
          <li class="rounded-md border border-mnema-accent/40 bg-mnema-raised px-3 py-2.5">
            <p class="font-mono text-xs uppercase tracking-wider text-mnema-tertiary">{{ $t('editHistory.current') }} · {{ formatStamp(request.editedAt) }}</p>
            <p class="mt-1 whitespace-pre-wrap wrap-break-word text-sm text-mnema-body-ink">{{ shortcodesForEditing(request.current) || $t('editHistory.empty') }}</p>
          </li>
          <li v-for="edit in edits" :key="edit.id" class="rounded-md bg-mnema-raised px-3 py-2.5" data-testid="edit-history-version">
            <p class="font-mono text-xs uppercase tracking-wider text-mnema-tertiary">{{ formatStamp(edit.edited_at) }}</p>
            <p class="mt-1 whitespace-pre-wrap wrap-break-word text-sm text-mnema-body-ink">{{ shortcodesForEditing(edit.content) || $t('editHistory.empty') }}</p>
          </li>
        </ol>
        <p v-if="loading" class="text-sm text-mnema-muted" role="status">{{ $t('editHistory.loading') }}</p>
        <p v-else-if="error" role="alert" class="rounded-lg border border-mnema-danger/30 bg-mnema-danger/10 p-2.5 text-sm text-mnema-danger">{{ error }}</p>
        <p v-else-if="!edits.length" class="text-sm text-mnema-muted">{{ $t('editHistory.none') }}</p>

        <div class="flex justify-end border-t border-mnema-hairline pt-3">
          <button
            type="button"
            class="btn btn-secondary h-8"
            @click="closeEditHistory"
          >{{ $t('common.close') }}</button>
        </div>
      </div>
    </template>
  </BaseDialog>
</template>
