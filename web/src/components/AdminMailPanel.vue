<script setup lang="ts">
// Owner only (System tab): whether the server can send e-mail (SMTP_* in the
// environment; nothing is stored in the database) and a "send test mail" action.
import type { PropType } from 'vue'
import { ref } from 'vue'
import { Loader2, Send } from '@lucide/vue'
import { api, caughtErrorMessage, isApiError } from '../lib/api'
import type { ServerMailStatus } from '../types/rest'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'

defineProps({ mail: { type: Object as PropType<ServerMailStatus>, required: true } })

const toasts = useToastStore()
const recipient = ref('')
const busy = ref(false)

// The server words its refusals in English; name the two common ones here.
function testFailure(err: unknown, noRecipient: boolean): string {
  if (isApiError(err) && err.code === 'RATE_LIMITED') return t('admin.mail.tooOften')
  if (isApiError(err) && err.code === 'INVALID_INPUT' && noRecipient) return t('admin.mail.needRecipient')
  return caughtErrorMessage(err, t('admin.unknownError'))
}

async function sendTest() {
  busy.value = true
  try {
    const to = recipient.value.trim()
    await api('/api/admin/system/mail/test', { method: 'POST', json: to ? { to } : {} })
    toasts.success(t('admin.mail.sent'))
  } catch (err) {
    toasts.error(testFailure(err, recipient.value.trim() === ''))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="space-y-2 rounded-lg border border-mnema-hairline bg-mnema-surface p-4" data-testid="admin-mail">
    <div class="text-sm font-semibold text-mnema-text">{{ $t('admin.mail.title') }}</div>
    <p v-if="!mail.configured" class="text-xs text-mnema-tertiary" data-testid="admin-mail-off">{{ $t('admin.mail.off') }}</p>
    <template v-else>
      <p class="text-xs text-mnema-tertiary" data-testid="admin-mail-on">{{ $t('admin.mail.on', { from: mail.from, tls: mail.tls }) }}</p>
      <form class="flex flex-wrap items-center gap-2" @submit.prevent="sendTest">
        <input v-model="recipient" type="email" maxlength="254" autocomplete="off" :aria-label="$t('admin.mail.recipient')" :placeholder="$t('admin.mail.recipientPlaceholder')" class="min-w-0 flex-1 rounded-md border border-mnema-border-field bg-mnema-surface px-2 py-1 text-sm text-mnema-text outline-hidden focus:border-mnema-accent" data-testid="admin-mail-recipient" />
        <button type="submit" :disabled="busy" class="flex items-center gap-1.5 rounded-md bg-mnema-accent px-3 py-1 text-sm font-semibold text-mnema-accent-ink shadow-xs transition hover:bg-mnema-accent-hover disabled:opacity-40" data-testid="admin-mail-test">
          <Loader2 v-if="busy" class="h-3.5 w-3.5 animate-spin" /><Send v-else class="h-3.5 w-3.5" />{{ $t('admin.mail.test') }}
        </button>
      </form>
    </template>
  </div>
</template>
