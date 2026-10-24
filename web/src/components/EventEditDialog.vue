<script setup lang="ts">
import { computed, ref } from 'vue'
import { Image as ImageIcon, Trash2 } from '@lucide/vue'
import BaseDialog from './BaseDialog.vue'
import { useEventsStore } from '../stores/events'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import type { CommunityEvent } from '../types/domain'
import { t } from '../i18n'

const props = defineProps<{
  event?: CommunityEvent | null
}>()

const emit = defineEmits<{
  close: []
  saved: [event: CommunityEvent]
}>()

const eventsStore = useEventsStore()
const chatStore = useChatStore()
const toast = useToastStore()

const isEdit = computed(() => !!props.event)
const title = ref(props.event?.title || '')
const description = ref(props.event?.description || '')

function toLocalIso(dateStr?: string | null): string {
  if (!dateStr) return ''
  const d = new Date(dateStr)
  if (isNaN(d.getTime())) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

const startTime = ref(toLocalIso(props.event?.start_time))
const endTime = ref(toLocalIso(props.event?.end_time))

const locationType = ref<'talk' | 'text'>(props.event?.channel_id ? 'talk' : 'text')
const channelId = ref(props.event?.channel_id || '')
const locationText = ref(props.event?.location || '')

const coverFile = ref<File | null>(null)
const coverPreview = ref<string | null>(props.event?.cover_url || null)
const removeExistingCover = ref(false)

const saving = ref(false)
const error = ref('')

const voiceChannels = computed(() => chatStore.allChannels.filter(c => c.type === 'voice'))

function onCoverSelected(e: Event) {
  const target = e.target as HTMLInputElement
  if (target.files && target.files[0]) {
    coverFile.value = target.files[0]
    coverPreview.value = URL.createObjectURL(target.files[0])
    removeExistingCover.value = false
  }
}

function removeCover() {
  coverFile.value = null
  coverPreview.value = null
  if (props.event?.cover_url) {
    removeExistingCover.value = true
  }
}

function validateForm(trimmedTitle: string): string | null {
  if (!trimmedTitle) return t('events.titlePlaceholder')
  if (!startTime.value) return t('events.startTime')
  const startIso = new Date(startTime.value).toISOString()
  const endIso = endTime.value ? new Date(endTime.value).toISOString() : null
  if (endIso && new Date(endIso) < new Date(startIso)) return t('events.endTime')
  if (locationType.value === 'talk' && !channelId.value) return t('events.selectTalk')
  if (locationType.value === 'text' && !locationText.value.trim()) return t('events.locationTextPlaceholder')
  return null
}

function buildPayload(trimmedTitle: string) {
  return {
    title: trimmedTitle,
    description: description.value,
    start_time: new Date(startTime.value).toISOString(),
    end_time: endTime.value ? new Date(endTime.value).toISOString() : null,
    channel_id: locationType.value === 'talk' ? channelId.value : null,
    location: locationType.value === 'text' ? locationText.value.trim() : null
  }
}

async function applyCover(id: string, current: CommunityEvent): Promise<CommunityEvent> {
  if (removeExistingCover.value) return eventsStore.deleteCover(id)
  if (coverFile.value) return eventsStore.uploadCover(id, coverFile.value)
  return current
}

async function submit() {
  error.value = ''
  const trimmedTitle = title.value.trim()
  const validationErr = validateForm(trimmedTitle)
  if (validationErr) {
    error.value = validationErr
    return
  }

  saving.value = true
  try {
    const payload = buildPayload(trimmedTitle)
    let result: CommunityEvent
    if (isEdit.value && props.event) {
      result = await eventsStore.update(props.event.id, payload)
      result = await applyCover(result.id, result)
      toast.success(t('events.updated'))
    } else {
      result = await eventsStore.create(payload)
      result = await applyCover(result.id, result)
      toast.success(t('events.created'))
    }
    emit('saved', result)
    emit('close')
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Operation failed'
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <BaseDialog
    :title="isEdit ? $t('events.editTitle') : $t('events.createTitle')"
    panel-class="max-w-lg"
    @close="emit('close')"
  >
    <form class="flex flex-col gap-4 p-6" @submit.prevent="submit">
      <div v-if="error" class="rounded-lg bg-red-500/10 p-3 text-sm text-red-400">
        {{ error }}
      </div>

      <!-- Title -->
      <div>
        <label class="mb-1 block text-sm font-medium text-mnema-text">
          {{ $t('events.title') }}
        </label>
        <input
          v-model="title"
          type="text"
          maxlength="100"
          required
          :placeholder="$t('events.titlePlaceholder')"
          class="w-full rounded-lg border border-mnema-border bg-mnema-surface px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary focus:border-mnema-accent focus:outline-hidden"
        />
      </div>

      <!-- Description -->
      <div>
        <label class="mb-1 block text-sm font-medium text-mnema-text">
          {{ $t('events.description') }}
        </label>
        <textarea
          v-model="description"
          rows="3"
          maxlength="2000"
          :placeholder="$t('events.descriptionPlaceholder')"
          class="w-full resize-none rounded-lg border border-mnema-border bg-mnema-surface px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary focus:border-mnema-accent focus:outline-hidden"
        ></textarea>
      </div>

      <!-- Time Fields -->
      <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div>
          <label class="mb-1 block text-sm font-medium text-mnema-text">
            {{ $t('events.startTime') }}
          </label>
          <input
            v-model="startTime"
            type="datetime-local"
            required
            class="w-full rounded-lg border border-mnema-border bg-mnema-surface px-3 py-2 text-sm text-mnema-text focus:border-mnema-accent focus:outline-hidden"
          />
        </div>
        <div>
          <label class="mb-1 block text-sm font-medium text-mnema-text">
            {{ $t('events.endTime') }}
          </label>
          <input
            v-model="endTime"
            type="datetime-local"
            class="w-full rounded-lg border border-mnema-border bg-mnema-surface px-3 py-2 text-sm text-mnema-text focus:border-mnema-accent focus:outline-hidden"
          />
        </div>
      </div>

      <!-- Location Type -->
      <div>
        <label class="mb-1 block text-sm font-medium text-mnema-text">
          {{ $t('events.locationType') }}
        </label>
        <div class="mb-2 flex gap-4">
          <label class="flex items-center gap-2 text-sm text-mnema-text cursor-pointer">
            <input
              v-model="locationType"
              type="radio"
              value="talk"
              class="accent-mnema-accent"
            />
            <span>{{ $t('events.locationTalk') }}</span>
          </label>
          <label class="flex items-center gap-2 text-sm text-mnema-text cursor-pointer">
            <input
              v-model="locationType"
              type="radio"
              value="text"
              class="accent-mnema-accent"
            />
            <span>{{ $t('events.locationText') }}</span>
          </label>
        </div>

        <div v-if="locationType === 'talk'">
          <select
            v-model="channelId"
            class="w-full rounded-lg border border-mnema-border bg-mnema-surface px-3 py-2 text-sm text-mnema-text focus:border-mnema-accent focus:outline-hidden"
          >
            <option value="" disabled>{{ $t('events.selectTalk') }}</option>
            <option v-for="ch in voiceChannels" :key="ch.id" :value="ch.id">
              {{ ch.name }}
            </option>
          </select>
        </div>
        <div v-else>
          <input
            v-model="locationText"
            type="text"
            maxlength="255"
            :placeholder="$t('events.locationTextPlaceholder')"
            class="w-full rounded-lg border border-mnema-border bg-mnema-surface px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary focus:border-mnema-accent focus:outline-hidden"
          />
        </div>
      </div>

      <!-- Cover Image -->
      <div>
        <label class="mb-1 block text-sm font-medium text-mnema-text">
          {{ $t('events.coverImage') }}
        </label>
        <div v-if="coverPreview" class="relative mb-2 aspect-[3/1] w-full overflow-hidden rounded-lg border border-mnema-border bg-mnema-surface">
          <img :src="coverPreview" alt="Event Cover" class="h-full w-full object-cover" />
          <button
            type="button"
            class="absolute top-2 right-2 rounded-full bg-black/60 p-1.5 text-white hover:bg-black/80 transition-colors"
            @click="removeCover"
          >
            <Trash2 class="w-4 h-4" />
          </button>
        </div>
        <div class="flex items-center gap-3">
          <label class="flex cursor-pointer items-center gap-2 rounded-lg border border-mnema-border bg-mnema-surface px-3 py-1.5 text-sm font-medium text-mnema-text hover:bg-mnema-hover transition-colors">
            <ImageIcon class="w-4 h-4 text-mnema-tertiary" />
            <span>{{ coverPreview ? $t('events.changeCover') : $t('events.uploadCover') }}</span>
            <input
              type="file"
              accept="image/*"
              class="hidden"
              @change="onCoverSelected"
            />
          </label>
          <button
            v-if="coverPreview"
            type="button"
            class="text-sm text-red-400 hover:underline"
            @click="removeCover"
          >
            {{ $t('events.removeCover') }}
          </button>
        </div>
      </div>

      <!-- Actions -->
      <div class="mt-4 flex justify-end gap-3 border-t border-mnema-hairline pt-4">
        <button
          type="button"
          class="rounded-lg border border-mnema-border px-4 py-2 text-sm font-medium text-mnema-text hover:bg-mnema-hover transition-colors"
          @click="emit('close')"
        >
          {{ $t('common.cancel') }}
        </button>
        <button
          type="submit"
          :disabled="saving"
          class="rounded-lg bg-mnema-accent px-4 py-2 text-sm font-medium text-white hover:opacity-90 disabled:opacity-50 transition-opacity"
        >
          {{ isEdit ? $t('events.save') : $t('events.create') }}
        </button>
      </div>
    </form>
  </BaseDialog>
</template>
