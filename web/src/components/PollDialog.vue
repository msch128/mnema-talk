<script setup lang="ts">
import { computed, nextTick, reactive, ref } from 'vue'
import { AlertCircle, Plus, X } from '@lucide/vue'
import BaseDialog from './BaseDialog.vue'
import { useChatStore } from '../stores/chat'
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
  list.value?.querySelector<HTMLInputElement>('input:last-of-type')?.focus()
}

function removeOption(i: number) {
  if (draft.options.length > POLL_MIN_OPTIONS) draft.options.splice(i, 1)
}

function setDuration(value: string) {
  if (isPollDuration(value)) draft.duration = value
}

async function submit() {
  if (problem.value || busy.value) return
  busy.value = true
  error.value = ''
  try {
    await chat.createPoll(props.channelId, draftRequest(draft))
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
  <BaseDialog :title="$t('poll.createTitle')" @close="emit('close')">
    <form class="space-y-4 overflow-y-auto p-5" data-testid="poll-dialog" @submit.prevent="submit">
      <div v-if="error" role="alert" class="flex items-center gap-2 rounded-lg border border-mnema-danger/30 bg-mnema-danger/10 p-2.5 text-sm text-mnema-danger">
        <AlertCircle class="h-4 w-4 shrink-0" />
        <span>{{ error }}</span>
      </div>

      <div class="stack-1.5">
        <label for="poll-question" class="font-mono text-xs font-medium uppercase tracking-wider text-mnema-tertiary">{{ $t('poll.question') }}</label>
        <input
          id="poll-question"
          v-model="draft.question"
          type="text"
          :maxlength="POLL_MAX_QUESTION"
          :placeholder="$t('poll.questionPlaceholder')"
          class="h-9 w-full rounded-md border border-mnema-border bg-mnema-canvas px-3 text-sm text-mnema-text outline-hidden focus:border-mnema-accent"
          autocomplete="off"
        />
      </div>

      <fieldset class="stack-1.5">
        <legend class="mb-1.5 font-mono text-xs font-medium uppercase tracking-wider text-mnema-tertiary">{{ $t('poll.answers') }}</legend>
        <div ref="list" class="space-y-1.5">
          <div v-for="(_, i) in draft.options" :key="i" class="flex items-center gap-1.5">
            <input
              v-model="draft.options[i]"
              type="text"
              :maxlength="POLL_MAX_LABEL"
              :aria-label="$t('poll.answerN', { n: i + 1 })"
              :placeholder="$t('poll.answerN', { n: i + 1 })"
              data-testid="poll-answer"
              class="h-9 min-w-0 flex-1 rounded-md border border-mnema-border bg-mnema-canvas px-3 text-sm text-mnema-text outline-hidden focus:border-mnema-accent"
              autocomplete="off"
            />
            <button
              type="button"
              :disabled="draft.options.length <= POLL_MIN_OPTIONS"
              :aria-label="$t('poll.removeAnswer', { n: i + 1 })"
              class="flex h-9 w-9 shrink-0 items-center justify-center rounded-md text-mnema-tertiary transition hover:bg-mnema-surface hover:text-mnema-text disabled:opacity-30"
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
          class="inline-flex items-center gap-1.5 rounded-md px-2 py-1.5 text-sm text-mnema-accent transition hover:bg-mnema-surface disabled:opacity-40"
          @click="addOption"
        >
          <Plus class="h-4 w-4" />{{ $t('poll.addAnswer') }}
        </button>
      </fieldset>

      <div class="grid gap-3 sm:grid-cols-2">
        <div class="stack-1.5">
          <label for="poll-duration" class="font-mono text-xs font-medium uppercase tracking-wider text-mnema-tertiary">{{ $t('poll.duration') }}</label>
          <select
            id="poll-duration"
            :value="draft.duration"
            class="h-9 w-full rounded-md border border-mnema-border bg-mnema-canvas px-2 text-sm text-mnema-text"
            @change="setDuration(($event.target as HTMLSelectElement).value)"
          >
            <option v-for="d in POLL_DURATIONS" :key="d" :value="d">{{ $t(`poll.durations.${d}`) }}</option>
          </select>
        </div>
        <div class="space-y-2 pt-1 sm:pt-5">
          <label class="flex items-center gap-2 text-sm text-mnema-body-ink">
            <input v-model="draft.multiple" type="checkbox" class="h-4 w-4 accent-mnema-accent" data-testid="poll-multiple" />
            {{ $t('poll.allowMultiple') }}
          </label>
          <label class="flex items-center gap-2 text-sm text-mnema-body-ink">
            <input v-model="draft.anonymous" type="checkbox" class="h-4 w-4 accent-mnema-accent" data-testid="poll-anonymous-toggle" />
            {{ $t('poll.makeAnonymous') }}
          </label>
        </div>
      </div>
      <p v-if="draft.anonymous" class="text-xs text-mnema-tertiary">{{ $t('poll.anonymousHint') }}</p>

      <div class="flex flex-wrap items-center justify-end gap-2 pt-1">
        <span v-if="problemText" class="max-sm:w-full sm:mr-auto text-xs text-mnema-tertiary" data-testid="poll-problem">{{ problemText }}</span>
        <button type="button" class="h-9 whitespace-nowrap rounded-md px-3 text-sm text-mnema-muted transition hover:bg-mnema-surface hover:text-mnema-text" @click="emit('close')">{{ $t('common.cancel') }}</button>
        <button
          type="submit"
          :disabled="!!problem || busy"
          data-testid="poll-post"
          class="h-9 whitespace-nowrap rounded-md bg-mnema-accent px-4 text-sm font-semibold text-mnema-accent-ink transition hover:bg-mnema-accent-hover disabled:opacity-40"
        >{{ $t('poll.post') }}</button>
      </div>
    </form>
  </BaseDialog>
</template>
