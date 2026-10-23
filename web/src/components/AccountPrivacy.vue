<script setup lang="ts">
// Your data (GDPR): download a copy of everything this server holds about the
// signed-in member, or delete the account for good. Both need the password
// and, when two-factor sign-in is on, a current code.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { Download, Loader2, Trash2 } from '@lucide/vue'
import { caughtErrorMessage } from '../lib/api'
import { deleteAccount as eraseAccount, exportStatus, requestExport as askForExport } from '../lib/accountApi'
import type { AuthExportStatus } from '../types/rest'
import { locale, t } from '../i18n'
import { useAccountStore } from '../stores/account'
import { useAuthStore } from '../stores/auth'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import AccountDmPolicy from './AccountDmPolicy.vue'
import AccountBlockedList from './AccountBlockedList.vue'
import { useToastStore } from '../stores/toast'

const account = useAccountStore()
const authStore = useAuthStore()
const features = useServerFeaturesStore()
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
  <section class="space-y-4" data-testid="account-privacy" aria-labelledby="security-privacy-title">
    <h3 id="security-privacy-title" class="text-xs font-bold uppercase tracking-wider text-mnema-tertiary">{{ $t('security.privacy.title') }}</h3>

    <!-- Who may write to this member, and whom they blocked (only on a server that has them) -->
    <AccountDmPolicy v-if="features.dm" />
    <AccountBlockedList v-if="features.blocks" />

    <div class="space-y-2">
      <p class="text-sm font-medium text-mnema-text">{{ $t('security.privacy.exportTitle') }}</p>
      <p class="text-sm text-mnema-muted">{{ $t('security.privacy.exportIntro') }}</p>
      <p v-if="status?.state === 'pending'" role="status" class="flex items-center gap-2 text-sm text-mnema-text" data-testid="export-pending"><Loader2 class="h-4 w-4 animate-spin" />{{ $t('security.privacy.exportPending') }}</p>
      <a v-else-if="status?.state === 'ready'" href="/api/users/me/export/download" download class="inline-flex items-center gap-1.5 rounded-md bg-mnema-accent px-3 py-1 text-sm font-semibold text-mnema-accent-ink transition hover:bg-mnema-accent-hover" data-testid="export-download" @click="afterDownload"><Download class="h-3.5 w-3.5" />{{ $t('security.privacy.exportReady', { when: when(status.expires_at) }) }}</a>
      <p v-else-if="status?.state === 'failed'" role="alert" class="text-sm text-mnema-danger" data-testid="export-failed">{{ $t('security.privacy.exportFailed') }}</p>
      <p v-else-if="status && status.state !== 'none'" class="text-sm text-mnema-tertiary" data-testid="export-done">{{ $t(status.state === 'downloaded' ? 'security.privacy.exportDownloaded' : 'security.privacy.exportExpired') }}</p>

      <form v-if="asking === 'export'" class="space-y-2" @submit.prevent="requestExport">
        <input v-model="password" type="password" required autocomplete="current-password" :aria-label="$t('profile.currentPassword')" :placeholder="$t('profile.currentPassword')" class="w-full rounded-lg border border-mnema-border bg-mnema-canvas px-2.5 py-1.5 text-sm text-mnema-text focus:border-mnema-accent focus:outline-hidden" data-testid="privacy-password" />
        <input v-if="needsCode" v-model="code" type="text" required autocomplete="one-time-code" autocapitalize="none" spellcheck="false" maxlength="32" :aria-label="$t('security.twoFactor.code')" :placeholder="$t('security.twoFactor.code')" class="w-full rounded-lg border border-mnema-border bg-mnema-canvas px-2.5 py-1.5 font-mono text-sm tracking-widest text-mnema-text focus:border-mnema-accent focus:outline-hidden" data-testid="privacy-code" />
        <div v-if="error" role="alert" class="rounded-sm border border-mnema-danger/30 bg-mnema-danger/10 p-1.5 text-sm text-mnema-danger">{{ error }}</div>
        <div class="flex justify-end gap-1.5 text-xs">
          <button type="button" class="btn btn-ghost btn-sm" @click="asking = null">{{ $t('common.cancel') }}</button>
          <button type="submit" :disabled="busy" class="btn btn-primary btn-sm" data-testid="export-submit">{{ $t('security.privacy.exportRequest') }}</button>
        </div>
      </form>
      <template v-else-if="status?.state !== 'pending' && status?.state !== 'ready'">
        <button v-if="canRequest" type="button" class="btn btn-secondary btn-sm" data-testid="export-start" @click="ask('export')">{{ $t('security.privacy.exportRequest') }}</button>
        <p v-else class="text-xs text-mnema-tertiary" data-testid="export-wait">{{ $t('security.privacy.exportNext', { when: when(status?.next_request_at) }) }}</p>
      </template>
    </div>

    <!-- The Owner cannot delete the account: this block only explains why, so it is not styled as a live danger. -->
    <div :class="['space-y-2 rounded-lg border p-3', isOwner ? 'border-mnema-hairline' : 'border-mnema-danger/30']" data-testid="delete-account">
      <p :class="['text-sm font-medium', isOwner ? 'text-mnema-text' : 'text-mnema-danger']">{{ $t('security.privacy.deleteTitle') }}</p>
      <p v-if="isOwner" class="text-sm text-mnema-muted" data-testid="delete-owner">{{ $t('security.privacy.deleteOwner') }}</p>
      <template v-else>
        <p class="text-sm text-mnema-muted">{{ $t('security.privacy.deleteIntro') }}</p>
        <form v-if="asking === 'delete'" class="space-y-2" @submit.prevent="deleteAccount">
          <input v-model="typedName" type="text" required autocomplete="off" spellcheck="false" :aria-label="$t('security.privacy.deleteType', { name: username })" :placeholder="$t('security.privacy.deleteType', { name: username })" class="w-full rounded-lg border border-mnema-border bg-mnema-canvas px-2.5 py-1.5 text-sm text-mnema-text focus:border-mnema-danger focus:outline-hidden" data-testid="delete-name" />
          <input v-model="password" type="password" required autocomplete="current-password" :aria-label="$t('profile.currentPassword')" :placeholder="$t('profile.currentPassword')" class="w-full rounded-lg border border-mnema-border bg-mnema-canvas px-2.5 py-1.5 text-sm text-mnema-text focus:border-mnema-danger focus:outline-hidden" data-testid="privacy-password" />
          <input v-if="needsCode" v-model="code" type="text" required autocomplete="one-time-code" autocapitalize="none" spellcheck="false" maxlength="32" :aria-label="$t('security.twoFactor.code')" :placeholder="$t('security.twoFactor.code')" class="w-full rounded-lg border border-mnema-border bg-mnema-canvas px-2.5 py-1.5 font-mono text-sm tracking-widest text-mnema-text focus:border-mnema-danger focus:outline-hidden" data-testid="privacy-code" />
          <div v-if="error" role="alert" class="rounded-sm border border-mnema-danger/30 bg-mnema-danger/10 p-1.5 text-sm text-mnema-danger">{{ error }}</div>
          <div class="flex justify-end gap-1.5 text-xs">
            <button type="button" class="btn btn-ghost btn-sm" @click="asking = null">{{ $t('common.cancel') }}</button>
            <button type="submit" :disabled="busy || !deleteReady" class="flex items-center gap-1 rounded-sm bg-mnema-danger px-2.5 py-1 font-bold text-white hover:brightness-110 disabled:opacity-40" data-testid="delete-submit"><Loader2 v-if="busy" class="h-3.5 w-3.5 animate-spin" /><Trash2 v-else class="h-3.5 w-3.5" />{{ $t('security.privacy.deleteConfirm') }}</button>
          </div>
        </form>
        <button v-else type="button" class="btn btn-danger btn-sm" data-testid="delete-start" @click="ask('delete')">{{ $t('security.privacy.deleteButton') }}</button>
      </template>
    </div>
  </section>
</template>
