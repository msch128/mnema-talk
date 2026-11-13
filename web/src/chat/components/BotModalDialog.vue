<script setup lang="ts">
// A form a bot opened for this member (interaction_modal): its title, the bot,
// and its text inputs. Send posts the values once; closing sends nothing (and
// asks first when something was typed). A form the server no longer knows (15
// minutes passed, or the server restarted) says so and closes.
import type { PropType } from 'vue'
import { computed, nextTick, reactive, ref } from 'vue'
import { AlertCircle } from '@lucide/vue'
import BaseDialog from '../../shared/components/BaseDialog.vue'
import UserAvatar from '../../shared/components/UserAvatar.vue'
import { api, caughtErrorMessage, isApiError } from '../../shared/lib/api'
import { useDiscardGuard } from '../../shared/composables/useDiscardGuard'
import { useToastStore } from '../../stores/toast'
import type { BotModal } from '../../stores/chat'
import { t } from '../../i18n'
import { inputProblem, lengthOf, modalPath, textInputs } from '../lib/modals'

const props = defineProps({
  modal: { type: Object as PropType<BotModal>, required: true }
})
const emit = defineEmits<{ close: [] }>()
const toasts = useToastStore()

const inputs = computed(() => textInputs(props.modal.components))
const values = reactive<Record<string, string>>(Object.fromEntries(inputs.value.map(i => [i.customId, i.value])))
const tried = ref(false)
const busy = ref(false)
const error = ref('')
const form = ref<HTMLFormElement | null>(null)

const changed = () => inputs.value.some(i => (values[i.customId] ?? '') !== i.value)
const guard = useDiscardGuard(() => !busy.value && changed())
const problems = computed(() => Object.fromEntries(inputs.value.map(i => [i.customId, inputProblem(i, values[i.customId] ?? '')])))
const fieldId = (customId: string) => `bot-modal-${props.modal.id}-${customId}`

function problemText(customId: string): string {
  const input = inputs.value.find(i => i.customId === customId)
  const problem = problems.value[customId]
  if (!input || !problem) return ''
  return problem === 'required' ? t('modals.required') : t('modals.tooShort', { count: input.min })
}

async function focusFirstProblem() {
  await nextTick()
  form.value?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus()
}

async function submit() {
  if (busy.value) return
  tried.value = true
  if (Object.values(problems.value).some(Boolean)) {
    await focusFirstProblem()
    return
  }
  busy.value = true
  error.value = ''
  try {
    await api(modalPath(props.modal.channel_id, props.modal.id), { method: 'POST', json: { values: { ...values } } })
    emit('close')
  } catch (e) {
    if (isApiError(e) && e.status === 404) {
      toasts.info(t('modals.expired'))
      emit('close')
      return
    }
    error.value = caughtErrorMessage(e, t('modals.failed'))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <BaseDialog :title="modal.title" wrap-title panel-class="max-w-lg" :close-on-scrim="false" :guard="guard" @close="emit('close')">
    <template #default="{ close }">
      <form ref="form" class="space-y-5 overflow-y-auto p-6 max-md:p-5" data-testid="bot-modal" novalidate @submit.prevent="submit">
        <p class="flex min-w-0 items-center gap-2 text-xs text-mnema-tertiary">
          <UserAvatar :user="{ display_name: modal.bot_name, username: modal.bot_name, avatar_url: modal.bot_avatar_url }" size="xs" />
          <span class="min-w-0 truncate" :title="modal.bot_name">{{ $t('modals.from', { bot: modal.bot_name }) }}</span>
        </p>

        <div v-if="error" role="alert" class="flex items-start gap-2 rounded-lg border border-mnema-danger/30 bg-mnema-danger/10 p-2.5 text-sm text-mnema-danger" data-testid="bot-modal-error">
          <AlertCircle class="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" />
          <span class="min-w-0 wrap-break-word">{{ error }}</span>
        </div>

        <div v-for="input in inputs" :key="input.customId" class="stack-2">
          <div class="flex min-w-0 items-baseline justify-between gap-3">
            <label :for="fieldId(input.customId)" class="min-w-0 wrap-break-word text-sm font-medium text-mnema-text">
              {{ input.label }}<span v-if="!input.required" class="ml-1 text-xs font-normal text-mnema-tertiary">{{ $t('modals.optional') }}</span>
            </label>
            <span v-if="input.max < 4000" class="shrink-0 text-xs tabular-nums text-mnema-tertiary" data-testid="bot-modal-count">{{ lengthOf(values[input.customId] ?? '') }}/{{ input.max }}</span>
          </div>
          <component
            :is="input.paragraph ? 'textarea' : 'input'"
            :id="fieldId(input.customId)"
            :value="values[input.customId]"
            v-max-chars="input.max"
            :type="input.paragraph ? undefined : 'text'"
            :rows="input.paragraph ? 4 : undefined"
            :placeholder="input.placeholder || undefined"
            :aria-required="input.required ? 'true' : undefined"
            :aria-invalid="tried && problems[input.customId] ? 'true' : undefined"
            :aria-describedby="tried && problems[input.customId] ? `${fieldId(input.customId)}-problem` : undefined"
            :data-input="input.customId"
            @input="values[input.customId] = ($event.target as HTMLInputElement).value"
            autocomplete="off"
            class="w-full rounded-lg border border-mnema-border bg-mnema-canvas px-3 py-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent"
          />
          <p v-if="tried && problems[input.customId]" :id="`${fieldId(input.customId)}-problem`" class="text-xs text-mnema-danger">{{ problemText(input.customId) }}</p>
        </div>

        <div class="flex flex-wrap items-center justify-end gap-2 pt-1">
          <button type="button" class="btn btn-ghost" :disabled="busy" data-testid="bot-modal-cancel" @click="close">{{ $t('common.cancel') }}</button>
          <button type="submit" class="btn btn-primary" :disabled="busy" :aria-busy="busy ? 'true' : undefined" data-testid="bot-modal-submit">{{ $t('modals.send') }}</button>
        </div>
      </form>
    </template>
  </BaseDialog>
</template>
