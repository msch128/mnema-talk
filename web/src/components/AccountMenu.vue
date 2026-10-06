<script setup>
// The ⋯ menu of the user bar: profile, audio, language, admin, legal, sign out.
import { ref, computed, nextTick, onMounted, onBeforeUnmount } from 'vue'
import { User, Sliders, Languages, ShieldCheck, HelpCircle, LogOut, ChevronRight, Activity, Monitor, MonitorOff } from '@lucide/vue'
import { useAuthStore } from '../stores/auth'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { useAppVersionStore } from '../stores/appVersion'
import { useWebRTC } from '../composables/useWebRTC'
import { locale, SUPPORTED, t } from '../i18n'
import UserAvatar from './UserAvatar.vue'

const props = defineProps({ trigger: { type: Object, default: null } })
const emit = defineEmits(['close', 'open-admin', 'open-legal'])

const authStore = useAuthStore()
const voiceStore = useVoiceStore()
const chatStore = useChatStore()
const toasts = useToastStore()
const versionStore = useAppVersionStore()
const { startScreenShare, stopScreenShare } = useWebRTC()

const root = ref(null)
const langOpen = ref(false)
const currentLanguage = computed(() => t(`language.${locale.value}`))

function items() {
  return root.value ? [...root.value.querySelectorAll('[role^="menuitem"]')] : []
}

function close(returnFocus = true) {
  emit('close')
  if (returnFocus) props.trigger?.focus()
}

function run(fn) {
  close()
  fn()
}

async function chooseLanguage(l) {
  langOpen.value = false
  close()
  if (l === locale.value) return
  try {
    await authStore.changeLocale(l)
    toasts.success(t('account.languageSet', { language: t(`language.${l}`) }))
  } catch (err) {
    toasts.error(err.message || t('account.languageFailed'))
  }
}

function toggleShare() {
  if (voiceStore.isScreenSharing) stopScreenShare()
  else startScreenShare()
}

function onKeydown(e) {
  const list = items().filter(el => el.offsetParent !== null || el === document.activeElement)
  const i = list.indexOf(document.activeElement)
  const inSub = !!document.activeElement?.closest?.('[data-submenu]')
  if (e.key === 'Escape') {
    e.preventDefault()
    e.stopPropagation()
    if (langOpen.value) {
      langOpen.value = false
      nextTick(() => root.value?.querySelector('[data-lang-trigger]')?.focus())
    } else close()
  } else if (e.key === 'ArrowDown') {
    e.preventDefault()
    list[(i + 1) % list.length]?.focus()
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    list[(i - 1 + list.length) % list.length]?.focus()
  } else if (e.key === 'ArrowRight' && document.activeElement?.hasAttribute('data-lang-trigger')) {
    e.preventDefault()
    openLang()
  } else if (e.key === 'ArrowLeft' && inSub) {
    e.preventDefault()
    langOpen.value = false
    nextTick(() => root.value?.querySelector('[data-lang-trigger]')?.focus())
  } else if (e.key === 'Tab') {
    close(false)
  }
}

function openLang() {
  langOpen.value = true
  nextTick(() => root.value?.querySelector('[data-submenu] [role="menuitemradio"]')?.focus())
}

function onPointerDown(e) {
  if (root.value && !root.value.contains(e.target) && !props.trigger?.contains(e.target)) close(false)
}

onMounted(() => {
  document.addEventListener('pointerdown', onPointerDown)
  nextTick(() => items()[0]?.focus())
})
onBeforeUnmount(() => document.removeEventListener('pointerdown', onPointerDown))

const item = 'flex min-h-8 w-full items-center gap-2.5 rounded-md px-2 py-1.5 text-left text-sm text-mnema-body-ink transition-colors hover:bg-mnema-hover hover:text-mnema-text focus:outline-none focus-visible:bg-mnema-hover focus-visible:text-mnema-text'
</script>

<template>
  <div
    ref="root"
    role="menu"
    :aria-label="$t('account.menu')"
    class="absolute bottom-full left-2 z-40 mb-1 flex w-[260px] max-w-[calc(100vw-16px)] flex-col rounded-[10px] bg-mnema-elevated p-1.5 shadow-[inset_0_0_0_1px_#2B2F2D,0_12px_32px_rgba(0,0,0,0.5)]"
    @keydown="onKeydown"
  >
    <div class="flex items-center gap-2.5 px-2 pb-2.5 pt-1.5">
      <UserAvatar :user="authStore.user" size="sm" />
      <span class="flex min-w-0 flex-col leading-[18px]">
        <span class="truncate text-sm font-semibold text-mnema-text">{{ authStore.user?.display_name || authStore.user?.username }}</span>
        <span class="truncate text-xs text-mnema-tertiary">{{ authStore.isAdmin ? $t('role.admin') : $t('role.member') }}</span>
      </span>
    </div>
    <div class="mx-0.5 my-1 h-px bg-mnema-hairline" role="separator"></div>

    <button type="button" role="menuitem" tabindex="-1" :class="item" @click="run(() => chatStore.openUserProfile(authStore.user))">
      <User class="h-4 w-4 flex-shrink-0 text-mnema-tertiary" />{{ $t('account.editProfile') }}
    </button>
    <button type="button" role="menuitem" tabindex="-1" :class="item" @click="run(() => { voiceStore.showAudioSettings = true })">
      <Sliders class="h-4 w-4 flex-shrink-0 text-mnema-tertiary" />{{ $t('account.audio') }}
    </button>
    <template v-if="voiceStore.isConnected">
      <button type="button" role="menuitem" tabindex="-1" :class="item" @click="run(() => { voiceStore.showStatsModal = true })">
        <Activity class="h-4 w-4 flex-shrink-0 text-mnema-tertiary" />{{ $t('voice.panel.details') }}
      </button>
      <button type="button" role="menuitem" tabindex="-1" :class="item" @click="run(toggleShare)">
        <MonitorOff v-if="voiceStore.isScreenSharing" class="h-4 w-4 flex-shrink-0 text-mnema-tertiary" />
        <Monitor v-else class="h-4 w-4 flex-shrink-0 text-mnema-tertiary" />
        {{ voiceStore.isScreenSharing ? $t('voice.stopShare') : $t('voice.share') }}
      </button>
    </template>

    <div class="relative">
      <button
        type="button"
        role="menuitem"
        tabindex="-1"
        data-lang-trigger
        aria-haspopup="menu"
        :aria-expanded="langOpen ? 'true' : 'false'"
        :class="[item, langOpen ? 'bg-mnema-hover text-mnema-text' : '']"
        @click="langOpen ? (langOpen = false) : openLang()"
      >
        <Languages class="h-4 w-4 flex-shrink-0 text-mnema-tertiary" />
        {{ $t('account.language') }}
        <span class="ml-auto flex items-center gap-1.5 whitespace-nowrap text-[13px] text-mnema-tertiary">
          {{ currentLanguage }}<ChevronRight class="h-3.5 w-3.5" />
        </span>
      </button>
      <div
        v-if="langOpen"
        data-submenu
        role="menu"
        :aria-label="$t('account.language')"
        class="absolute left-full top-0 z-50 ml-1 flex w-[180px] flex-col rounded-[10px] bg-mnema-elevated p-1.5 shadow-[inset_0_0_0_1px_#2B2F2D,0_12px_32px_rgba(0,0,0,0.5)]"
      >
        <button
          v-for="l in SUPPORTED"
          :key="l"
          type="button"
          role="menuitemradio"
          tabindex="-1"
          :lang="l"
          :aria-checked="locale === l ? 'true' : 'false'"
          :class="item"
          @click="chooseLanguage(l)"
        >
          {{ $t(`language.${l}`) }}
          <span :class="['ml-auto h-3.5 w-3.5 flex-shrink-0 rounded-full', locale === l ? 'bg-mnema-accent shadow-[inset_0_0_0_1.5px_#2DA771,inset_0_0_0_4px_#1C1E1D]' : 'shadow-[inset_0_0_0_1.5px_#6E7672]']"></span>
        </button>
      </div>
    </div>

    <div class="mx-0.5 my-1 h-px bg-mnema-hairline" role="separator"></div>
    <button v-if="authStore.isAdmin" type="button" role="menuitem" tabindex="-1" :class="item" @click="run(() => emit('open-admin'))">
      <ShieldCheck class="h-4 w-4 flex-shrink-0 text-mnema-tertiary" />{{ $t('menu.adminConsole') }}
      <span
        v-if="versionStore.adminUpdateAvailable"
        data-testid="admin-update-badge"
        class="ml-auto rounded-full bg-mnema-accent/15 px-2 py-0.5 text-xs font-semibold text-mnema-accent"
      >{{ $t('update.badge', { version: versionStore.adminUpdate.latest_version }) }}</span>
    </button>
    <button type="button" role="menuitem" tabindex="-1" :class="item" @click="run(() => emit('open-legal'))">
      <HelpCircle class="h-4 w-4 flex-shrink-0 text-mnema-tertiary" />{{ $t('menu.legal') }}
    </button>
    <div class="mx-0.5 my-1 h-px bg-mnema-hairline" role="separator"></div>
    <button type="button" role="menuitem" tabindex="-1" :class="item" @click="run(() => authStore.logout())">
      <LogOut class="h-4 w-4 flex-shrink-0 text-mnema-tertiary" />{{ $t('account.signOut') }}
    </button>
  </div>
</template>
