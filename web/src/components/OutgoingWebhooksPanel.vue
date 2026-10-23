<script setup lang="ts">
// Outgoing webhooks: URLs that receive signed events of a channel (or of the
// whole server, for the Owner). The signing secret is shown once.
import { ref, computed, onMounted } from 'vue'
import { Copy, KeyRound, Pencil, Trash2, Send, ListChecks, Power } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import { arrayDecoder } from '../types/validation'
import { isOutgoingWebhook, isWebhookDelivery, decodeCreatedOutgoingWebhook, decodeOutgoingWebhook, decodeWebhookDelivery, CHANNEL_WEBHOOK_EVENTS } from '../types/webhooks'
import type { OutgoingWebhook, WebhookDelivery } from '../types/webhooks'
import { confirm } from '../lib/confirm'
import { useToastStore } from '../stores/toast'
import { useChatStore } from '../stores/chat'
import { t, locale } from '../i18n'

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
const busy = ref(false)

// null: closed; id '': a new webhook
const form = ref<{ id: string; name: string; url: string; events: string[] } | null>(null)

const allEvents: readonly string[] = CHANNEL_WEBHOOK_EVENTS
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

function openForm(hook?: OutgoingWebhook) {
  form.value = hook
    ? { id: hook.id, name: hook.name, url: hook.url, events: [...hook.events] }
    : { id: '', name: '', url: '', events: ['MESSAGE_CREATE'] }
}

function toggleEvent(event: string) {
  const f = form.value
  if (!f) return
  f.events = f.events.includes(event) ? f.events.filter(e => e !== event) : [...f.events, event]
}

async function save() {
  const f = form.value
  if (!f || busy.value) return
  busy.value = true
  try {
    const body = { name: f.name.trim(), url: f.url.trim(), events: f.events }
    if (f.id) {
      await api(`/api/admin/outgoing-webhooks/${f.id}`, { method: 'PATCH', json: body, decode: decodeOutgoingWebhook })
    } else {
      const created = await api(listPath.value, { method: 'POST', json: body, decode: decodeCreatedOutgoingWebhook })
      secret.value = { id: created.id, value: created.secret }
      toasts.success(t('webhooks.created'))
    }
    form.value = null
  } catch (e) {
    showError(e)
    return
  } finally {
    busy.value = false
  }
  await load()
}

async function copy(value: string) {
  try {
    await navigator.clipboard.writeText(value)
    toasts.success(t('webhooks.copied'))
  } catch {
    toasts.error(t('admin.copyFailed'), { detail: value })
  }
}

async function setEnabled(hook: OutgoingWebhook, enabled: boolean) {
  try {
    const updated = await api(`/api/admin/outgoing-webhooks/${hook.id}`, { method: 'PATCH', json: { enabled }, decode: decodeOutgoingWebhook })
    hooks.value = hooks.value.map(h => (h.id === updated.id ? updated : h))
  } catch (e) {
    showError(e)
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
        <button type="button" class="rounded-md border border-mnema-border-strong p-2 text-mnema-text transition hover:bg-mnema-hover" :aria-label="$t('webhooks.copySecret')" @click="copy(secret.value)">
          <Copy class="h-4 w-4" />
        </button>
      </div>
    </div>

    <form v-if="form" class="space-y-3 rounded-lg border border-mnema-border bg-mnema-surface p-3" data-testid="outgoing-form" @submit.prevent="save">
      <label class="block text-sm text-mnema-text">
        <span class="mb-1 block text-xs font-medium text-mnema-muted">{{ $t('common.name') }}</span>
        <input v-model="form.name" type="text" maxlength="80" required class="w-full rounded-md border border-mnema-border-field bg-mnema-elevated px-3 py-1.5 text-sm text-mnema-text" />
      </label>
      <label class="block text-sm text-mnema-text">
        <span class="mb-1 block text-xs font-medium text-mnema-muted">{{ $t('webhooks.url') }}</span>
        <input v-model="form.url" type="url" maxlength="2048" required :placeholder="'https://example.org/hook'" class="w-full rounded-md border border-mnema-border-field bg-mnema-elevated px-3 py-1.5 font-mono text-sm text-mnema-text" />
      </label>
      <fieldset>
        <legend class="mb-1 text-xs font-medium text-mnema-muted">{{ $t('webhooks.eventsLabel') }}</legend>
        <div class="grid gap-1 sm:grid-cols-2">
          <label v-for="event in allEvents" :key="event" class="flex items-center gap-2 text-sm text-mnema-text">
            <input type="checkbox" :checked="form.events.includes(event)" :data-event="event" @change="toggleEvent(event)" />
            <span>{{ eventLabel(event) }}</span>
          </label>
        </div>
      </fieldset>
      <div class="flex justify-end gap-2">
        <button type="button" class="rounded-md px-3.5 py-1.5 text-sm text-mnema-muted transition hover:text-mnema-text" @click="form = null">{{ $t('common.cancel') }}</button>
        <button type="submit" :disabled="busy || !form.name.trim() || !form.url.trim() || !form.events.length" class="rounded-md bg-mnema-accent px-3.5 py-1.5 text-sm font-semibold text-mnema-accent-ink transition hover:bg-mnema-accent-hover disabled:opacity-40">{{ form.id ? $t('common.save') : $t('webhooks.create') }}</button>
      </div>
    </form>
    <button v-else type="button" class="rounded-md border border-mnema-border-strong px-3 py-1.5 text-sm text-mnema-text transition hover:bg-mnema-hover" data-testid="outgoing-new" @click="openForm()">{{ $t('webhooks.newOutgoing') }}</button>

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
          <button type="button" class="rounded-sm p-1.5 transition hover:text-mnema-text" v-tooltip="$t('webhooks.edit', { name: hook.name })" @click="openForm(hook)"><Pencil class="h-4 w-4" /></button>
          <button type="button" class="rounded-sm p-1.5 transition hover:text-mnema-text" v-tooltip="hook.status === 'active' ? $t('webhooks.switchOff', { name: hook.name }) : $t('webhooks.switchOn', { name: hook.name })" @click="setEnabled(hook, hook.status !== 'active')"><Power class="h-4 w-4" /></button>
          <button type="button" class="rounded-sm p-1.5 transition hover:text-mnema-text" v-tooltip="$t('webhooks.test', { name: hook.name })" @click="test(hook)"><Send class="h-4 w-4" /></button>
          <button type="button" class="rounded-sm p-1.5 transition hover:text-mnema-text" v-tooltip="$t('webhooks.deliveries', { name: hook.name })" :aria-expanded="deliveries?.id === hook.id" @click="toggleDeliveries(hook)"><ListChecks class="h-4 w-4" /></button>
          <button type="button" class="rounded-sm p-1.5 transition hover:text-mnema-text" v-tooltip="$t('webhooks.rotateFor', { name: hook.name })" @click="rotate(hook)"><KeyRound class="h-4 w-4" /></button>
          <button type="button" class="rounded-sm p-1.5 transition hover:text-mnema-danger" v-tooltip="$t('webhooks.deleteFor', { name: hook.name })" @click="remove(hook)"><Trash2 class="h-4 w-4" /></button>
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
