<script setup lang="ts">
// Outgoing webhooks: URLs that receive signed events of a channel (or of the
// whole server, for the Owner). The signing secret is shown once.
import { ref, computed, onMounted } from 'vue'
import { Copy, KeyRound, Pencil, Trash2, Send, ListChecks } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import { arrayDecoder } from '../types/validation'
import { isOutgoingWebhook, isWebhookDelivery, decodeCreatedOutgoingWebhook, decodeWebhookDelivery } from '../types/webhooks'
import type { OutgoingWebhook, WebhookDelivery } from '../types/webhooks'
import { confirm } from '../lib/confirm'
import { useToastStore } from '../stores/toast'
import { useChatStore } from '../stores/chat'
import { t, locale } from '../i18n'
import OutgoingWebhookForm from './OutgoingWebhookForm.vue'

const props = defineProps({
  channelId: { type: String, required: true }
})

const toasts = useToastStore()
const chat = useChatStore()
const hooks = ref<OutgoingWebhook[]>([])
const loaded = ref(false)
const secret = ref<{ id: string; value: string } | null>(null)
const deliveries = ref<{ id: string; list: WebhookDelivery[] } | null>(null)
const testResult = ref<{ id: string; delivery: WebhookDelivery } | null>(null)

// null: closed; hook null: a new webhook
const form = ref<{ hook: OutgoingWebhook | null } | null>(null)

const listPath = computed(() => `/api/admin/channels/${props.channelId}/outgoing-webhooks`)
const eventLabel = (event: string) => t(`webhooks.events.${event}`)

function showError(e: unknown) {
  toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
}

async function load() {
  try {
    hooks.value = await api(listPath.value, { decode: arrayDecoder('outgoing webhooks', isOutgoingWebhook) })
  } catch (e) {
    showError(e)
  } finally {
    loaded.value = true
  }
}

// Created or saved from the form: show a new secret once and reload the list.
async function onSaved(created: { id: string; value: string } | null) {
  if (created) secret.value = created
  form.value = null
  await load()
}

const formRef = ref<{ guard: () => Promise<boolean> } | null>(null)
// Opening another form while one has unsaved edits asks first.
async function openForm(hook?: OutgoingWebhook) {
  if (formRef.value && !(await formRef.value.guard())) return
  form.value = { hook: hook ?? null }
}

async function copy(value: string) {
  try {
    await navigator.clipboard.writeText(value)
    toasts.success(t('webhooks.copied'))
  } catch {
    toasts.error(t('admin.copyFailed'), { detail: value })
  }
}

async function test(hook: OutgoingWebhook) {
  try {
    const delivery = await api(`/api/admin/outgoing-webhooks/${hook.id}/test`, { method: 'POST', decode: decodeWebhookDelivery })
    testResult.value = { id: hook.id, delivery }
  } catch (e) {
    showError(e)
  }
}

async function toggleDeliveries(hook: OutgoingWebhook) {
  if (deliveries.value?.id === hook.id) {
    deliveries.value = null
    return
  }
  try {
    const list = await api(`/api/admin/outgoing-webhooks/${hook.id}/deliveries`, { decode: arrayDecoder('deliveries', isWebhookDelivery) })
    deliveries.value = { id: hook.id, list }
  } catch (e) {
    showError(e)
  }
}

async function rotate(hook: OutgoingWebhook) {
  const ok = await confirm({
    title: t('webhooks.rotateTitle', { name: hook.name }),
    body: t('webhooks.rotateBody'),
    confirmLabel: t('webhooks.rotate'),
    danger: true
  })
  if (!ok) return
  try {
    const created = await api(`/api/admin/outgoing-webhooks/${hook.id}/secret`, { method: 'POST', decode: decodeCreatedOutgoingWebhook })
    secret.value = { id: created.id, value: created.secret }
  } catch (e) {
    showError(e)
  }
}

async function remove(hook: OutgoingWebhook) {
  const ok = await confirm({
    title: t('webhooks.deleteTitle', { name: hook.name }),
    body: t('webhooks.deleteOutgoingBody'),
    confirmLabel: t('common.delete'),
    danger: true
  })
  if (!ok) return
  try {
    await api(`/api/admin/outgoing-webhooks/${hook.id}`, { method: 'DELETE' })
    hooks.value = hooks.value.filter(h => h.id !== hook.id)
    if (secret.value?.id === hook.id) secret.value = null
  } catch (e) {
    showError(e)
  }
}

function memberName(id: string | null): string {
  if (id === null) return t('webhooks.unknownMember')
  const m = chat.members.find(x => x.id === id)
  return m?.display_name || m?.username || t('webhooks.unknownMember')
}
const when = (iso: string) => new Date(iso).toLocaleString(locale.value)
const statusClass = (status: OutgoingWebhook['status']) =>
  status === 'active' ? 'bg-mnema-accent-subtle text-mnema-accent' : status === 'auto_disabled' ? 'bg-mnema-danger/15 text-mnema-danger' : 'bg-mnema-raised text-mnema-muted'

onMounted(load)
</script>

<template>
  <div class="space-y-4" data-testid="outgoing-webhooks">
    <p class="text-sm text-mnema-tertiary">{{ $t('webhooks.outgoingHint') }}</p>

    <div v-if="secret" class="rounded-lg border border-mnema-accent/40 bg-mnema-accent-subtle p-3" role="status" data-testid="revealed-secret">
      <p class="text-sm font-medium text-mnema-text">{{ $t('webhooks.secretOnce') }}</p>
      <div class="mt-2 flex items-center gap-2">
        <code class="min-w-0 flex-1 select-all break-all rounded-sm bg-mnema-surface px-2 py-1 font-mono text-xs text-mnema-text">{{ secret.value }}</code>
        <button type="button" class="btn btn-secondary p-2" :aria-label="$t('webhooks.copySecret')" @click="copy(secret.value)">
          <Copy class="h-4 w-4" />
        </button>
      </div>
    </div>

    <OutgoingWebhookForm v-if="form" :key="form.hook?.id ?? 'new'" ref="formRef" :channel-id="channelId" :hook="form.hook" @saved="onSaved" @cancel="form = null" />
    <button v-else type="button" class="btn btn-secondary" data-testid="outgoing-new" @click="openForm()">{{ $t('webhooks.newOutgoing') }}</button>

    <p v-if="loaded && !hooks.length" class="text-sm text-mnema-tertiary">{{ $t('webhooks.emptyOutgoing') }}</p>
    <ul v-if="hooks.length" class="divide-y divide-mnema-hairline overflow-hidden rounded-lg border border-mnema-hairline bg-mnema-surface">
      <li v-for="hook in hooks" :key="hook.id" class="space-y-2 p-3" data-testid="outgoing-webhook">
        <div class="flex flex-wrap items-center gap-2">
          <span class="min-w-0 truncate text-sm font-medium text-mnema-text">{{ hook.name }}</span>
          <span :class="['rounded-sm px-1.5 py-px text-[11px] font-semibold', statusClass(hook.status)]" data-testid="outgoing-status">{{ $t(`webhooks.status.${hook.status}`) }}</span>
        </div>
        <p class="truncate font-mono text-xs text-mnema-muted" :title="hook.url">{{ hook.url }}</p>
        <p class="text-xs text-mnema-tertiary">{{ hook.events.map(eventLabel).join(', ') }}</p>
        <p class="text-xs text-mnema-tertiary" data-testid="outgoing-audit">{{ $t('webhooks.urlSet', { name: memberName(hook.url_set_by), when: when(hook.url_set_at) }) }}</p>
        <p v-if="hook.status === 'auto_disabled'" class="text-xs text-mnema-danger" data-testid="outgoing-auto-off">{{ $t('webhooks.autoDisabled', { count: hook.consecutive_failures }) }}</p>
        <p v-if="hook.last_attempt_at" class="text-xs text-mnema-tertiary" data-testid="outgoing-last">
          {{ hook.last_error ? $t('webhooks.lastFailed', { when: when(hook.last_attempt_at), error: hook.last_error }) : $t('webhooks.lastOk', { when: when(hook.last_attempt_at) }) }}
        </p>

        <div class="flex flex-wrap items-center gap-1 text-mnema-tertiary">
          <button type="button" class="btn btn-icon btn-sm hover:text-mnema-text" v-tooltip="$t('webhooks.edit', { name: hook.name })" @click="openForm(hook)"><Pencil class="h-4 w-4" /></button>
          <button type="button" class="btn btn-icon btn-sm hover:text-mnema-text" v-tooltip="$t('webhooks.test', { name: hook.name })" @click="test(hook)"><Send class="h-4 w-4" /></button>
          <button type="button" class="btn btn-icon btn-sm hover:text-mnema-text" v-tooltip="$t('webhooks.deliveries', { name: hook.name })" :aria-expanded="deliveries?.id === hook.id" @click="toggleDeliveries(hook)"><ListChecks class="h-4 w-4" /></button>
          <button type="button" class="btn btn-icon btn-sm hover:text-mnema-text" v-tooltip="$t('webhooks.rotateFor', { name: hook.name })" @click="rotate(hook)"><KeyRound class="h-4 w-4" /></button>
          <button type="button" class="btn btn-icon btn-sm hover:text-mnema-danger" v-tooltip="$t('webhooks.deleteFor', { name: hook.name })" @click="remove(hook)"><Trash2 class="h-4 w-4" /></button>
        </div>

        <p v-if="testResult?.id === hook.id" :class="['text-xs', testResult.delivery.success ? 'text-mnema-accent' : 'text-mnema-danger']" role="status" data-testid="outgoing-test">
          {{ testResult.delivery.success ? $t('webhooks.testOk', { status: testResult.delivery.status_code ?? 0 }) : $t('webhooks.testFailed', { error: testResult.delivery.error ?? '' }) }}
        </p>
        <ul v-if="deliveries?.id === hook.id" class="max-h-48 space-y-0.5 overflow-y-auto rounded-sm border border-mnema-hairline bg-mnema-elevated p-2 text-xs" data-testid="deliveries">
          <li v-if="!deliveries.list.length" class="text-mnema-tertiary">{{ $t('webhooks.noDeliveries') }}</li>
          <li v-for="d in deliveries.list" :key="d.id" class="flex items-center justify-between gap-2" data-testid="delivery">
            <span class="truncate text-mnema-text">{{ eventLabel(d.event_type) }} <span class="text-mnema-tertiary">#{{ d.attempt }}</span></span>
            <span :class="d.success ? 'text-mnema-accent' : 'text-mnema-danger'">{{ d.success ? (d.status_code ?? 'OK') : (d.error ?? '') }}</span>
            <time class="shrink-0 text-mnema-tertiary" :datetime="d.created_at">{{ when(d.created_at) }}</time>
          </li>
        </ul>
      </li>
    </ul>
  </div>
</template>
