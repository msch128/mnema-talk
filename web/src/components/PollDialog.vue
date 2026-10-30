<script setup lang="ts">
import { computed, nextTick, reactive, ref } from 'vue'
import { AlertCircle, Plus, X } from '@lucide/vue'
import BaseDialog from './BaseDialog.vue'
import { useChatStore } from '../stores/chat'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { caughtErrorMessage } from '../lib/api'
import {
  POLL_DURATIONS, POLL_MAX_LABEL, POLL_MAX_OPTIONS, POLL_MAX_QUESTION, POLL_MIN_OPTIONS,
  draftProblem, draftRequest, emptyDraft, isPollDuration
} from '../lib/polls'
import { t } from '../i18n'

// Asks a question with 2 to 10 answers (the composer's poll button, and /poll
// from the command menu). The server checks everything again.
const props = defineProps({
  channelId: { type: String, required: true }
})
const emit = defineEmits<{ close: []; posted: [] }>()

const chat = useChatStore()
const features = useServerFeaturesStore()
const draft = reactive(emptyDraft())
const busy = ref(false)
const error = ref('')
const list = ref<HTMLElement | null>(null)

const problem = computed(() => draftProblem(draft))
const canAdd = computed(() => draft.options.length < POLL_MAX_OPTIONS)
const problemText = computed(() => (problem.value ? t(`poll.problem.${problem.value}`, { min: POLL_MIN_OPTIONS }) : ''))

async function addOption() {
  if (!canAdd.value) return
  draft.options.push('')
  await nextTick()
  const inputs = list.value?.querySelectorAll<HTMLInputElement>('input')
  inputs?.[inputs.length - 1]?.focus()
}

function removeOption(i: number) {
  if (draft.options.length > POLL_MIN_OPTIONS) draft.options.splice(i, 1)
}

// A server without the feature would still make the poll anonymous in the old, weaker way: never ask for it.
function request() {
  const body = draftRequest(draft)
  return features.anonymousPolls ? body : { ...body, anonymous: false }
}

function setDuration(value: string) {
  if (isPollDuration(value)) draft.duration = value
}

async function submit() {
  if (problem.value || busy.value) return
  busy.value = true
  error.value = ''
  try {
    await chat.createPoll(props.channelId, request())
    emit('posted')
    emit('close')
  } catch (err) {
    error.value = caughtErrorMessage(err, t('poll.postFailed'))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <BaseDialog :title="$t('poll.createTitle')" panel-class="max-w-lg" @close="emit('close')">
    <form class="space-y-6 overflow-y-auto p-6 max-md:p-5" data-testid="poll-dialog" @submit.prevent="submit">
      <div v-if="error" role="alert" class="flex items-center gap-2 rounded-lg border border-mnema-danger/30 bg-mnema-danger/10 p-2.5 text-sm text-mnema-danger">
        <AlertCircle class="h-4 w-4 shrink-0" />
        <span>{{ error }}</span>
      </div>

      <div class="stack-2">
        <label for="poll-question" class="font-mono text-xs font-medium uppercase tracking-wider text-mnema-tertiary">{{ $t('poll.question') }}</label>
        <input
          id="poll-question"
          v-model="draft.question"
          type="text"
          v-max-chars="POLL_MAX_QUESTION"
          :placeholder="$t('poll.questionPlaceholder')"
          class="h-11 w-full rounded-lg border border-mnema-border bg-mnema-canvas px-4 text-base text-mnema-text"
          autocomplete="off"
        />
      </div>

      <fieldset class="stack-2">
        <legend class="mb-2 font-mono text-xs font-medium uppercase tracking-wider text-mnema-tertiary">{{ $t('poll.answers') }}</legend>
        <div ref="list" class="space-y-2.5">
          <div v-for="(_, i) in draft.options" :key="i" class="flex items-center gap-2">
            <span class="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-mnema-surface font-mono text-xs text-mnema-tertiary" aria-hidden="true">{{ i + 1 }}</span>
            <input
              v-model="draft.options[i]"
              type="text"
              v-max-chars="POLL_MAX_LABEL"
              :aria-label="$t('poll.answerN', { n: i + 1 })"
              :placeholder="$t('poll.answerN', { n: i + 1 })"
              data-testid="poll-answer"
              class="h-11 min-w-0 flex-1 rounded-lg border border-mnema-border bg-mnema-canvas px-4 text-sm text-mnema-text"
              autocomplete="off"
            />
            <button
              type="button"
              :disabled="draft.options.length <= POLL_MIN_OPTIONS"
              :aria-label="$t('poll.removeAnswer', { n: i + 1 })"
              class="btn btn-icon h-9 w-9 shrink-0 rounded-full opacity-70 hover:opacity-100 disabled:opacity-20"
              @click="removeOption(i)"
            >
              <X class="h-4 w-4" />
            </button>
          </div>
        </div>
        <button
          type="button"
          :disabled="!canAdd"
          data-testid="poll-add"
          class="btn btn-ghost mt-3 w-full justify-start border border-dashed border-mnema-border text-mnema-accent hover:border-mnema-accent/50 hover:bg-mnema-accent/5 hover:text-mnema-accent"
          @click="addOption"
        >
          <Plus class="h-4 w-4" />{{ $t('poll.addAnswer') }}
        </button>
      </fieldset>

      <div class="grid gap-5 sm:grid-cols-2">
        <div class="stack-2">
          <label for="poll-duration" class="font-mono text-xs font-medium uppercase tracking-wider text-mnema-tertiary">{{ $t('poll.duration') }}</label>
          <select
            id="poll-duration"
            :value="draft.duration"
            class="h-11 w-full rounded-lg border border-mnema-border bg-mnema-canvas px-3 text-sm text-mnema-text"
            @change="setDuration(($event.target as HTMLSelectElement).value)"
          >
            <option v-for="d in POLL_DURATIONS" :key="d" :value="d">{{ $t(`poll.durations.${d}`) }}</option>
          </select>
        </div>
        <div class="space-y-3 pt-1 sm:pt-6">
          <label class="flex cursor-pointer items-center gap-3 text-sm text-mnema-body-ink">
            <input v-model="draft.multiple" type="checkbox" data-testid="poll-multiple" />
            {{ $t('poll.allowMultiple') }}
          </label>
          <label v-if="features.anonymousPolls" class="flex cursor-pointer items-center gap-3 text-sm text-mnema-body-ink">
            <input v-model="draft.anonymous" type="checkbox" data-testid="poll-anonymous-toggle" />
            {{ $t('poll.makeAnonymous') }}
          </label>
        </div>
      </div>
      <p v-if="features.anonymousPolls && draft.anonymous" class="settings-hint" data-testid="poll-anonymous-hint">{{ $t('poll.anonymousHint') }}</p>

      <div class="flex flex-wrap items-center justify-end gap-3 border-t border-mnema-hairline pt-5">
        <span v-if="problemText" class="max-sm:w-full sm:mr-auto text-xs text-mnema-tertiary" data-testid="poll-problem">{{ problemText }}</span>
        <button type="button" class="btn btn-ghost whitespace-nowrap" @click="emit('close')">{{ $t('common.cancel') }}</button>
        <button
          type="submit"
          :disabled="!!problem || busy"
          data-testid="poll-post"
          class="btn btn-primary whitespace-nowrap"
        >{{ $t('poll.post') }}</button>
      </div>
    </form>
  </BaseDialog>
</template>
