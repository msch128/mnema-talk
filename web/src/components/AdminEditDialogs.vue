<script setup lang="ts">
import type { PropType } from 'vue'
import type { Category, Channel } from '../types/domain'
import { computed } from 'vue'
import { normalise, useSettingsDraft } from '../composables/useSettingsDraft'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'
import UnsavedChangesBar from './UnsavedChangesBar.vue'

// The rename dialogs of the layout editor: a category's name, or a channel's
// name and topic. One draft with one Save; `submit` stores the trimmed fields
// (the editor's job) and a rejection is shown inline with the draft kept.
const props = defineProps({
  category: { type: Object as PropType<Category | null>, default: null },
  channel: { type: Object as PropType<Channel | null>, default: null },
  submit: { type: Function as PropType<(fields: { name: string; topic: string }) => Promise<void>>, required: true }
})
const emit = defineEmits<{ close: [] }>()

const form = useSettingsDraft<{ name: string; topic: string }>({
  source: () => ({ name: (props.category ?? props.channel)?.name || '', topic: props.channel?.topic || '' }),
  normalisers: { name: normalise.text, topic: normalise.text },
  validate: draft => (normalise.text(draft.name) ? {} : { name: t('channel.nameRequired') }),
  save: ({ draft }) => props.submit({ name: normalise.text(draft.name), topic: normalise.text(draft.topic) })
})
const { draft, errors } = form
const title = computed(() => props.category
  ? t('admin.editCategoryTitle', { name: props.category.name })
  : t('admin.editChannelTitle', { name: props.channel?.name ?? '' }))

const field = 'w-full bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent transition'
const label = 'block text-xs font-semibold text-mnema-tertiary uppercase tracking-wider mb-1.5 font-mono'
</script>

<template>
  <BaseDialog v-if="category || channel" :title="title" panel-class="max-w-md" :guard="form.guard" @close="emit('close')">
    <form class="flex min-h-0 flex-1 flex-col" data-testid="admin-edit-form" @submit.prevent="form.save" @keydown="form.onKeydown">
      <div class="min-h-0 flex-1 space-y-5 overflow-y-auto p-6">
        <div>
          <label for="admin-edit-name" :class="label">{{ category ? $t('admin.categoryNameLabel') : $t('admin.channelNameLabel') }}</label>
          <input
            id="admin-edit-name"
            v-model="draft.name"
            type="text"
            :placeholder="category ? $t('admin.categoryNamePlaceholder') : $t('admin.channelNamePlaceholder')"
            :aria-invalid="errors.name ? 'true' : undefined"
            :class="field"
          />
          <p v-if="errors.name" role="alert" class="mt-1 text-xs text-mnema-danger" data-testid="admin-edit-error">{{ errors.name }}</p>
        </div>
        <div v-if="channel">
          <label for="admin-edit-topic" :class="label">{{ $t('admin.channelTopicLabel') }}</label>
          <input id="admin-edit-topic" v-model="draft.topic" type="text" :placeholder="$t('admin.channelTopicPlaceholder')" :class="field" />
        </div>
      </div>
      <UnsavedChangesBar :dirty="form.dirty.value" :can-save="form.canSave.value" :saving="form.saving.value" :error="form.error.value" @save="form.save" @reset="form.reset" />
    </form>
  </BaseDialog>
</template>
