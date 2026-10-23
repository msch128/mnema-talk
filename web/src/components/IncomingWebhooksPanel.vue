<script setup lang="ts">
// Incoming webhooks of one channel: a secret URL scripts post to. The URL is
// shown once (the server keeps only a hash), so it is revealed right after
// creating or regenerating, with a copy button.
import { ref, onMounted } from 'vue'
import { Copy, KeyRound, Pencil, Trash2, ImagePlus, SlidersHorizontal } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import { arrayDecoder } from '../types/validation'
import { isWebhook, decodeCreatedWebhook, decodeWebhook } from '../types/webhooks'
import type { Webhook } from '../types/webhooks'
import { confirm } from '../lib/confirm'
import { useToastStore } from '../stores/toast'
import { useChatStore } from '../stores/chat'
import { t } from '../i18n'

const props = defineProps({
  channelId: { type: String, required: true }
})

const toasts = useToastStore()
const chat = useChatStore()
const hooks = ref<Webhook[]>([])
const loaded = ref(false)
const newName = ref('')
const busy = ref(false)
// The one URL on show: right after creating or regenerating.
const revealed = ref<{ id: string; url: string } | null>(null)
const renaming = ref<{ id: string; name: string } | null>(null)
// The webhook whose settings are open.
const settingsOpen = ref<string | null>(null)

// Each setting is a switch on the webhook; the label says what it allows.
const SETTINGS = ['allow_everyone', 'allow_user_mentions', 'allow_attachments', 'allow_embeds', 'allow_identity_override'] as const
type SettingKey = typeof SETTINGS[number]

function memberName(id: string | null): string {
  if (id === null) return t('webhooks.unknownMember')
  const m = chat.members.find(x => x.id === id)
  return m ? (m.display_name || m.username) : t('webhooks.unknownMember')
}

async function setSetting(hook: Webhook, key: SettingKey, value: boolean) {
  try {
    const updated = await api(`/api/admin/webhooks/${hook.id}`, { method: 'PATCH', json: { [key]: value }, decode: decodeWebhook })
    hooks.value = hooks.value.map(h => (h.id === updated.id ? updated : h))
  } catch (e) {
    showError(e)
  }
}

const base = `/api/admin/channels/${props.channelId}/webhooks`

function showError(e: unknown) {
  toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
}

async function load() {
  try {
    hooks.value = await api(base, { decode: arrayDecoder('webhooks', isWebhook) })
  } catch (e) {
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
    const created = await api(base, { method: 'POST', json: { name }, decode: decodeCreatedWebhook })
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

async function saveName() {
  const r = renaming.value
  if (!r) return
  const name = r.name.trim()
  renaming.value = null
  if (!name) return
  try {
    const updated = await api(`/api/admin/webhooks/${r.id}`, { method: 'PATCH', json: { name }, decode: decodeWebhook })
    hooks.value = hooks.value.map(h => (h.id === updated.id ? updated : h))
  } catch (e) {
    showError(e)
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
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
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
        maxlength="80"
        class="min-w-0 flex-1 rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-1.5 text-sm text-mnema-text"
        :placeholder="$t('webhooks.namePlaceholder')"
        :aria-label="$t('webhooks.name')"
      />
      <button type="submit" :disabled="busy || !newName.trim()" class="rounded-md bg-mnema-accent px-3.5 py-1.5 text-sm font-semibold text-mnema-accent-ink transition hover:bg-mnema-accent-hover disabled:opacity-40">{{ $t('webhooks.create') }}</button>
    </form>

    <div v-if="revealed" class="rounded-lg border border-mnema-accent/40 bg-mnema-accent-subtle p-3" role="status" data-testid="revealed-url">
      <p class="text-sm font-medium text-mnema-text">{{ $t('webhooks.urlOnce') }}</p>
      <div class="mt-2 flex items-center gap-2">
        <code class="min-w-0 flex-1 select-all break-all rounded bg-mnema-surface px-2 py-1 font-mono text-xs text-mnema-text">{{ revealed.url }}</code>
        <button type="button" class="rounded-md border border-mnema-border-strong p-2 text-mnema-text transition hover:bg-mnema-hover" :aria-label="$t('webhooks.copyUrl')" @click="copy(revealed.url)">
          <Copy class="h-4 w-4" />
        </button>
      </div>
    </div>

    <p v-if="loaded && !hooks.length" class="text-sm text-mnema-tertiary">{{ $t('webhooks.emptyIncoming') }}</p>
    <ul v-if="hooks.length" class="divide-y divide-mnema-hairline overflow-hidden rounded-lg border border-mnema-hairline bg-mnema-surface">
      <li v-for="hook in hooks" :key="hook.id" class="flex flex-wrap items-center gap-3 p-3" data-testid="incoming-webhook">
        <img v-if="hook.avatar_url" :src="hook.avatar_url" alt="" class="h-9 w-9 flex-shrink-0 rounded-full object-cover" />
        <span v-else class="flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-full bg-mnema-raised text-sm font-semibold text-mnema-muted" aria-hidden="true">{{ hook.name.slice(0, 1).toUpperCase() }}</span>

        <form v-if="renaming?.id === hook.id" class="min-w-0 flex-1" @submit.prevent="saveName">
          <input v-model="renaming.name" type="text" maxlength="80" autofocus class="w-full rounded-md border border-mnema-border-field bg-mnema-elevated px-2 py-1 text-sm text-mnema-text" :aria-label="$t('webhooks.name')" @keydown.esc="renaming = null" @blur="saveName" />
        </form>
        <span v-else class="min-w-0 flex-1 truncate text-sm font-medium text-mnema-text" data-testid="incoming-name">{{ hook.name }}</span>

        <div class="flex flex-shrink-0 items-center gap-1 text-mnema-tertiary">
          <button type="button" class="rounded p-1.5 transition hover:text-mnema-text" v-tooltip="$t('webhooks.settingsFor', { name: hook.name })" :aria-expanded="settingsOpen === hook.id" @click="settingsOpen = settingsOpen === hook.id ? null : hook.id">
            <SlidersHorizontal class="h-4 w-4" />
          </button>
          <button type="button" class="rounded p-1.5 transition hover:text-mnema-text" v-tooltip="$t('webhooks.rename', { name: hook.name })" @click="renaming = { id: hook.id, name: hook.name }">
            <Pencil class="h-4 w-4" />
          </button>
          <label class="cursor-pointer rounded p-1.5 transition hover:text-mnema-text" :title="$t('webhooks.avatar')">
            <ImagePlus class="h-4 w-4" aria-hidden="true" />
            <input type="file" accept="image/*" class="sr-only" :aria-label="$t('webhooks.avatarFor', { name: hook.name })" @change="setAvatar(hook, $event)" />
          </label>
          <button type="button" class="rounded p-1.5 transition hover:text-mnema-text" v-tooltip="$t('webhooks.regenerateFor', { name: hook.name })" @click="regenerate(hook)">
            <KeyRound class="h-4 w-4" />
          </button>
          <button type="button" class="rounded p-1.5 transition hover:text-mnema-danger" v-tooltip="$t('webhooks.deleteFor', { name: hook.name })" @click="remove(hook)">
            <Trash2 class="h-4 w-4" />
          </button>
        </div>
        <div v-if="settingsOpen === hook.id" class="basis-full space-y-1 pl-12 text-sm text-mnema-text" data-testid="webhook-settings">
          <p class="text-xs text-mnema-tertiary" data-testid="incoming-audit">{{ $t('webhooks.createdBy', { name: memberName(hook.created_by) }) }}</p>
          <label v-for="key in SETTINGS" :key="key" class="flex items-center gap-2">
            <input type="checkbox" :checked="hook[key]" :data-setting="key" @change="setSetting(hook, key, ($event.target as HTMLInputElement).checked)" />
            <span>{{ $t(`webhooks.setting.${key}`) }}</span>
          </label>
        </div>
      </li>
    </ul>
  </div>
</template>
