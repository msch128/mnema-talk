<script setup lang="ts">
// Incoming webhooks of one channel: a secret URL scripts post to. The URL is
// shown once (the server keeps only a hash), so it is revealed right after
// creating or regenerating, with a copy button.
import { ref, computed, onMounted } from 'vue'
import { Copy, KeyRound, Trash2, ImagePlus, SlidersHorizontal } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import { arrayDecoder } from '../types/validation'
import { isWebhook, decodeCreatedWebhook, decodeWebhook } from '../types/webhooks'
import type { Webhook } from '../types/webhooks'
import { confirm } from '../lib/confirm'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import StatePanel from './StatePanel.vue'
import IncomingWebhookEditor from './IncomingWebhookEditor.vue'
import { IMAGE_UPLOAD_MAX_MB, pickImage } from '../lib/pickImage'
import { initialOf } from '../lib/initial'

const props = defineProps({
  channelId: { type: String, required: true }
})

const toasts = useToastStore()
const hooks = ref<Webhook[]>([])
const loaded = ref(false)
const loadFailed = ref(false)
const newName = ref('')
const busy = ref(false)
// The one URL on show: right after creating or regenerating.
const revealed = ref<{ id: string; url: string } | null>(null)
// The webhook whose settings are open: one draft at a time, so another one is asked about first.
const settingsOpen = ref<string | null>(null)
const editor = ref<{ guard: () => Promise<boolean> } | null>(null)
const setEditor = (el: unknown) => { editor.value = el as { guard: () => Promise<boolean> } | null }

async function toggleSettings(id: string) {
  if (editor.value && !(await editor.value.guard())) return
  settingsOpen.value = settingsOpen.value === id ? null : id
}

function onUpdated(updated: Webhook) {
  hooks.value = hooks.value.map(h => (h.id === updated.id ? updated : h))
}

const base = computed(() => `/api/admin/channels/${props.channelId}/webhooks`)

function showError(e: unknown) {
  toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
}

async function load() {
  loadFailed.value = false
  try {
    hooks.value = await api(base.value, { decode: arrayDecoder('webhooks', isWebhook) })
  } catch (e) {
    loadFailed.value = true
    showError(e)
  } finally {
    loaded.value = true
  }
}

async function create() {
  const name = newName.value.trim()
  if (!name) return
  busy.value = true
  try {
    const created = await api(base.value, { method: 'POST', json: { name }, decode: decodeCreatedWebhook })
    revealed.value = { id: created.id, url: created.url }
    newName.value = ''
    toasts.success(t('webhooks.created'))
  } catch (e) {
    showError(e)
    return
  } finally {
    busy.value = false
  }
  await load()
}

async function copy(url: string) {
  try {
    await navigator.clipboard.writeText(url)
    toasts.success(t('webhooks.copied'))
  } catch {
    toasts.error(t('admin.copyFailed'), { detail: url })
  }
}

async function regenerate(hook: Webhook) {
  const ok = await confirm({
    title: t('webhooks.regenerateTitle', { name: hook.name }),
    body: t('webhooks.regenerateBody'),
    confirmLabel: t('webhooks.regenerate'),
    danger: true
  })
  if (!ok) return
  try {
    const created = await api(`/api/admin/webhooks/${hook.id}/token`, { method: 'POST', decode: decodeCreatedWebhook })
    revealed.value = { id: created.id, url: created.url }
  } catch (e) {
    showError(e)
  }
}

async function remove(hook: Webhook) {
  const ok = await confirm({
    title: t('webhooks.deleteTitle', { name: hook.name }),
    body: t('webhooks.deleteBody'),
    confirmLabel: t('common.delete'),
    danger: true
  })
  if (!ok) return
  try {
    await api(`/api/admin/webhooks/${hook.id}`, { method: 'DELETE' })
    hooks.value = hooks.value.filter(h => h.id !== hook.id)
    if (revealed.value?.id === hook.id) revealed.value = null
  } catch (e) {
    showError(e)
  }
}

async function setAvatar(hook: Webhook, event: Event) {
  const { file, tooBig } = pickImage(event.target as HTMLInputElement)
  if (tooBig) toasts.error(t('profile.avatarTooBig', { size: IMAGE_UPLOAD_MAX_MB }))
  if (!file) return
  const form = new FormData()
  form.append('avatar', file)
  try {
    const updated = await api(`/api/admin/webhooks/${hook.id}/avatar`, { method: 'POST', form, decode: decodeWebhook })
    hooks.value = hooks.value.map(h => (h.id === updated.id ? updated : h))
  } catch (e) {
    showError(e)
  }
}

onMounted(load)
</script>

<template>
  <div class="space-y-4" data-testid="incoming-webhooks">
    <p class="text-sm text-mnema-tertiary">{{ $t('webhooks.incomingHint') }}</p>

    <form class="flex gap-2" @submit.prevent="create">
      <input
        v-model="newName"
        type="text"
        v-max-chars="80"
        class="min-w-0 flex-1 rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-1.5 text-sm text-mnema-text"
        :placeholder="$t('webhooks.namePlaceholder')"
        :aria-label="$t('common.name')"
      />
      <button type="submit" :disabled="busy || !newName.trim()" class="btn btn-primary">{{ $t('webhooks.create') }}</button>
    </form>

    <div v-if="revealed" class="rounded-lg border border-mnema-accent/40 bg-mnema-accent-subtle p-3" role="status" data-testid="revealed-url">
      <p class="text-sm font-medium text-mnema-text">{{ $t('webhooks.urlOnce') }}</p>
      <div class="mt-2 flex items-center gap-2">
        <code class="min-w-0 flex-1 select-all break-all rounded-sm bg-mnema-surface px-2 py-1 font-mono text-xs text-mnema-text">{{ revealed.url }}</code>
        <button type="button" class="btn btn-secondary p-2" :aria-label="$t('webhooks.copyUrl')" @click="copy(revealed.url)">
          <Copy class="h-4 w-4" />
        </button>
      </div>
    </div>

    <StatePanel v-if="loadFailed" state="error" retryable data-testid="incoming-load-failed" @retry="load">{{ $t('webhooks.loadFailed') }}</StatePanel>
    <p v-else-if="loaded && !hooks.length" class="text-sm text-mnema-tertiary">{{ $t('webhooks.emptyIncoming') }}</p>
    <ul v-if="hooks.length" class="divide-y divide-mnema-hairline overflow-hidden rounded-lg border border-mnema-hairline bg-mnema-surface">
      <li v-for="hook in hooks" :key="hook.id" class="flex flex-wrap items-center gap-3 p-3" data-testid="incoming-webhook">
        <img v-if="hook.avatar_url" :src="hook.avatar_url" alt="" class="h-9 w-9 shrink-0 rounded-full object-cover" />
        <span v-else class="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-mnema-raised text-sm font-semibold text-mnema-muted" aria-hidden="true">{{ initialOf(hook.name) }}</span>

        <span class="min-w-0 flex-1 truncate text-sm font-medium text-mnema-text" data-testid="incoming-name">{{ hook.name }}</span>

        <div class="flex shrink-0 items-center gap-1 text-mnema-tertiary">
          <button type="button" class="btn btn-icon btn-sm hover:text-mnema-text" v-tooltip="$t('webhooks.settingsFor', { name: hook.name })" :aria-expanded="settingsOpen === hook.id" @click="toggleSettings(hook.id)">
            <SlidersHorizontal class="h-4 w-4" />
          </button>
          <label class="cursor-pointer rounded-sm p-1.5 transition hover:text-mnema-text" :title="$t('webhooks.avatar')">
            <ImagePlus class="h-4 w-4" aria-hidden="true" />
            <input type="file" accept="image/*" class="sr-only" :aria-label="$t('webhooks.avatarFor', { name: hook.name })" @change="setAvatar(hook, $event)" />
          </label>
          <button type="button" class="btn btn-icon btn-sm hover:text-mnema-text" v-tooltip="$t('webhooks.regenerateFor', { name: hook.name })" @click="regenerate(hook)">
            <KeyRound class="h-4 w-4" />
          </button>
          <button type="button" class="btn btn-icon btn-sm hover:text-mnema-danger" v-tooltip="$t('webhooks.deleteFor', { name: hook.name })" @click="remove(hook)">
            <Trash2 class="h-4 w-4" />
          </button>
        </div>
        <IncomingWebhookEditor v-if="settingsOpen === hook.id" :ref="setEditor" :hook="hook" @updated="onUpdated" />
      </li>
    </ul>
  </div>
</template>
