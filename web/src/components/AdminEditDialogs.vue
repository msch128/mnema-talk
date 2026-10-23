<script setup lang="ts">
import type { PropType } from 'vue'
import type { Category, Channel } from '../types/domain'
import { ref } from 'vue'
import BaseDialog from './BaseDialog.vue'

// The rename dialogs of the layout editor: a category's name, or a channel's
// name and topic. They hand the trimmed fields back; saving is the editor's.
const props = defineProps({
  category: { type: Object as PropType<Category | null>, default: null },
  channel: { type: Object as PropType<Channel | null>, default: null }
})
const emit = defineEmits<{
  close: []
  'save-category': [name: string]
  'save-channel': [fields: { name: string; topic: string }]
}>()

const categoryName = ref(props.category?.name || '')
const channelName = ref(props.channel?.name || '')
const channelTopic = ref(props.channel?.topic || '')

function saveCategory() {
  const name = categoryName.value.trim()
  if (name) emit('save-category', name)
}

function saveChannel() {
  const name = channelName.value.trim()
  if (name) emit('save-channel', { name, topic: channelTopic.value.trim() })
}

const field = 'w-full bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent transition'
const label = 'block text-xs font-semibold text-mnema-tertiary uppercase tracking-wider mb-1.5 font-mono'
const cancelButton = 'btn btn-secondary'
const saveButton = 'btn btn-primary'
</script>

<template>
  <BaseDialog
    v-if="category"
    :title="$t('admin.editCategoryTitle', { name: category.name })"
    panel-class="max-w-md"
    @close="emit('close')"
  >
    <form @submit.prevent="saveCategory" class="p-6 space-y-4">
      <div>
        <label :class="label">{{ $t('admin.categoryNameLabel') }}</label>
        <input
          v-model="categoryName"
          type="text"
          :placeholder="$t('admin.categoryNamePlaceholder')"
          :aria-label="$t('admin.categoryNameLabel')"
          :class="field"
        />
      </div>
      <div class="flex justify-end gap-2 pt-2">
        <button type="button" :class="cancelButton" @click="emit('close')">{{ $t('common.cancel') }}</button>
        <button type="submit" :disabled="!categoryName.trim()" :class="saveButton">{{ $t('common.save') }}</button>
      </div>
    </form>
  </BaseDialog>

  <BaseDialog
    v-if="channel"
    :title="$t('admin.editChannelTitle', { name: channel.name })"
    panel-class="max-w-md"
    @close="emit('close')"
  >
    <form @submit.prevent="saveChannel" class="p-6 space-y-4">
      <div>
        <label :class="label">{{ $t('admin.channelNameLabel') }}</label>
        <input
          v-model="channelName"
          type="text"
          :placeholder="$t('admin.channelNamePlaceholder')"
          :aria-label="$t('admin.channelNameLabel')"
          :class="field"
        />
      </div>
      <div>
        <label :class="label">{{ $t('admin.channelTopicLabel') }}</label>
        <input
          v-model="channelTopic"
          type="text"
          :placeholder="$t('admin.channelTopicPlaceholder')"
          :aria-label="$t('admin.channelTopicLabel')"
          :class="field"
        />
      </div>
      <div class="flex justify-end gap-2 pt-2">
        <button type="button" :class="cancelButton" @click="emit('close')">{{ $t('common.cancel') }}</button>
        <button type="submit" :disabled="!channelName.trim()" :class="saveButton">{{ $t('common.save') }}</button>
      </div>
    </form>
  </BaseDialog>
</template>
