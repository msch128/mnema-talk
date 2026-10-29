<script setup lang="ts">
// "Set a status message": the one place to write one's own status line: an
// emoji, the text (the server's limit), how long it stays and Save / Clear.
import { computed, ref } from 'vue'
import { Loader2, Smile } from '@lucide/vue'
import { caughtErrorMessage } from '../lib/api'
import { EXPIRY_CHOICES, expiryTime, isFuture } from '../lib/statusExpiry'
import type { ExpiryChoice } from '../lib/statusExpiry'
import { dateTime } from '../lib/time'
import { locale, t } from '../i18n'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useToastStore } from '../stores/toast'
import BaseDialog from './BaseDialog.vue'
import EmojiPicker from './EmojiPicker.vue'
import FormField from './FormField.vue'

const emit = defineEmits<{ close: [] }>()

// The server's limit, in characters (an emoji counts once).
const MAX_STATUS = 32
const authStore = useAuthStore()
const chatStore = useChatStore()
const toasts = useToastStore()
const features = useServerFeaturesStore()

const hadStatus = !!authStore.user?.status_text
const text = ref(authStore.user?.status_text ?? '')
const expiry = ref<ExpiryChoice>('never')
const error = ref('')
const saving = ref(false)
const pickerOpen = ref(false)
const input = ref<HTMLInputElement | null>(null)
const emojiButton = ref<HTMLButtonElement | null>(null)

const chars = (s: string) => Array.from(s)
const count = computed(() => chars(text.value).length)
const canExpire = computed(() => features.has('status.expiry') && text.value.trim() !== '')
const until = computed(() => authStore.user?.status_expires_at)

function limit() {
  if (count.value > MAX_STATUS) text.value = chars(text.value).slice(0, MAX_STATUS).join('')
}

// An emoji goes where the cursor is, as far as the limit leaves room.
function insertEmoji(emoji: string) {
  const el = input.value
  if (count.value + chars(emoji).length > MAX_STATUS) return
  const start = el?.selectionStart ?? text.value.length
  const end = el?.selectionEnd ?? start
  text.value = text.value.slice(0, start) + emoji + text.value.slice(end)
  pickerOpen.value = false
  const caret = start + emoji.length
  requestAnimationFrame(() => {
    el?.focus()
    el?.setSelectionRange(caret, caret)
  })
}

async function send(next: string, expiresAt: Date | null) {
  const id = authStore.user?.id
  if (!id) return
  saving.value = true
  try {
    const updated = await chatStore.setStatusText(id, next, expiresAt)
    chatStore.patchOpenProfile(id, { status_text: updated.status_text, ...(updated.status_expires_at ? { status_expires_at: updated.status_expires_at } : {}) }, updated.status_expires_at ? [] : ['status_expires_at'])
    toasts.success(next ? t('profile.statusSaved') : t('profile.statusCleared'))
    emit('close')
  } catch (err) {
    error.value = caughtErrorMessage(err, t('profile.saveFailed'))
  } finally {
    saving.value = false
  }
}

function save() {
  error.value = ''
  const next = text.value.trim()
  const at = next && canExpire.value ? expiryTime(expiry.value, new Date()) : null
  if (at && !isFuture(at, new Date())) {
    error.value = t('profile.expiryPast')
    return
  }
  void send(next, at)
}

function clear() {
  error.value = ''
  void send('', null)
}
</script>

<template>
  <BaseDialog
    :title="hadStatus ? $t('presence.editStatus') : $t('presence.setStatus')"
    :subtitle="$t('statusEditor.subtitle')"
    panel-class="max-w-md"
    initial-focus="[data-testid='status-input']"
    @close="emit('close')"
  >
    <form class="flex min-h-0 flex-1 flex-col" data-testid="status-dialog" @submit.prevent="save">
      <div class="min-h-0 flex-1 space-y-6 overflow-y-auto px-6 py-5">
        <FormField v-slot="{ id, describedby }" :label="$t('profile.status')" :hint="$t('statusEditor.hint')">
          <div class="flex items-center gap-2">
            <button
              ref="emojiButton"
              type="button"
              data-testid="status-emoji"
              v-tooltip="$t('emoji.open')"
              :aria-label="$t('emoji.open')"
              aria-haspopup="dialog"
              :aria-expanded="pickerOpen ? 'true' : 'false'"
              :class="['flex h-10 w-10 shrink-0 items-center justify-center rounded-lg border bg-mnema-canvas transition-colors max-md:h-11 max-md:w-11', pickerOpen ? 'border-mnema-accent text-mnema-accent' : 'border-mnema-border-field text-mnema-tertiary hover:text-mnema-text']"
              @click="pickerOpen = !pickerOpen"
            >
              <Smile class="h-5 w-5" />
            </button>
            <input
              :id="id"
              ref="input"
              v-model="text"
              data-testid="status-input"
              type="text"
              autocomplete="off"
              :aria-describedby="describedby"
              :placeholder="$t('profile.statusPlaceholder')"
              class="min-w-0 flex-1 rounded-lg border border-mnema-border-field bg-mnema-canvas px-3 py-2 text-sm text-mnema-text max-md:min-h-11"
              @input="limit"
            />
            <span data-testid="status-count" :class="['w-10 shrink-0 text-right text-xs tabular-nums', count >= MAX_STATUS ? 'text-mnema-warning' : 'text-mnema-tertiary']" aria-hidden="true">{{ count }}/{{ MAX_STATUS }}</span>
          </div>
        </FormField>

        <EmojiPicker v-if="pickerOpen" :trigger="emojiButton" class="mx-auto -mt-2" @pick="insertEmoji" @close="pickerOpen = false" />

        <div v-if="canExpire" role="radiogroup" :aria-labelledby="'status-expiry-label'" data-testid="status-expiry" class="space-y-2">
          <p id="status-expiry-label" class="text-sm font-medium text-mnema-body-ink">{{ $t('profile.clearAfter') }}</p>
          <div class="flex flex-wrap gap-2">
            <button
              v-for="c in EXPIRY_CHOICES"
              :key="c"
              type="button"
              role="radio"
              :aria-checked="expiry === c ? 'true' : 'false'"
              :data-expiry="c"
              :class="['rounded-full border px-3 py-1.5 text-sm transition-colors max-md:min-h-11', expiry === c ? 'border-mnema-accent/40 bg-mnema-accent/15 text-mnema-accent' : 'border-mnema-border text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text']"
              @click="expiry = c"
            >{{ $t(`profile.expiry.${c}`) }}</button>
          </div>
          <p v-if="until && hadStatus" class="field-hint">{{ $t('profile.clearsAt', { time: dateTime(until, locale) }) }}</p>
        </div>

        <p v-if="error" role="alert" data-testid="status-error" class="field-error">{{ error }}</p>
      </div>

      <footer class="flex items-center justify-between gap-3 border-t border-mnema-hairline bg-mnema-raised px-6 py-4">
        <button v-if="hadStatus" type="button" data-testid="status-clear" class="btn btn-ghost max-md:min-h-11" :disabled="saving" @click="clear">{{ $t('statusEditor.clear') }}</button>
        <span v-else></span>
        <button type="submit" data-testid="status-save" class="btn btn-primary max-md:min-h-11" :disabled="saving" :aria-busy="saving">
          <Loader2 v-if="saving" class="h-4 w-4 animate-spin" aria-hidden="true" />
          <span>{{ $t('common.save') }}</span>
        </button>
      </footer>
    </form>
  </BaseDialog>
</template>
