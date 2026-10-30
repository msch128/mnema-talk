<script setup lang="ts">
// Host for openForward() from lib/forwardDialog: pick where to forward a
// message and add an optional comment (POST /api/messages/{id}/forward).
import { computed, ref, watch } from 'vue'
import { Hash, MessageCircle, Volume2 } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import { closeForward, pendingForward } from '../lib/forwardDialog'
import { decodeChatMessage } from '../types/rest'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { dmTitle } from '../lib/dm'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'

const COMMENT_MAX = 4000
const request = computed(() => pendingForward.value)
const chatStore = useChatStore()
const authStore = useAuthStore()
const features = useServerFeaturesStore()
const permissions = usePermissionsStore()
const toasts = useToastStore()
const target = ref('')
const comment = ref('')
const filter = ref('')
const sending = ref(false)
const error = ref('')

const targets = computed(() => {
  const needle = filter.value.trim().toLowerCase()
  return chatStore.allChannels.filter(c => permissions.canIn(c.id, 'send_messages') && c.name.toLowerCase().includes(needle))
})

// Direct message conversations the member may write in are targets too (both features needed).
const conversations = computed(() => {
  if (!features.dm || !features.has('forward')) return []
  const needle = filter.value.trim().toLowerCase()
  return chatStore.dms
    .filter(d => d.can_send)
    .map(d => ({ id: d.id, name: dmTitle(d, authStore.user?.id) }))
    .filter(d => d.name.toLowerCase().includes(needle))
})

// The open channel is only a default when it is somewhere the member may write
// (not a read-only channel, a category or a DM view).
function defaultTarget(): string {
  const id = chatStore.activeChannel?.id ?? ''
  if (!id) return ''
  const channelOk = chatStore.allChannels.some(c => c.id === id) && permissions.canIn(id, 'send_messages')
  const dmOk = features.dm && features.has('forward') && chatStore.dms.some(d => d.id === id && d.can_send)
  return channelOk || dmOk ? id : ''
}

watch(request, () => {
  comment.value = ''
  filter.value = ''
  target.value = defaultTarget()
  error.value = ''
}, { immediate: true })

async function send() {
  const req = request.value
  if (!req || !target.value || sending.value) return
  sending.value = true
  error.value = ''
  try {
    const body = { channel_id: target.value, ...(comment.value.trim() ? { comment: comment.value.trim() } : {}) }
    await api(`/api/messages/${req.messageId}/forward`, { method: 'POST', json: body, decode: decodeChatMessage })
    toasts.success(t('forward.done'))
    closeForward()
  } catch (e) {
    error.value = caughtErrorMessage(e, t('forward.failed'))
  } finally {
    sending.value = false
  }
}
</script>

<template>
  <BaseDialog v-if="request" panel-class="max-w-[480px]" @close="closeForward">
    <template #default="{ titleId }">
      <form class="flex max-h-[80vh] flex-col space-y-3 p-5" data-testid="forward-dialog" @submit.prevent="send">
        <h2 :id="titleId" class="text-lg font-semibold text-mnema-text">{{ $t('forward.title') }}</h2>
        <p class="line-clamp-3 wrap-break-word rounded-md bg-mnema-raised px-3 py-2 text-sm text-mnema-muted">{{ request.excerpt }}</p>
        <input
          v-model="filter"
          type="search"
          data-testid="forward-filter"
          :aria-label="$t('forward.search')"
          :placeholder="$t('forward.search')"
          class="rounded-md border border-mnema-border bg-mnema-surface px-2.5 py-1.5 text-sm text-mnema-text placeholder:text-mnema-tertiary focus:border-mnema-accent focus:outline-hidden"
        >
        <ul class="min-h-0 flex-1 space-y-0.5 overflow-y-auto" role="radiogroup" :aria-label="$t('forward.to')">
          <li v-for="c in targets" :key="c.id">
            <label class="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm text-mnema-text hover:bg-mnema-hover">
              <input v-model="target" type="radio" name="forward-target" :value="c.id" :data-testid="`forward-target-${c.name}`">
              <Volume2 v-if="c.type === 'voice'" class="h-4 w-4 text-mnema-tertiary" />
              <Hash v-else class="h-4 w-4 text-mnema-tertiary" />
              <span class="truncate">{{ c.name }}</span>
            </label>
          </li>
          <li v-for="d in conversations" :key="d.id">
            <label class="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm text-mnema-text hover:bg-mnema-hover">
              <input v-model="target" type="radio" name="forward-target" :value="d.id" :data-testid="`forward-target-${d.name}`">
              <MessageCircle class="h-4 w-4 text-mnema-tertiary" />
              <span class="truncate">{{ d.name }}</span>
            </label>
          </li>
          <li v-if="!targets.length && !conversations.length" class="px-2 py-3 text-sm text-mnema-tertiary">{{ $t('forward.none') }}</li>
        </ul>
        <textarea
          v-model="comment"
          data-testid="forward-comment"
          rows="2"
          v-max-chars="COMMENT_MAX"
          :aria-label="$t('forward.comment')"
          :placeholder="$t('forward.comment')"
          class="resize-none rounded-md border border-mnema-border bg-mnema-surface px-2.5 py-1.5 text-sm text-mnema-text placeholder:text-mnema-tertiary focus:border-mnema-accent focus:outline-hidden"
        ></textarea>
        <p v-if="error" role="alert" data-testid="forward-error" class="rounded-lg border border-mnema-danger/30 bg-mnema-danger/10 p-2.5 text-sm text-mnema-danger">{{ error }}</p>
        <div class="flex justify-end gap-2 border-t border-mnema-hairline pt-3">
          <button type="button" class="btn btn-secondary h-8" @click="closeForward">{{ $t('common.cancel') }}</button>
          <button
            type="submit"
            data-testid="forward-send"
            :disabled="!target || sending"
            class="btn btn-primary h-8"
          >{{ $t('forward.send') }}</button>
        </div>
      </form>
    </template>
  </BaseDialog>
</template>
