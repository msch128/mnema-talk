<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue'
import BaseDialog from './BaseDialog.vue'
import { useEventsStore } from '../stores/events'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import type { CommunityEvent } from '../types/domain'
import { caughtErrorMessage } from '../lib/api'
import { t } from '../i18n'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useEventRoles } from '../composables/useEventRoles'
import { useEventCover } from '../composables/useEventCover'
import { useRosterDraft } from '../composables/useRosterDraft'
import { normalise, useSettingsDraft } from '../composables/useSettingsDraft'
import type { SaveContext } from '../composables/useSettingsDraft'
import { changedFields, EVENT_FIELD_KEYS, fieldsOf, payloadOf, validateFields } from '../lib/eventPayload'
import type { EventFields } from '../lib/eventPayload'
import EventCoverField from './EventCoverField.vue'
import EventRolesEditor from './EventRolesEditor.vue'
import EventRepeatFields from './EventRepeatFields.vue'
import ScopeChoice from './ScopeChoice.vue'
import UnsavedChangesBar from './UnsavedChangesBar.vue'
import type { Scope } from '../lib/eventRecurrence'

// Create and edit an event. Editing is one draft with one Save (doc/settings-ux.md):
// the fields, the cover and the roles are all part of what is unsaved, and each is
// written by its own call, so a failure after the first leaves only the rest unsaved.
// Creating asks before it drops typed input, and its button stays "Create".
const props = defineProps<{
  event?: CommunityEvent | null
}>()

const emit = defineEmits<{
  close: []
  saved: [event: CommunityEvent]
}>()

type EventDraft = EventFields & { cover: string; roles: string }

const eventsStore = useEventsStore()
const chatStore = useChatStore()
const toast = useToastStore()
const features = useServerFeaturesStore()
const { save: saveRoles } = useEventRoles()

// An event this dialog created itself: when the cover upload failed after the
// create, the next submit updates it instead of creating a duplicate.
const created = ref<CommunityEvent | null>(null)
const existing = computed(() => props.event ?? created.value)
const isEdit = computed(() => !!existing.value)
const ids = { title: useId(), description: useId(), start: useId(), end: useId() }

const cover = useEventCover(() => existing.value?.cover_url)
const roster = useRosterDraft(() => existing.value?.roles)

// A new event may repeat; an occurrence of a series may be changed for itself or for it and the later ones.
const scope = ref<Scope>('this')
const inSeries = computed(() => !!existing.value?.series)
const createError = ref('')
let wasEdit = false
let result: CommunityEvent | null = null

/** Roles chosen while creating a series are its template; an existing occurrence follows the scope. */
function rolesScope(): Scope | undefined {
  if (created.value?.series) return 'following'
  return inSeries.value ? scope.value : undefined
}

async function writeEvent(fields: EventFields): Promise<CommunityEvent> {
  const payload = payloadOf(fields, !isEdit.value && features.eventRecurrence)
  const current = existing.value
  if (!current) {
    created.value = await eventsStore.create(payload)
    return created.value
  }
  const changes = changedFields(payload, current)
  return inSeries.value ? eventsStore.update(current.id, changes, scope.value) : eventsStore.update(current.id, changes)
}

async function writeCover(current: CommunityEvent): Promise<CommunityEvent> {
  try {
    if (cover.removeStored.value) return await eventsStore.deleteCover(current.id)
    if (cover.file.value) return await eventsStore.uploadCover(current.id, cover.file.value)
    return current
  } catch (err) {
    // The event itself is saved; only the cover is missing.
    throw new Error(caughtErrorMessage(err, t(created.value ? 'events.coverFailed' : 'events.saveFailed')))
  }
}

async function writeRoles(current: CommunityEvent): Promise<CommunityEvent> {
  try {
    const out = await saveRoles(current.id, roster.rows.value, roster.removals.value, roster.sync, rolesScope())
    roster.removals.value = []
    return out
  } catch (err) {
    // The event is saved; the roles are not. The next save retries them.
    throw new Error(caughtErrorMessage(err, t('events.roles.saveFailed')))
  }
}

/** The event, then its cover, then its roles; each part that went through counts as saved. */
async function write({ changed, commit, draft }: SaveContext<EventDraft>) {
  const problem = validateFields(draft)
  if (problem) throw new Error(problem)
  wasEdit = isEdit.value
  result = existing.value
  const eventKeys = EVENT_FIELD_KEYS.filter(k => changed.includes(k))
  if (!result || eventKeys.length > 0) {
    result = await writeEvent(draft)
    commit(wasEdit ? eventKeys : EVENT_FIELD_KEYS)
  }
  if (changed.includes('cover')) {
    result = await writeCover(result)
    commit(['cover'])
  }
  if (features.eventRoles && changed.includes('roles')) {
    result = await writeRoles(result)
    commit(['roles'], { roles: roster.key.value })
  }
}

const form = useSettingsDraft<EventDraft>({
  source: () => ({ ...fieldsOf(existing.value), cover: '', roles: roster.savedKey.value }),
  normalisers: {
    title: normalise.text,
    locationText: normalise.text,
    // Only a new event repeats; once it exists the choice is spent.
    repeat: v => (isEdit.value ? '' : v),
    repeatUntil: v => (isEdit.value ? '' : v)
  },
  save: write,
  saved: () => {
    toast.success(wasEdit ? t('events.updated') : t('events.created'))
    if (result) emit('saved', result)
    emit('close')
  }
})
const { draft } = form

// The cover and the roster live in their own state; the draft carries what they are.
watch(cover.marker, marker => { draft.cover = marker })
watch(roster.key, key => { draft.roles = key })

function reset() {
  form.reset()
  cover.reset()
  roster.reset()
}

/** Create: a blank or invalid form says what is missing instead of staying silent. */
function submit() {
  createError.value = ''
  if (!form.canSave.value && !isEdit.value) createError.value = validateFields(draft) ?? ''
  else void form.save()
}

const voiceChannels = computed(() => chatStore.allChannels.filter(c => c.type === 'voice'))
</script>

<template>
  <BaseDialog
    :title="isEdit ? $t('events.editTitle') : $t('events.createTitle')"
    panel-class="max-w-lg"
    :guard="form.guard"
    @close="emit('close')"
  >
    <template #default="{ close }">
    <form class="flex min-h-0 flex-1 flex-col" @submit.prevent="submit" @keydown="form.onKeydown">
      <div class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-6">
      <div v-if="createError || (!isEdit && form.error.value)" role="alert" class="rounded-lg bg-red-500/10 p-3 text-sm text-red-400">
        {{ createError || form.error.value }}
      </div>

      <!-- Title -->
      <div>
        <label :for="ids.title" class="mb-1 block text-sm font-medium text-mnema-text">
          {{ $t('events.title') }}
        </label>
        <input
          :id="ids.title"
          v-model="draft.title"
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
          v-model="draft.description"
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
            v-model="draft.startTime"
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
            v-model="draft.endTime"
            type="datetime-local"
            class="w-full rounded-lg border border-mnema-border bg-mnema-surface px-3 py-2 text-sm text-mnema-text focus:outline-hidden"
          />
        </div>
      </div>

      <EventRepeatFields v-if="!isEdit && features.eventRecurrence" v-model:repeat="draft.repeat" v-model:until="draft.repeatUntil" />

      <!-- Location Type -->
      <div>
        <span class="mb-1 block text-sm font-medium text-mnema-text">
          {{ $t('events.locationType') }}
        </span>
        <div class="mb-2 flex gap-4">
          <label class="flex items-center gap-2 text-sm text-mnema-text cursor-pointer">
            <input
              v-model="draft.locationType"
              type="radio"
              value="talk"
              class="accent-mnema-accent"
            />
            <span>{{ $t('events.locationTalk') }}</span>
          </label>
          <label class="flex items-center gap-2 text-sm text-mnema-text cursor-pointer">
            <input
              v-model="draft.locationType"
              type="radio"
              value="text"
              class="accent-mnema-accent"
            />
            <span>{{ $t('events.locationText') }}</span>
          </label>
        </div>

        <div v-if="draft.locationType === 'talk'">
          <select
            v-model="draft.channelId"
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
            v-model="draft.locationText"
            :aria-label="$t('events.locationText')"
            type="text"
            v-max-chars="255"
            :placeholder="$t('events.locationTextPlaceholder')"
            class="w-full rounded-lg border border-mnema-border bg-mnema-surface px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary focus:outline-hidden"
          />
        </div>
      </div>

      <EventCoverField :preview="cover.preview.value" @pick="cover.pick" @remove="cover.remove" />

      <EventRolesEditor v-if="features.eventRoles" v-model="roster.rows.value" v-model:removals="roster.removals.value" />

      <ScopeChoice v-if="isEdit && inSeries" v-model="scope" :hint="$t('events.scope.editHint')" />

      </div>

      <UnsavedChangesBar
        v-if="isEdit"
        :dirty="form.dirty.value"
        :can-save="form.canSave.value"
        :saving="form.saving.value"
        :error="form.error.value"
        :save-label="$t('events.save')"
        @save="submit"
        @reset="reset"
      />
      <div v-else class="flex shrink-0 justify-end gap-3 border-t border-mnema-hairline px-6 py-4">
        <button type="button" class="rounded-lg border border-mnema-border px-4 py-2 text-sm font-medium text-mnema-text hover:bg-mnema-hover transition-colors" data-testid="event-cancel" @click="close">
          {{ $t('common.cancel') }}
        </button>
        <button type="submit" :disabled="form.saving.value" class="rounded-lg bg-mnema-accent px-4 py-2 text-sm font-medium text-white hover:opacity-90 disabled:opacity-50 transition-opacity">
          {{ $t('events.create') }}
        </button>
      </div>
    </form>
    </template>
  </BaseDialog>
</template>
