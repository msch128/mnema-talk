<script setup>
// Rename a channel (name + topic) or a category (name) from the sidebar menu.
// Without an entity it creates a new category instead (appended at the end).
import { ref, computed } from 'vue'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { nextSortOrder } from '../lib/channelLayout'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'

const props = defineProps({
  kind: { type: String, default: 'channel' }, // 'channel' | 'category'
  entity: { type: Object, default: null } // null: create a category
})
const emit = defineEmits(['close', 'created'])

const chatStore = useChatStore()
const toasts = useToastStore()
const isCreate = computed(() => !props.entity)
const isChannel = computed(() => !isCreate.value && props.kind === 'channel')
const name = ref(props.entity?.name || '')
const topic = ref(props.entity?.topic || '')
const error = ref('')
const saving = ref(false)

const title = computed(() => {
  if (isCreate.value) return t('sidebar.createCategory')
  return t(isChannel.value ? 'admin.editChannelTitle' : 'admin.editCategoryTitle', { name: props.entity.name })
})

async function submit() {
  error.value = ''
  const trimmed = name.value.trim()
  if (!trimmed) {
    error.value = t('channel.nameRequired')
    return
  }
  saving.value = true
  try {
    if (isCreate.value) {
      const created = await chatStore.createCategory(trimmed, nextSortOrder(chatStore.categories))
      toasts.success(t('sidebar.categoryCreated'))
      emit('created', created)
    } else if (isChannel.value) {
      await chatStore.updateChannel(props.entity.id, { name: trimmed, topic: topic.value.trim() })
      toasts.success(t('sidebar.channelUpdated'))
    } else {
      await chatStore.updateCategory(props.entity.id, { name: trimmed })
      toasts.success(t('sidebar.categoryUpdated'))
    }
    emit('close')
  } catch (e) {
    error.value = e?.message || t(isCreate.value ? 'sidebar.createCategoryFailed' : 'sidebar.updateFailed')
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

      <div class="space-y-1.5">
        <label for="edit-name" class="text-xs font-medium text-mnema-tertiary uppercase tracking-wider font-mono">
          {{ $t(isChannel ? 'admin.channelNameLabel' : 'admin.categoryNameLabel') }}
        </label>
        <input
          id="edit-name"
          v-model="name"
          type="text"
          maxlength="64"
          :placeholder="isCreate ? $t('admin.categoryNamePlaceholder') : undefined"
          class="w-full bg-mnema-canvas border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary focus:outline-none focus:border-mnema-accent"
        />
      </div>

      <div v-if="isChannel && entity.type !== 'voice'" class="space-y-1.5">
        <label for="edit-topic" class="text-xs font-medium text-mnema-tertiary uppercase tracking-wider font-mono">
          {{ $t('admin.channelTopicLabel') }}
        </label>
        <input
          id="edit-topic"
          v-model="topic"
          type="text"
          maxlength="200"
          :placeholder="$t('admin.channelTopicPlaceholder')"
          class="w-full bg-mnema-canvas border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary focus:outline-none focus:border-mnema-accent"
        />
      </div>

      <div class="pt-3 flex items-center justify-end gap-2 border-t border-mnema-hairline">
        <button
          type="button"
          class="h-8 px-3 rounded-md border border-mnema-border hover:bg-mnema-hover text-mnema-text text-sm font-semibold transition"
          @click="emit('close')"
        >
          {{ $t('common.cancel') }}
        </button>
        <button
          type="submit"
          :disabled="saving || !name.trim()"
          class="h-8 px-3 rounded-md bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold text-sm transition disabled:opacity-30 disabled:cursor-not-allowed"
        >
          {{ isCreate ? $t('common.create') : $t('common.save') }}
        </button>
      </div>
    </form>
  </BaseDialog>
</template>
