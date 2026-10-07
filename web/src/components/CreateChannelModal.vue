<script setup lang="ts">
import type { PropType } from 'vue'
import type { Channel, ChannelType } from '../types/domain'
import { caughtErrorMessage } from '../lib/api'
import { ref } from 'vue'
import { Hash, Volume2, FolderPlus, Plus, AlertCircle } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'

const props = defineProps({
  initialType: {
    type: String as PropType<ChannelType>,
    default: 'text' // 'text' | 'voice'
  },
  initialCategoryId: {
    type: String,
    default: ''
  }
})

const emit = defineEmits<{ close: []; created: [channel: Channel] }>()
const chatStore = useChatStore()

const channelType = ref(props.initialType || 'text')
const channelName = ref('')
const selectedCategoryId = ref(props.initialCategoryId || '')
const channelTopic = ref('')
const isCreatingCategory = ref(false)
const newCategoryName = ref('')
const error = ref('')
const isSubmitting = ref(false)

// Slugify helper for text channels
function formatName(val: string) {
  if (channelType.value === 'text') {
    return val.toLowerCase().replace(/\s+/g, '-').replace(/[^a-z0-9-_]/g, '')
  }
  return val
}

function handleNameInput(e: Event) {
  if (channelType.value === 'text') {
    channelName.value = formatName((e.target as HTMLInputElement).value)
  } else {
    channelName.value = (e.target as HTMLInputElement).value
  }
}

async function handleSubmit() {
  error.value = ''
  if (!channelName.value.trim()) {
    error.value = t('channel.nameRequired')
    return
  }

  isSubmitting.value = true
  try {
    let catId = selectedCategoryId.value || null

    // If a new category was chosen inline
    if (isCreatingCategory.value && newCategoryName.value.trim()) {
      const newCat = await chatStore.createCategory(newCategoryName.value.trim(), chatStore.categories.length)
      catId = newCat.id
    }

    const createdChannel = await chatStore.createChannel({
      categoryId: catId,
      name: channelName.value.trim(),
      type: channelType.value,
      topic: channelTopic.value.trim()
    })

    emit('created', createdChannel)
    emit('close')
  } catch (err) {
    error.value = caughtErrorMessage(err, t('channel.createFailed'))
  } finally {
    isSubmitting.value = false
  }
}
</script>

<template>
  <BaseDialog :title="$t('channel.createTitle')" @close="emit('close')">
    <form @submit.prevent="handleSubmit" class="p-5 space-y-4 overflow-y-auto">
      <!-- Error -->
      <div v-if="error" role="alert" class="p-2.5 rounded-lg bg-mnema-danger/10 border border-mnema-danger/30 text-mnema-danger text-sm flex items-center gap-2">
        <AlertCircle class="w-4 h-4 flex-shrink-0" />
        <span>{{ error }}</span>
      </div>

      <!-- 1. Channel type -->
      <fieldset class="space-y-1.5">
        <legend class="text-xs font-medium text-mnema-tertiary uppercase tracking-wider font-mono mb-1.5">{{ $t('channel.type') }}</legend>
        <div class="grid grid-cols-2 gap-2">
          <button
            type="button"
            @click="channelType = 'text'"
            :aria-pressed="channelType === 'text' ? 'true' : 'false'"
            :class="[
              'flex items-center gap-2.5 p-3 rounded-lg border text-left transition min-w-0',
              channelType === 'text'
                ? 'bg-mnema-surface border-mnema-accent text-mnema-text ring-1 ring-mnema-accent/40'
                : 'bg-mnema-canvas border-mnema-hairline text-mnema-muted hover:border-mnema-border'
            ]"
          >
            <div :class="['w-7 h-7 rounded-md flex items-center justify-center flex-shrink-0', channelType === 'text' ? 'bg-mnema-accent/20 text-mnema-accent' : 'bg-mnema-raised text-mnema-tertiary']">
              <Hash class="w-4 h-4" />
            </div>
            <div class="min-w-0">
              <div class="text-sm font-semibold truncate">{{ $t('channel.typeText') }}</div>
              <div class="text-xs text-mnema-tertiary">{{ $t('channel.typeTextHint') }}</div>
            </div>
          </button>

          <button
            type="button"
            @click="channelType = 'voice'"
            :aria-pressed="channelType === 'voice' ? 'true' : 'false'"
            :class="[
              'flex items-center gap-2.5 p-3 rounded-lg border text-left transition min-w-0',
              channelType === 'voice'
                ? 'bg-mnema-surface border-mnema-accent text-mnema-text ring-1 ring-mnema-accent/40'
                : 'bg-mnema-canvas border-mnema-hairline text-mnema-muted hover:border-mnema-border'
            ]"
          >
            <div :class="['w-7 h-7 rounded-md flex items-center justify-center flex-shrink-0', channelType === 'voice' ? 'bg-mnema-accent/20 text-mnema-accent' : 'bg-mnema-raised text-mnema-tertiary']">
              <Volume2 class="w-4 h-4" />
            </div>
            <div class="min-w-0">
              <div class="text-sm font-semibold truncate">{{ $t('channel.typeVoice') }}</div>
              <div class="text-xs text-mnema-tertiary">{{ $t('channel.typeVoiceHint') }}</div>
            </div>
          </button>
        </div>
      </fieldset>

      <!-- 2. Category -->
      <div class="space-y-1.5">
        <div class="flex items-center justify-between gap-2">
          <label for="channel-category" class="text-xs font-medium text-mnema-tertiary uppercase tracking-wider font-mono">{{ $t('channel.category') }}</label>
          <button
            type="button"
            @click="isCreatingCategory = !isCreatingCategory"
            class="text-xs text-mnema-accent hover:underline flex items-center gap-1 font-mono min-w-0"
          >
            <FolderPlus class="w-3.5 h-3.5 flex-shrink-0" />
            <span class="truncate">{{ isCreatingCategory ? $t('channel.chooseExisting') : $t('channel.newCategory') }}</span>
          </button>
        </div>

        <select
          v-if="!isCreatingCategory"
          id="channel-category"
          v-model="selectedCategoryId"
          class="w-full bg-mnema-canvas border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text focus:outline-none focus:border-mnema-accent"
        >
          <option value="">{{ $t('channel.noCategory') }}</option>
          <option v-for="cat in chatStore.categories" :key="cat.id" :value="cat.id">
            {{ cat.name }}
          </option>
        </select>

        <div v-else class="space-y-1">
          <input
            id="channel-category"
            v-model="newCategoryName"
            type="text"
            :placeholder="$t('channel.newCategoryPlaceholder')"
            class="w-full bg-mnema-canvas border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary focus:outline-none focus:border-mnema-accent"
          />
          <p class="text-xs text-mnema-tertiary">
            {{ $t('channel.newCategoryHint') }}
          </p>
        </div>
      </div>

      <!-- 3. Name -->
      <div class="space-y-1.5">
        <label for="channel-name" class="text-xs font-medium text-mnema-tertiary uppercase tracking-wider font-mono block">{{ $t('channel.name') }}</label>
        <div class="relative flex items-center">
          <span class="absolute left-3 text-mnema-tertiary select-none" aria-hidden="true">
            <Hash v-if="channelType === 'text'" class="w-4 h-4" />
            <Volume2 v-else class="w-4 h-4" />
          </span>
          <input
            id="channel-name"
            v-model="channelName"
            @input="handleNameInput"
            type="text"
            :placeholder="channelType === 'text' ? $t('channel.namePlaceholderText') : $t('channel.namePlaceholderVoice')"
            required
            aria-describedby="channel-name-hint"
            class="w-full bg-mnema-canvas border border-mnema-border-field rounded-md pl-9 pr-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary focus:outline-none focus:border-mnema-accent font-mono"
          />
        </div>
        <p id="channel-name-hint" class="text-xs text-mnema-tertiary">
          {{ channelType === 'text' ? $t('channel.nameHintText') : $t('channel.nameHintVoice') }}
        </p>
      </div>

      <!-- 4. Topic -->
      <div class="space-y-1.5">
        <label for="channel-topic" class="text-xs font-medium text-mnema-tertiary uppercase tracking-wider font-mono block">{{ $t('channel.topic') }}</label>
        <input
          id="channel-topic"
          v-model="channelTopic"
          type="text"
          :placeholder="$t('channel.topicPlaceholder')"
          class="w-full bg-mnema-canvas border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary focus:outline-none focus:border-mnema-accent"
        />
      </div>

      <!-- Actions -->
      <div class="pt-3 flex items-center justify-end gap-2 border-t border-mnema-hairline">
        <button
          type="button"
          @click="emit('close')"
          class="h-8 px-3 rounded-md border border-mnema-border hover:bg-mnema-hover text-mnema-text text-sm font-semibold transition"
        >
          {{ $t('common.cancel') }}
        </button>
        <button
          type="submit"
          :disabled="isSubmitting || !channelName.trim()"
          class="h-8 px-3 rounded-md bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold text-sm transition disabled:opacity-30 disabled:cursor-not-allowed flex items-center gap-1.5"
        >
          <Plus class="w-4 h-4" />
          <span>{{ isSubmitting ? $t('channel.creating') : $t('channel.create') }}</span>
        </button>
      </div>
    </form>
  </BaseDialog>
</template>
