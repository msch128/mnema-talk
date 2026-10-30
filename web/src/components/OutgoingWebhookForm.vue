<script setup lang="ts">
// Create or edit one outgoing webhook as one draft with one Save
// (doc/settings-ux.md). For an existing webhook the on/off switch is a field of
// the draft too; test, new secret and delete stay actions on the row.
import type { PropType } from 'vue'
import { api } from '../lib/api'
import { decodeCreatedOutgoingWebhook, decodeOutgoingWebhook, CHANNEL_WEBHOOK_EVENTS } from '../types/webhooks'
import type { OutgoingWebhook } from '../types/webhooks'
import { normalise, useSettingsDraft } from '../composables/useSettingsDraft'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import UnsavedChangesBar from './UnsavedChangesBar.vue'

const props = defineProps({
  channelId: { type: String, required: true },
  /** The webhook to edit; none creates a new one. */
  hook: { type: Object as PropType<OutgoingWebhook | null>, default: null }
})
const emit = defineEmits<{ saved: [secret: { id: string; value: string } | null]; cancel: [] }>()

const toasts = useToastStore()
const allEvents: readonly string[] = CHANNEL_WEBHOOK_EVENTS
const eventLabel = (event: string) => t(`webhooks.events.${event}`)

// The secret shown once after creating; null when nothing is to be revealed.
let secret: { id: string; value: string } | null = null

interface HookForm { name: string; url: string; events: string[]; enabled: boolean }

const form = useSettingsDraft<HookForm>({
  source: () => props.hook
    ? { name: props.hook.name, url: props.hook.url, events: [...props.hook.events], enabled: props.hook.status === 'active' }
    : { name: '', url: '', events: ['MESSAGE_CREATE'], enabled: true },
  normalisers: { name: normalise.text, url: normalise.text, events: normalise.set, enabled: normalise.flag },
  validate: draft => {
    const found: Partial<Record<keyof HookForm, string>> = {}
    if (!normalise.text(draft.name)) found.name = t('bots.nameRequired')
    if (!normalise.text(draft.url)) found.url = t('webhooks.urlRequired')
    if (draft.events.length === 0) found.events = t('webhooks.eventsRequired')
    return found
  },
  save: async ({ changes, changed, draft, commit }) => {
    if (props.hook) {
      const json: Record<string, string | string[] | boolean> = {}
      if (changed.includes('name')) json['name'] = normalise.text(draft.name)
      if (changed.includes('url')) json['url'] = normalise.text(draft.url)
      if (changed.includes('events')) json['events'] = changes.events ?? draft.events
      if (changed.includes('enabled')) json['enabled'] = draft.enabled
      await api(`/api/admin/outgoing-webhooks/${props.hook.id}`, { method: 'PATCH', json, decode: decodeOutgoingWebhook })
      secret = null
    } else {
      const body = { name: normalise.text(draft.name), url: normalise.text(draft.url), events: draft.events }
      const created = await api(`/api/admin/channels/${props.channelId}/outgoing-webhooks`, { method: 'POST', json: body, decode: decodeCreatedOutgoingWebhook })
      secret = { id: created.id, value: created.secret }
    }
    commit(changed)
  },
  saved: () => {
    toasts.success(t(props.hook ? 'webhooks.saved' : 'webhooks.created'))
    emit('saved', secret)
  }
})
const { draft, errors } = form

function toggleEvent(event: string) {
  draft.events = draft.events.includes(event) ? draft.events.filter(e => e !== event) : [...draft.events, event]
}

/** Cancel asks first when there are unsaved edits. */
async function cancel() {
  if (await form.guard()) emit('cancel')
}

defineExpose({ guard: form.guard })
</script>

<template>
  <form class="overflow-hidden rounded-lg border border-mnema-border bg-mnema-surface" data-testid="outgoing-form" @submit.prevent="form.save" @keydown="form.onKeydown">
    <div class="space-y-4 p-4">
      <label class="block text-sm text-mnema-text">
        <span class="mb-1 block text-xs font-medium text-mnema-muted">{{ $t('common.name') }}</span>
        <input v-model="draft.name" type="text" v-max-chars="80" class="w-full rounded-md border border-mnema-border-field bg-mnema-elevated px-3 py-1.5 text-sm text-mnema-text" />
      </label>
      <label class="block text-sm text-mnema-text">
        <span class="mb-1 block text-xs font-medium text-mnema-muted">{{ $t('webhooks.url') }}</span>
        <input v-model="draft.url" type="url" maxlength="2048" :placeholder="'https://example.org/hook'" class="w-full rounded-md border border-mnema-border-field bg-mnema-elevated px-3 py-1.5 font-mono text-sm text-mnema-text" />
      </label>
      <fieldset>
        <legend class="mb-1 text-xs font-medium text-mnema-muted">{{ $t('webhooks.eventsLabel') }}</legend>
        <div class="grid gap-1 sm:grid-cols-2">
          <label v-for="event in allEvents" :key="event" class="flex items-center gap-2 text-sm text-mnema-text">
            <input type="checkbox" :checked="draft.events.includes(event)" :data-event="event" @change="toggleEvent(event)" />
            <span>{{ eventLabel(event) }}</span>
          </label>
        </div>
        <p v-if="errors.events" class="mt-1 text-xs text-mnema-danger" data-testid="outgoing-events-error">{{ errors.events }}</p>
      </fieldset>
      <label v-if="hook" class="flex items-center gap-2 text-sm text-mnema-text">
        <input v-model="draft.enabled" type="checkbox" data-testid="outgoing-enabled" />
        <span>{{ $t('webhooks.enabledLabel') }}</span>
      </label>
      <div class="flex justify-start">
        <button type="button" class="btn btn-ghost btn-sm" data-testid="outgoing-cancel" @click="cancel">{{ $t('common.cancel') }}</button>
      </div>
    </div>
    <UnsavedChangesBar :dirty="form.dirty.value" :can-save="form.canSave.value" :saving="form.saving.value" :error="form.error.value" @save="form.save" @reset="form.reset" />
  </form>
</template>
