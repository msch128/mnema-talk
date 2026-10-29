<script setup lang="ts">
import type { PropType } from 'vue'
import type { Channel, Category } from '../types/domain'
import { caughtErrorMessage } from '../lib/api'
// Edit a channel (name, topic, slow mode, auto-delete) or a category (name) from
// the sidebar menu. The edit is one draft with one Save (doc/settings-ux.md).
// Without an entity it creates a new category instead (appended at the end).
import { ref, computed } from 'vue'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { nextSortOrder } from '../lib/channelLayout'
import { confirmAutoDelete } from '../lib/autoDelete'
import { normalise, useSettingsDraft } from '../composables/useSettingsDraft'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'
import SlowModePicker from './SlowModePicker.vue'
import ChannelAutoDeleteField from './ChannelAutoDeleteField.vue'
import UnsavedChangesBar from './UnsavedChangesBar.vue'

interface ChannelForm { name: string; topic: string; slow: number; autoDelete: number }

const props = defineProps({
  kind: { type: String as PropType<'channel' | 'category'>, default: 'channel' }, // 'channel' | 'category'
  entity: { type: Object as PropType<Channel | Category | null>, default: null } // null: create a category
})
const emit = defineEmits<{ close: []; created: [category: Category] }>()

const chatStore = useChatStore()
const toasts = useToastStore()
const isCreate = computed(() => !props.entity)
const isChannel = computed(() => !isCreate.value && props.kind === 'channel')
const MAX_TOPIC = 255
const slowModeValid = ref(true)

// The store replaces channel objects on every refresh: read the saved values from there.
const live = computed(() => {
  const entity = props.entity
  if (!entity) return null
  const found = isChannel.value ? chatStore.allChannels.find(c => c.id === entity.id) : chatStore.categories.find(c => c.id === entity.id)
  return found ?? entity
})

const form = useSettingsDraft<ChannelForm>({
  source: () => {
    const e = live.value
    return {
      name: e?.name || '',
      topic: (e && 'topic' in e ? e.topic : '') || '',
      slow: e && 'slow_mode_seconds' in e ? e.slow_mode_seconds : 0,
      autoDelete: (e && 'auto_delete_days' in e ? e.auto_delete_days : 0) ?? 0
    }
  },
  normalisers: { name: normalise.text, topic: normalise.text, slow: normalise.count, autoDelete: normalise.count },
  validate: draft => (normalise.text(draft.name) ? {} : { name: t('channel.nameRequired') }),
  blocked: () => !slowModeValid.value,
  save: async ({ changed, draft, commit }) => {
    const entity = props.entity
    if (!entity) return
    if (!isChannel.value) {
      await chatStore.updateCategory(entity.id, { name: normalise.text(draft.name) })
      return
    }
    if (changed.some(k => k !== 'autoDelete')) {
      await chatStore.updateChannel(entity.id, { name: normalise.text(draft.name), topic: normalise.text(draft.topic), slow_mode_seconds: normalise.count(draft.slow) })
      commit(changed.filter(k => k !== 'autoDelete'))
    }
    if (changed.includes('autoDelete')) {
      await chatStore.setChannelAutoDelete(entity.id, normalise.count(draft.autoDelete))
      commit(['autoDelete'])
    }
  },
  saved: () => {
    toasts.success(t(isChannel.value ? 'sidebar.channelUpdated' : 'sidebar.categoryUpdated'))
    emit('close')
  }
})
const { draft, errors } = form

// Create: a new category, no draft to keep.
const createName = ref('')
const createError = ref('')
const creating = ref(false)

async function create() {
  createError.value = ''
  const trimmed = createName.value.trim()
  if (!trimmed) {
    createError.value = t('channel.nameRequired')
    return
  }
  creating.value = true
  try {
    const created = await chatStore.createCategory(trimmed, nextSortOrder(chatStore.categories))
    toasts.success(t('sidebar.categoryCreated'))
    emit('created', created)
    emit('close')
  } catch (e) {
    createError.value = caughtErrorMessage(e, t('sidebar.createCategoryFailed'))
  } finally {
    creating.value = false
  }
}

// Saving asks first when auto-delete is switched on: it destroys messages.
async function submit() {
  if (isCreate.value) return create()
  if (!form.canSave.value) return
  if (form.changed.value.includes('autoDelete') && !(await confirmAutoDelete(live.value?.name ?? '', normalise.count(draft.autoDelete)))) return
  await form.save()
}

const guard = () => isCreate.value || form.guard()

function onKeydown(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === 's') {
    e.preventDefault()
    void submit()
  }
}

const title = computed(() => {
  if (isCreate.value) return t('sidebar.createCategory')
  return t(isChannel.value ? 'admin.editChannelTitle' : 'admin.editCategoryTitle', { name: props.entity?.name })
})

const field = 'w-full bg-mnema-canvas border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary focus:outline-hidden focus:border-mnema-accent'
const label = 'text-xs font-medium text-mnema-tertiary uppercase tracking-wider font-mono'
</script>

<template>
  <BaseDialog :title="title" :guard="guard" @close="emit('close')">
    <form class="flex min-h-0 flex-1 flex-col" data-testid="edit-name-form" @submit.prevent="submit" @keydown="onKeydown">
      <div class="min-h-0 flex-1 space-y-5 overflow-y-auto p-6">
        <div v-if="createError" role="alert" class="p-2.5 rounded-lg bg-mnema-danger/10 border border-mnema-danger/30 text-mnema-danger text-sm">
          {{ createError }}
        </div>

        <div class="stack-1.5">
          <label for="edit-name" :class="label">
            {{ $t(isChannel ? 'admin.channelNameLabel' : 'admin.categoryNameLabel') }}
          </label>
          <input
            v-if="isCreate"
            id="edit-name"
            v-model="createName"
            type="text"
            maxlength="64"
            :placeholder="$t('admin.categoryNamePlaceholder')"
            :class="field"
          />
          <input v-else id="edit-name" v-model="draft.name" type="text" maxlength="64" :aria-invalid="errors.name ? 'true' : undefined" :class="field" />
          <p v-if="errors.name" role="alert" class="text-xs text-mnema-danger" data-testid="edit-name-error">{{ errors.name }}</p>
        </div>

        <div v-if="isChannel && entity && 'type' in entity && entity.type !== 'voice'" class="stack-1.5">
          <label for="edit-topic" :class="label">
            {{ $t('admin.channelTopicLabel') }}
          </label>
          <textarea
            id="edit-topic"
            v-model="draft.topic"
            rows="2"
            :maxlength="MAX_TOPIC"
            :placeholder="$t('admin.channelTopicPlaceholder')"
            aria-describedby="edit-topic-help"
            :class="[field, 'resize-none']"
          ></textarea>
          <p id="edit-topic-help" class="flex justify-between gap-2 text-xs text-mnema-tertiary">
            <span>{{ $t('admin.channelTopicHelp') }}</span>
            <span class="tabular-nums shrink-0">{{ draft.topic.length }}/{{ MAX_TOPIC }}</span>
          </p>
        </div>

        <div v-if="isChannel" class="stack-1.5">
          <span :class="label">
            {{ $t('moderation.slowMode') }}
          </span>
          <SlowModePicker v-model="draft.slow" v-model:valid="slowModeValid" />
        </div>

        <ChannelAutoDeleteField v-if="isChannel && entity && 'type' in entity" v-model="draft.autoDelete" />
      </div>

      <div v-if="isCreate" class="flex items-center justify-end gap-2 border-t border-mnema-hairline bg-mnema-raised px-5 py-3">
        <button type="button" class="btn btn-secondary h-9" @click="emit('close')">{{ $t('common.cancel') }}</button>
        <button type="submit" :disabled="creating || !createName.trim()" class="btn btn-primary h-9 disabled:cursor-not-allowed">{{ $t('common.create') }}</button>
      </div>
      <UnsavedChangesBar v-else :dirty="form.dirty.value" :can-save="form.canSave.value" :saving="form.saving.value" :error="form.error.value" @save="submit" @reset="form.reset" />
    </form>
  </BaseDialog>
</template>
