<script setup lang="ts">
// "Where am I signed in": the member's own sessions on this server, with a
// coarse device label, when each was last used and whether it gets push
// notifications. One session can be signed out, or all the others at once.
import { computed, onMounted, ref } from 'vue'
import { Bell, LogOut, Monitor } from '@lucide/vue'
import { caughtErrorMessage } from '../lib/api'
import { confirm } from '../lib/confirm'
import { listSessions, revokeOtherSessions, revokeSession } from '../lib/accountApi'
import { relativeTime } from '../lib/bots'
import type { AuthMemberSession } from '../types/rest'
import { locale, t } from '../i18n'
import { useAccountStore } from '../stores/account'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'

const account = useAccountStore()
const authStore = useAuthStore()
const toasts = useToastStore()

const sessions = ref<AuthMemberSession[]>([])
const loaded = ref(false)
const error = ref('')
const busy = ref(false)

const others = computed(() => sessions.value.filter(s => !s.current))
const name = (s: AuthMemberSession) => s.device || t('security.sessions.unknownDevice')
const day = (iso: string) => new Date(iso).toLocaleDateString(locale.value, { dateStyle: 'medium' })
// Activity is only accurate to a few minutes, so "just now" covers the first of them.
const lastActive = (iso: string) => (Date.now() - Date.parse(iso) < 5 * 60_000 ? t('security.sessions.justNow') : relativeTime(iso, locale.value))

async function refresh() {
  try {
    sessions.value = await listSessions()
    error.value = ''
  } catch (err) {
    error.value = caughtErrorMessage(err, t('errors.unknown'))
  } finally {
    loaded.value = true
  }
}

async function run(action: () => Promise<void>) {
  busy.value = true
  try {
    await action()
  } catch (err) {
    toasts.error(caughtErrorMessage(err, t('errors.unknown')))
  } finally {
    busy.value = false
  }
}

// Signing out this very session ends the page's session too.
function leave() {
  authStore.resetLocalSession()
  account.close()
  window.location.assign('/')
}

const signOut = (s: AuthMemberSession) => run(async () => {
  const ok = await confirm({
    title: t(s.current ? 'security.sessions.signOutThisTitle' : 'security.sessions.signOutTitle', { device: name(s) }),
    body: t(s.current ? 'security.sessions.signOutThisBody' : 'security.sessions.signOutBody'),
    confirmLabel: t('security.sessions.signOut'), danger: true
  })
  if (!ok) return
  await revokeSession(s.id)
  if (s.current) return leave()
  toasts.success(t('security.sessions.signedOut', { device: name(s) }))
  await refresh()
})

const signOutOthers = () => run(async () => {
  const ok = await confirm({
    title: t('security.sessions.signOutOthersTitle'), body: t('security.sessions.signOutOthersBody'),
    confirmLabel: t('security.sessions.signOutOthers'), danger: true
  })
  if (!ok) return
  const count = await revokeOtherSessions()
  toasts.success(t('security.sessions.signedOutOthers', { count }))
  await refresh()
})

onMounted(refresh)
</script>

<template>
  <section class="settings-card" data-testid="account-sessions" aria-labelledby="security-sessions-title">
    <h3 id="security-sessions-title" class="settings-title">{{ $t('security.sessions.title') }}</h3>
    <p class="settings-help">{{ $t('security.sessions.intro') }}</p>

    <p v-if="error" role="alert" class="text-sm text-mnema-danger" data-testid="sessions-error">{{ error }}</p>
    <p v-else-if="!loaded" role="status" class="text-sm text-mnema-tertiary">{{ $t('security.loading') }}</p>
    <template v-else>
      <ul class="divide-y divide-mnema-hairline rounded-lg border border-mnema-hairline">
        <li v-for="s in sessions" :key="s.id" class="flex items-center gap-3 px-3 py-2.5" :data-session="name(s)" :data-current="s.current ? 'true' : 'false'">
          <Monitor class="h-4 w-4 shrink-0 text-mnema-tertiary" aria-hidden="true" />
          <div class="min-w-0 flex-1">
            <p class="flex flex-wrap items-center gap-x-2 text-sm text-mnema-text">
              <span class="truncate">{{ name(s) }}</span>
              <span v-if="s.current" class="rounded-full bg-mnema-accent-subtle px-2 py-0.5 text-xs text-mnema-accent" data-testid="session-current">{{ $t('security.sessions.thisDevice') }}</span>
              <span v-if="s.push" class="inline-flex items-center gap-1 text-xs text-mnema-tertiary" data-testid="session-push"><Bell class="h-3 w-3" aria-hidden="true" />{{ $t('security.sessions.push') }}</span>
            </p>
            <p class="text-xs text-mnema-tertiary">{{ $t('security.sessions.activity', { active: lastActive(s.last_active_at), since: day(s.created_at) }) }}</p>
          </div>
          <button type="button" class="btn btn-ghost btn-sm" :disabled="busy" :aria-label="$t('security.sessions.signOutLabel', { device: name(s) })" data-testid="session-sign-out" @click="signOut(s)"><LogOut class="h-3.5 w-3.5" aria-hidden="true" />{{ $t('security.sessions.signOut') }}</button>
        </li>
      </ul>
      <button v-if="others.length" type="button" class="btn btn-danger btn-sm" :disabled="busy" data-testid="sessions-sign-out-others" @click="signOutOthers">{{ $t('security.sessions.signOutOthers') }}</button>
      <p v-else class="text-xs text-mnema-tertiary" data-testid="sessions-only-this">{{ $t('security.sessions.onlyThis') }}</p>
      <p class="text-xs text-mnema-tertiary">{{ $t('security.sessions.inactivity') }}</p>
    </template>
  </section>
</template>
