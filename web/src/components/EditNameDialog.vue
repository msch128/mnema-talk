<script setup lang="ts">
import type { PropType } from 'vue'
import type { Channel, Category } from '../types/domain'
import { caughtErrorMessage } from '../lib/api'
// Rename a channel (name + topic) or a category (name) from the sidebar menu.
// Without an entity it creates a new category instead (appended at the end).
import { ref, computed } from 'vue'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { nextSortOrder } from '../lib/channelLayout'
import { durationLabel } from '../lib/moderation'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'
import ChannelAutoDeleteField from './ChannelAutoDeleteField.vue'

const props = defineProps({
  kind: { type: String as PropType<'channel' | 'category'>, default: 'channel' }, // 'channel' | 'category'
  entity: { type: Object as PropType<Channel | Category | null>, default: null } // null: create a category
})
const emit = defineEmits<{ close: []; created: [category: Category] }>()

const chatStore = useChatStore()
const toasts = useToastStore()
const isCreate = computed(() => !props.entity)
const isChannel = computed(() => !isCreate.value && props.kind === 'channel')
const name = ref(props.entity?.name || '')
const topic = ref((props.entity && 'topic' in props.entity ? props.entity.topic : '') || '')
// Slow mode (seconds between two messages of a member); 0 = off.
const SLOW_MODE_CHOICES = [0, 5, 10, 15, 30, 60, 120, 300, 600, 900, 1800, 3600, 7200, 21600] as const
const slowMode = ref(props.entity && 'slow_mode_seconds' in props.entity ? props.entity.slow_mode_seconds : 0)
const slowModeChoices = computed(() => (SLOW_MODE_CHOICES as readonly number[]).includes(slowMode.value) ? SLOW_MODE_CHOICES : [...SLOW_MODE_CHOICES, slowMode.value].sort((a, b) => a - b))
function slowModeLabel(seconds: number): string {
  if (seconds === 0) return t('moderation.slowModeOff')
  return durationLabel(seconds)
}
const error = ref('')
const saving = ref(false)

const title = computed(() => {
  if (isCreate.value) return t('sidebar.createCategory')
  return t(isChannel.value ? 'admin.editChannelTitle' : 'admin.editCategoryTitle', { name: props.entity?.name })
})

async function submit() {
  error.value = ''
  const trimmed = name.value.trim()
  if (!trimmed) {
    error.value = t('channel.nameRequired')
    return
  }
  saving.value = true
  const entity = props.entity
  try {
    if (isCreate.value) {
      const created = await chatStore.createCategory(trimmed, nextSortOrder(chatStore.categories))
      toasts.success(t('sidebar.categoryCreated'))
      emit('created', created)
    } else if (entity && isChannel.value) {
      await chatStore.updateChannel(entity.id, { name: trimmed, topic: topic.value.trim(), slow_mode_seconds: slowMode.value })
      toasts.success(t('sidebar.channelUpdated'))
    } else if (entity) {
      await chatStore.updateCategory(entity.id, { name: trimmed })
      toasts.success(t('sidebar.categoryUpdated'))
    }
    emit('close')
  } catch (e) {
    error.value = caughtErrorMessage(e, t(isCreate.value ? 'sidebar.createCategoryFailed' : 'sidebar.updateFailed'))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <BaseDialog :title="title" @close="emit('close')">
    <form class="p-5 space-y-4 overflow-y-auto" @submit.prevent="submit">
      <div v-if="error" role="alert" class="p-2.5 rounded-lg bg-mnema-danger/10 border border-mnema-danger/30 text-mnema-danger text-sm">
        {{ error }}
      </div>

      <div class="stack-1.5">
        <label for="edit-name" class="text-xs font-medium text-mnema-tertiary uppercase tracking-wider font-mono">
          {{ $t(isChannel ? 'admin.channelNameLabel' : 'admin.categoryNameLabel') }}
        </label>
        <input
          id="edit-name"
          v-model="name"
          type="text"
          maxlength="64"
          :placeholder="isCreate ? $t('admin.categoryNamePlaceholder') : undefined"
          class="w-full bg-mnema-canvas border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary focus:outline-hidden focus:border-mnema-accent"
        />
      </div>

      <div v-if="isChannel && entity && 'type' in entity && entity.type !== 'voice'" class="stack-1.5">
        <label for="edit-topic" class="text-xs font-medium text-mnema-tertiary uppercase tracking-wider font-mono">
          {{ $t('admin.channelTopicLabel') }}
        </label>
        <input
          id="edit-topic"
          v-model="topic"
          type="text"
          maxlength="200"
          :placeholder="$t('admin.channelTopicPlaceholder')"
          class="w-full bg-mnema-canvas border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary focus:outline-hidden focus:border-mnema-accent"
        />
      </div>

      <div v-if="isChannel" class="stack-1.5">
        <label for="edit-slow-mode" class="text-xs font-medium text-mnema-tertiary uppercase tracking-wider font-mono">
          {{ $t('moderation.slowMode') }}
        </label>
        <select
          id="edit-slow-mode"
          v-model.number="slowMode"
          data-testid="slow-mode"
          class="w-full bg-mnema-canvas border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text focus:outline-hidden focus:border-mnema-accent"
        >
          <option v-for="s in slowModeChoices" :key="s" :value="s">{{ slowModeLabel(s) }}</option>
        </select>
        <p class="text-xs text-mnema-tertiary">{{ $t('moderation.slowModeHint') }}</p>
      </div>

      <ChannelAutoDeleteField v-if="isChannel && entity && 'type' in entity" :channel="entity" />

      <div class="pt-3 flex items-center justify-end gap-2 border-t border-mnema-hairline">
        <button
          type="button"
          class="btn btn-secondary h-8"
          @click="emit('close')"
        >
          {{ $t('common.cancel') }}
        </button>
        <button
          type="submit"
          :disabled="saving || !name.trim()"
          class="btn btn-primary h-8 disabled:cursor-not-allowed"
        >
          {{ isCreate ? $t('common.create') : $t('common.save') }}
        </button>
      </div>
    </form>
  </BaseDialog>
</template>
