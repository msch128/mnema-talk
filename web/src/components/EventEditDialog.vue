<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId } from 'vue'
import { Image as ImageIcon, Trash2 } from '@lucide/vue'
import BaseDialog from './BaseDialog.vue'
import { useEventsStore } from '../stores/events'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import type { CommunityEvent } from '../types/domain'
import { caughtErrorMessage } from '../lib/api'
import { t } from '../i18n'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { draftsOf } from '../lib/eventRoleDrafts'
import type { RoleDraft, RoleRemoval } from '../lib/eventRoleDrafts'
import { useEventRoles } from '../composables/useEventRoles'
import EventRolesEditor from './EventRolesEditor.vue'

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

// An event this dialog created itself: when the cover upload failed after the
// create, the next submit updates it instead of creating a duplicate.
const created = ref<CommunityEvent | null>(null)
const existing = computed(() => props.event ?? created.value)
const isEdit = computed(() => !!existing.value)
const ids = {
  title: useId(), description: useId(), start: useId(), end: useId()
}
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

const features = useServerFeaturesStore()
const roleRows = ref<RoleDraft[]>(draftsOf(props.event?.roles ?? []))
const roleRemovals = ref<RoleRemoval[]>([])
const { save: saveRoles } = useEventRoles()
// Only when the editor was used: an untouched event is not sent a role list.
const rolesTouched = computed(() => roleRows.value.length > 0 || roleRemovals.value.length > 0 || (existing.value?.roles?.length ?? 0) > 0)

const saving = ref(false)
const error = ref('')

const voiceChannels = computed(() => chatStore.allChannels.filter(c => c.type === 'voice'))

// The preview of a picked file is an object URL that must be released again.
let previewUrl: string | null = null
function releasePreview() {
  if (previewUrl) URL.revokeObjectURL(previewUrl)
  previewUrl = null
}
onBeforeUnmount(releasePreview)

function onCoverSelected(e: Event) {
  const target = e.target as HTMLInputElement
  if (target.files && target.files[0]) {
    releasePreview()
    coverFile.value = target.files[0]
    previewUrl = URL.createObjectURL(target.files[0])
    coverPreview.value = previewUrl
    removeExistingCover.value = false
  }
}

function removeCover() {
  releasePreview()
  coverFile.value = null
  coverPreview.value = null
  if (props.event?.cover_url) {
    removeExistingCover.value = true
  }
}

function validateForm(trimmedTitle: string): string | null {
  if (!trimmedTitle) return t('events.errTitle')
  if (!startTime.value) return t('events.errStart')
  const startIso = new Date(startTime.value).toISOString()
  const endIso = endTime.value ? new Date(endTime.value).toISOString() : null
  if (endIso && new Date(endIso) < new Date(startIso)) return t('events.errEnd')
  if (locationType.value === 'talk' && !channelId.value) return t('events.selectTalk')
  if (locationType.value === 'text' && !locationText.value.trim()) return t('events.errLocation')
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
    const current = existing.value
    let result: CommunityEvent
    if (current) {
      result = await eventsStore.update(current.id, payload)
    } else {
      result = await eventsStore.create(payload)
      created.value = result
    }
    try {
      result = await applyCover(result.id, result)
    } catch (err) {
      // The event itself is saved; only the cover is missing.
      error.value = created.value ? caughtErrorMessage(err, t('events.coverFailed')) : caughtErrorMessage(err, t('events.saveFailed'))
      return
    }
    if (features.eventRoles && rolesTouched.value) {
      try {
        result = await saveRoles(result.id, roleRows.value, roleRemovals.value, state => { roleRows.value = state.rows; roleRemovals.value = state.removals })
        roleRemovals.value = []
      } catch (err) {
        // The event is saved; the roles are not. The next submit retries them.
        error.value = caughtErrorMessage(err, t('events.roles.saveFailed'))
        return
      }
    }
    toast.success(current ? t('events.updated') : t('events.created'))
    emit('saved', result)
    emit('close')
  } catch (err) {
    error.value = caughtErrorMessage(err, t('events.saveFailed'))
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
        <label :for="ids.title" class="mb-1 block text-sm font-medium text-mnema-text">
          {{ $t('events.title') }}
        </label>
        <input
          :id="ids.title"
          v-model="title"
          type="text"
          v-max-chars="100"
          required
          :placeholder="$t('events.titlePlaceholder')"
          class="w-full rounded-lg border border-mnema-border bg-mnema-surface px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary focus:outline-hidden"
        />
      </div>

      <!-- Description -->
      <div>
        <label :for="ids.description" class="mb-1 block text-sm font-medium text-mnema-text">
          {{ $t('events.description') }}
        </label>
        <textarea
          :id="ids.description"
          v-model="description"
          rows="3"
          v-max-chars="2000"
          :placeholder="$t('events.descriptionPlaceholder')"
          class="w-full resize-none rounded-lg border border-mnema-border bg-mnema-surface px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary focus:outline-hidden"
        ></textarea>
      </div>

      <!-- Time Fields -->
      <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div>
          <label :for="ids.start" class="mb-1 block text-sm font-medium text-mnema-text">
            {{ $t('events.startTime') }}
          </label>
          <input
            :id="ids.start"
            v-model="startTime"
            type="datetime-local"
            required
            class="w-full rounded-lg border border-mnema-border bg-mnema-surface px-3 py-2 text-sm text-mnema-text focus:outline-hidden"
          />
        </div>
        <div>
          <label :for="ids.end" class="mb-1 block text-sm font-medium text-mnema-text">
            {{ $t('events.endTime') }}
          </label>
          <input
            :id="ids.end"
            v-model="endTime"
            type="datetime-local"
            class="w-full rounded-lg border border-mnema-border bg-mnema-surface px-3 py-2 text-sm text-mnema-text focus:outline-hidden"
          />
        </div>
      </div>

      <!-- Location Type -->
      <div>
        <span class="mb-1 block text-sm font-medium text-mnema-text">
          {{ $t('events.locationType') }}
        </span>
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
            :aria-label="$t('events.locationTalk')"
            class="w-full rounded-lg border border-mnema-border bg-mnema-surface px-3 py-2 text-sm text-mnema-text focus:outline-hidden"
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
            :aria-label="$t('events.locationText')"
            type="text"
            v-max-chars="255"
            :placeholder="$t('events.locationTextPlaceholder')"
            class="w-full rounded-lg border border-mnema-border bg-mnema-surface px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary focus:outline-hidden"
          />
        </div>
      </div>

      <!-- Cover Image -->
      <div>
        <span class="mb-1 block text-sm font-medium text-mnema-text">
          {{ $t('events.coverImage') }}
        </span>
        <div v-if="coverPreview" class="relative mb-2 aspect-[3/1] w-full shrink-0 overflow-hidden rounded-lg border border-mnema-border bg-mnema-surface">
          <img :src="coverPreview" :alt="t('events.coverAlt')" class="h-full w-full object-cover" />
          <button
            type="button"
            :aria-label="$t('events.removeCover')"
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

      <EventRolesEditor v-if="features.eventRoles" v-model="roleRows" v-model:removals="roleRemovals" />

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
