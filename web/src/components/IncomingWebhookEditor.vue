<script setup lang="ts">
// The settings of one incoming webhook (name and what its posts may do) as one
// draft with one Save (doc/settings-ux.md). The avatar, a new URL and deleting
// are actions on the row and run at once.
import type { PropType } from 'vue'
import { api } from '../lib/api'
import { decodeWebhook } from '../types/webhooks'
import type { Webhook } from '../types/webhooks'
import { normalise, useSettingsDraft } from '../composables/useSettingsDraft'
import { useToastStore } from '../stores/toast'
import { useChatStore } from '../stores/chat'
import { t } from '../i18n'
import UnsavedChangesBar from './UnsavedChangesBar.vue'

// Each setting is a switch on the webhook; the label says what it allows.
const SETTINGS = ['allow_everyone', 'allow_user_mentions', 'allow_attachments', 'allow_embeds', 'allow_identity_override'] as const

const props = defineProps({
  hook: { type: Object as PropType<Webhook>, required: true }
})
const emit = defineEmits<{ updated: [hook: Webhook] }>()

const toasts = useToastStore()
const chat = useChatStore()

type EditorForm = { name: string } & Record<typeof SETTINGS[number], boolean>

const form = useSettingsDraft<EditorForm>({
  source: () => ({
    name: props.hook.name,
    allow_everyone: props.hook.allow_everyone,
    allow_user_mentions: props.hook.allow_user_mentions,
    allow_attachments: props.hook.allow_attachments,
    allow_embeds: props.hook.allow_embeds,
    allow_identity_override: props.hook.allow_identity_override
  }),
  normalisers: { name: normalise.text },
  validate: draft => (normalise.text(draft.name) ? {} : { name: t('bots.nameRequired') }),
  save: async ({ changes, changed, commit }) => {
    const json: Record<string, string | boolean> = { ...changes }
    if (typeof changes.name === 'string') json['name'] = normalise.text(changes.name)
    emit('updated', await api(`/api/admin/webhooks/${props.hook.id}`, { method: 'PATCH', json, decode: decodeWebhook }))
    commit(changed)
  },
  saved: () => { toasts.success(t('webhooks.saved')) }
})
const { draft, errors } = form

function memberName(id: string | null): string {
  if (id === null) return t('webhooks.unknownMember')
  const m = chat.members.find(x => x.id === id)
  return m ? (m.display_name || m.username) : t('webhooks.unknownMember')
}

defineExpose({ guard: form.guard })
</script>

<template>
  <form class="basis-full overflow-hidden rounded-lg border border-mnema-hairline bg-mnema-elevated" data-testid="webhook-settings" @submit.prevent="form.save" @keydown="form.onKeydown">
    <div class="space-y-3 p-4 text-sm text-mnema-text">
      <label class="block">
        <span class="mb-1 block text-xs font-medium text-mnema-muted">{{ $t('common.name') }}</span>
        <input v-model="draft.name" type="text" v-max-chars="80" :aria-invalid="errors.name ? 'true' : undefined" class="w-full rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-1.5 text-sm text-mnema-text" data-testid="incoming-edit-name" />
        <span v-if="errors.name" class="mt-1 block text-xs text-mnema-danger">{{ errors.name }}</span>
      </label>
      <p class="text-xs text-mnema-tertiary" data-testid="incoming-audit">{{ $t('webhooks.createdBy', { name: memberName(hook.created_by) }) }}</p>
      <label v-for="key in SETTINGS" :key="key" class="flex items-center gap-2">
        <input v-model="draft[key]" type="checkbox" :data-setting="key" />
        <span>{{ $t(`webhooks.setting.${key}`) }}</span>
      </label>
    </div>
    <UnsavedChangesBar :dirty="form.dirty.value" :can-save="form.canSave.value" :saving="form.saving.value" :error="form.error.value" @save="form.save" @reset="form.reset" />
  </form>
</template>
