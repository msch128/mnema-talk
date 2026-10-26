<script setup lang="ts">
// "Data & account" (GDPR): download a copy of everything this server holds
// about the signed-in member, or delete the account for good. Both need the
// password and, when two-factor sign-in is on, a current code.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { Download, Loader2, Trash2 } from '@lucide/vue'
import { caughtErrorMessage } from '../lib/api'
import { deleteAccount as eraseAccount, exportStatus, requestExport as askForExport } from '../lib/accountApi'
import type { AuthExportStatus } from '../types/rest'
import { locale, t } from '../i18n'
import { useAccountStore } from '../stores/account'
import { useAuthStore } from '../stores/auth'
import FormField from './FormField.vue'
import PasswordInput from './PasswordInput.vue'
import { useToastStore } from '../stores/toast'

const account = useAccountStore()
const authStore = useAuthStore()
const toasts = useToastStore()

const POLL_MS = 2000
const status = ref<AuthExportStatus | null>(null)
const asking = ref<'export' | 'delete' | null>(null)
const password = ref('')
const code = ref('')
const typedName = ref('')
const busy = ref(false)
const error = ref('')
let timer: ReturnType<typeof setTimeout> | undefined

const needsCode = computed(() => !!account.info?.two_factor_enabled)
const isOwner = computed(() => authStore.isAdmin)
const username = computed(() => authStore.user?.username ?? '')
const canRequest = computed(() => {
  const next = status.value?.next_request_at
  return !next || new Date(next).getTime() <= Date.now()
})
const when = (iso: string | undefined) => (iso ? new Date(iso).toLocaleString(locale.value, { dateStyle: 'medium', timeStyle: 'short' }) : '')
const deleteReady = computed(() => typedName.value.trim().toLowerCase() === username.value.toLowerCase() && username.value !== '')

async function refresh() {
  try {
    status.value = await exportStatus()
  } catch (err) {
    void err
  }
  clearTimeout(timer)
  if (status.value?.state === 'pending') timer = setTimeout(refresh, POLL_MS)
}

function ask(what: 'export' | 'delete') {
  password.value = code.value = typedName.value = error.value = ''
  asking.value = what
}

async function run(action: () => Promise<void>) {
  error.value = ''
  busy.value = true
  try {
    await action()
  } catch (err) {
    error.value = caughtErrorMessage(err, t('errors.unknown'))
  } finally {
    busy.value = false
  }
}

const requestExport = () => run(async () => {
  status.value = await askForExport(password.value, code.value)
  asking.value = null
  toasts.success(t('security.privacy.exportStarted'))
  clearTimeout(timer)
  timer = setTimeout(refresh, POLL_MS)
})

const deleteAccount = () => run(async () => {
  await eraseAccount(password.value, code.value)
  // The server ended the session; leave nothing of this account in the page.
  authStore.resetLocalSession()
  account.close()
  window.location.assign('/')
})

// The file is taken by the first request; look again shortly after.
function afterDownload() {
  clearTimeout(timer)
  timer = setTimeout(refresh, 1500)
}

onMounted(refresh)
onBeforeUnmount(() => clearTimeout(timer))
</script>

<template>
  <div class="space-y-5" data-testid="account-data">
    <section class="settings-card" aria-labelledby="security-data-title">
      <div>
        <h3 id="security-data-title" class="settings-title">{{ $t('security.privacy.exportTitle') }}</h3>
        <p class="settings-help">{{ $t('security.privacy.exportIntro') }}</p>
      </div>
      <p v-if="status?.state === 'pending'" role="status" class="flex items-center gap-2 text-sm text-mnema-text" data-testid="export-pending"><Loader2 class="h-4 w-4 animate-spin" />{{ $t('security.privacy.exportPending') }}</p>
      <a v-else-if="status?.state === 'ready'" href="/api/users/me/export/download" download class="btn btn-primary self-start" data-testid="export-download" @click="afterDownload"><Download class="h-3.5 w-3.5" />{{ $t('security.privacy.exportReady', { when: when(status.expires_at) }) }}</a>
      <p v-else-if="status?.state === 'failed'" role="alert" class="text-sm text-mnema-danger" data-testid="export-failed">{{ $t('security.privacy.exportFailed') }}</p>
      <p v-else-if="status && status.state !== 'none'" class="text-sm text-mnema-tertiary" data-testid="export-done">{{ $t(status.state === 'downloaded' ? 'security.privacy.exportDownloaded' : 'security.privacy.exportExpired') }}</p>

      <form v-if="asking === 'export'" class="space-y-3" @submit.prevent="requestExport">
        <FormField v-slot="{ id }" :label="$t('profile.currentPassword')">
          <PasswordInput :id="id" v-model="password" required autocomplete="current-password" data-testid="privacy-password" />
        </FormField>
        <FormField v-if="needsCode" v-slot="{ id }" :label="$t('security.twoFactor.code')">
          <input :id="id" v-model="code" type="text" required autocomplete="one-time-code" autocapitalize="none" spellcheck="false" maxlength="32" class="w-full rounded-lg border border-mnema-border-field bg-mnema-canvas px-3 py-2 font-mono text-sm tracking-widest text-mnema-text" data-testid="privacy-code" />
        </FormField>
        <div v-if="error" role="alert" class="rounded-sm border border-mnema-danger/30 bg-mnema-danger/10 p-1.5 text-sm text-mnema-danger">{{ error }}</div>
        <div class="flex justify-end gap-2 pt-1">
          <button type="button" class="btn btn-ghost" @click="asking = null">{{ $t('common.cancel') }}</button>
          <button type="submit" :disabled="busy" class="btn btn-primary" data-testid="export-submit">{{ $t('security.privacy.exportRequest') }}</button>
        </div>
      </form>
      <template v-else-if="status?.state !== 'pending' && status?.state !== 'ready'">
        <button v-if="canRequest" type="button" class="btn btn-secondary self-start" data-testid="export-start" @click="ask('export')">{{ $t('security.privacy.exportRequest') }}</button>
        <p v-else class="text-xs text-mnema-tertiary" data-testid="export-wait">{{ $t('security.privacy.exportNext', { when: when(status?.next_request_at) }) }}</p>
      </template>
    </section>

    <!-- The Owner cannot delete the account: the card only explains why, so it is not styled as a live danger. -->
    <section :class="['settings-card', !isOwner && 'border-mnema-danger/30']" data-testid="delete-account" aria-labelledby="security-delete-title">
      <div>
        <h3 id="security-delete-title" :class="['settings-title', !isOwner && 'text-mnema-danger']">{{ $t('security.privacy.deleteTitle') }}</h3>
        <p v-if="isOwner" class="settings-help" data-testid="delete-owner">{{ $t('security.privacy.deleteOwner') }}</p>
        <p v-else class="settings-help">{{ $t('security.privacy.deleteIntro') }}</p>
      </div>
      <template v-if="!isOwner">
        <form v-if="asking === 'delete'" class="space-y-3" @submit.prevent="deleteAccount">
          <FormField v-slot="{ id }" :label="$t('security.privacy.deleteType', { name: username })">
            <input :id="id" v-model="typedName" type="text" required autocomplete="off" spellcheck="false" class="w-full rounded-lg border border-mnema-border-field bg-mnema-canvas px-3 py-2 text-sm text-mnema-text" data-testid="delete-name" />
          </FormField>
          <FormField v-slot="{ id }" :label="$t('profile.currentPassword')">
            <PasswordInput :id="id" v-model="password" required autocomplete="current-password" data-testid="privacy-password" />
          </FormField>
          <FormField v-if="needsCode" v-slot="{ id }" :label="$t('security.twoFactor.code')">
            <input :id="id" v-model="code" type="text" required autocomplete="one-time-code" autocapitalize="none" spellcheck="false" maxlength="32" class="w-full rounded-lg border border-mnema-border-field bg-mnema-canvas px-3 py-2 font-mono text-sm tracking-widest text-mnema-text" data-testid="privacy-code" />
          </FormField>
          <div v-if="error" role="alert" class="rounded-sm border border-mnema-danger/30 bg-mnema-danger/10 p-1.5 text-sm text-mnema-danger">{{ error }}</div>
          <div class="flex justify-end gap-2 pt-1">
            <button type="button" class="btn btn-ghost" @click="asking = null">{{ $t('common.cancel') }}</button>
            <button type="submit" :disabled="busy || !deleteReady" class="btn btn-danger-solid" data-testid="delete-submit"><Loader2 v-if="busy" class="h-3.5 w-3.5 animate-spin" /><Trash2 v-else class="h-3.5 w-3.5" />{{ $t('security.privacy.deleteConfirm') }}</button>
          </div>
        </form>
        <button v-else type="button" class="btn btn-danger btn-sm self-start" data-testid="delete-start" @click="ask('delete')">{{ $t('security.privacy.deleteButton') }}</button>
      </template>
    </section>
  </div>
</template>
