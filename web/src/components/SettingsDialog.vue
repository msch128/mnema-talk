<script setup lang="ts">
// "Settings": everything a member sets for themselves in one dialog, one
// section at a time instead of one long scroll. Sections: sign-in security,
// sessions, privacy, the roles members may choose, data and account, push
// and app, audio.
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch, type Component } from 'vue'
import { Database, KeyRound, Laptop, Shield, Sliders, Smartphone, Tags } from '@lucide/vue'
import { caughtErrorMessage } from '../lib/api'
import { SETTINGS_TABS, focusSettingsField, rememberSettingsTab, settingsField, settingsFieldTarget, settingsTab, type SettingsTab } from '../lib/settingsDialog'
import { platform } from '../lib/platform'
import { t } from '../i18n'
import { useAccountStore } from '../stores/account'
import { usePermissionsStore } from '../stores/permissions'
import BaseDialog from './BaseDialog.vue'
import ProfilePassword from './ProfilePassword.vue'
import AccountEmail from './AccountEmail.vue'
import AccountTwoFactor from './AccountTwoFactor.vue'
import AccountSessions from './AccountSessions.vue'
import AccountPrivacy from './AccountPrivacy.vue'
import AccountData from './AccountData.vue'
import AppSettingsPanel from './AppSettingsPanel.vue'
import AudioSettingsPanel from './AudioSettingsPanel.vue'
import SelfRolesPanel from './SelfRolesPanel.vue'

const account = useAccountStore()
const permissions = usePermissionsStore()
const loadError = ref('')
const nav = ref<HTMLElement | null>(null)
// The dialog's own first focus goes to the requested field when it is already rendered.
const initialFocus = (root: HTMLElement) => (settingsField.value ? settingsFieldTarget(root, settingsField.value) : null)
const panel = ref<HTMLElement | null>(null)

const ICONS: Record<SettingsTab, Component> = { signin: KeyRound, sessions: Laptop, privacy: Shield, data: Database, app: Smartphone, audio: Sliders, roles: Tags }
// Only these need the loaded account (e-mail, two-factor state).
const NEEDS_ACCOUNT: ReadonlySet<SettingsTab> = new Set(['signin', 'data'])

// The installed desktop client has its own notifications and no install step.
// Roles shows only where an admin opened some to everyone.
const visible = computed(() => SETTINGS_TABS.filter(tab => (tab !== 'app' || !platform.isTrustedDesktop()) && (tab !== 'roles' || permissions.roles.some(r => r.self_assignable))))
const groups = computed(() => [
  { id: 'device', tabs: visible.value.filter(tab => ['app', 'audio'].includes(tab)) },
  { id: 'account', tabs: visible.value.filter(tab => !['app', 'audio'].includes(tab)) }
])
const active = computed<SettingsTab>(() => (visible.value.includes(settingsTab.value) ? settingsTab.value : SETTINGS_TABS[0]))
const waiting = computed(() => NEEDS_ACCOUNT.has(active.value) && !account.info)

function select(tab: SettingsTab) {
  rememberSettingsTab(tab)
  settingsField.value = null
}

// A notice or menu may ask for one field: once its section has rendered (the
// audio devices fill in late), scroll to it and put the cursor there.
const FIELD_TRIES = 10
const FIELD_RETRY_MS = 50
async function revealField() {
  const field = settingsField.value
  if (!field || waiting.value) return
  // The dialog places its own first focus a tick after it opens; this comes after it.
  for (let i = 0; i < FIELD_TRIES && settingsField.value === field; i++) {
    await new Promise(resolve => setTimeout(resolve, i === 0 ? 0 : FIELD_RETRY_MS))
    if (panel.value && focusSettingsField(panel.value, field)) break
  }
  if (settingsField.value === field) settingsField.value = null
}
watch([settingsField, active, waiting], () => { void revealField() }, { flush: 'post', immediate: true })

// Arrow keys move through the sections; Tab leaves the list for the content.
function onNavKey(e: KeyboardEvent) {
  const keys: Record<string, number> = { ArrowDown: 1, ArrowRight: 1, ArrowUp: -1, ArrowLeft: -1 }
  const list = visible.value
  const at = list.indexOf(active.value)
  let next = -1
  if (e.key in keys) next = (at + (keys[e.key] ?? 0) + list.length) % list.length
  else if (e.key === 'Home') next = 0
  else if (e.key === 'End') next = list.length - 1
  const tab = list[next]
  if (!tab) return
  e.preventDefault()
  select(tab)
  void nextTick(() => nav.value?.querySelector<HTMLElement>(`#settings-tab-${tab}`)?.focus())
}

onMounted(async () => {
  try {
    await account.load()
  } catch (err) {
    loadError.value = caughtErrorMessage(err, t('errors.unknown'))
  }
})
// The loaded account is dropped with the dialog, however it is closed.
onBeforeUnmount(() => account.close())
</script>

<template>
  <BaseDialog :title="$t('settings.title')" :subtitle="$t('settings.subtitle')" panel-class="max-w-3xl md:h-[min(46rem,calc(100vh-4rem))]" sheet :initial-focus="initialFocus" @close="account.close()">
    <div class="flex min-h-0 flex-1 max-md:flex-col" data-testid="settings-dialog">
      <nav
        ref="nav"
        :aria-label="$t('settings.nav')"
        class="shrink-0 border-mnema-hairline bg-mnema-raised/40 md:w-56 md:overflow-y-auto md:border-r md:p-3 max-md:border-b max-md:p-2"
      >
        <div role="tablist" aria-orientation="vertical" class="flex flex-col gap-4 max-md:flex-row max-md:flex-wrap max-md:gap-1" @keydown="onNavKey">
          <div v-for="g in groups" :key="g.id" class="flex flex-col gap-0.5 max-md:contents">
            <p aria-hidden="true" class="px-2.5 pb-1 font-mono text-[11px] uppercase tracking-wider text-mnema-tertiary max-md:hidden">{{ $t(`settings.group.${g.id}`) }}</p>
            <button
              v-for="tab in g.tabs"
              :id="`settings-tab-${tab}`"
              :key="tab"
              type="button"
              role="tab"
              :aria-selected="active === tab ? 'true' : 'false'"
              :aria-controls="`settings-panel-${tab}`"
              :tabindex="active === tab ? 0 : -1"
              :data-testid="`settings-tab-${tab}`"
              :class="[
                'flex min-h-9 items-center gap-2.5 rounded-lg px-2.5 py-2 text-left text-sm transition-colors focus:outline-hidden focus-visible:ring-2 focus-visible:ring-mnema-accent max-md:min-h-11 max-md:px-3',
                active === tab ? 'bg-mnema-accent/10 font-medium text-mnema-text' : 'text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text'
              ]"
              @click="select(tab)"
            >
              <component :is="ICONS[tab]" :class="['h-4 w-4 shrink-0', active === tab ? 'text-mnema-accent' : 'text-mnema-tertiary']" aria-hidden="true" />
              {{ $t(`settings.tab.${tab}`) }}
            </button>
          </div>
        </div>
      </nav>

      <div
        :id="`settings-panel-${active}`"
        ref="panel"
        role="tabpanel"
        tabindex="0"
        :aria-labelledby="`settings-tab-${active}`"
        class="min-h-0 flex-1 space-y-5 overflow-y-auto p-6 focus:outline-hidden max-md:p-4"
        data-testid="account-security"
      >
        <div v-if="waiting && loadError" role="alert" class="rounded-sm border border-mnema-danger/30 bg-mnema-danger/10 p-2 text-sm text-mnema-danger">{{ loadError }}</div>
        <p v-else-if="waiting" role="status" class="text-sm text-mnema-tertiary">{{ $t('security.loading') }}</p>
        <template v-else>
          <template v-if="active === 'signin'">
            <ProfilePassword />
            <AccountTwoFactor />
            <AccountEmail />
          </template>
          <AccountSessions v-else-if="active === 'sessions'" />
          <AccountPrivacy v-else-if="active === 'privacy'" />
          <SelfRolesPanel v-else-if="active === 'roles'" />
          <AccountData v-else-if="active === 'data'" />
          <AppSettingsPanel v-else-if="active === 'app'" />
          <AudioSettingsPanel v-else />
        </template>
      </div>
    </div>
  </BaseDialog>
</template>
