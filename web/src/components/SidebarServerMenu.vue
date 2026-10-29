<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import { ShieldCheck, Crown, Plus, FolderPlus, ChevronDown, X, CheckCheck, ScrollText, Bell } from '@lucide/vue'
import { usePermissionsStore } from '../stores/permissions'
import { useOnboardingStore } from '../stores/onboarding'
import { useAppVersionStore } from '../stores/appVersion'
import { rulesOpen } from '../lib/communityRules'
import { notifyDialogOpen } from '../lib/notifyDialog'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { t } from '../i18n'
import { useDismissable } from '../composables/useDismissable'
import { useCommunityName } from '../composables/useCommunityName'

// The server header (48px, aligned with the channel header) and its dropdown:
// admin console, creating channels and categories, the legal notices.

const emit = defineEmits<{ 'open-admin': []; 'open-legal': []; 'create-channel': []; 'create-category': []; 'mark-all-read': [] }>()

const permissions = usePermissionsStore()
const features = useServerFeaturesStore()
const onboarding = useOnboardingStore()
// Only people who may open the console see that a new release is out.
const versionStore = useAppVersionStore()
const updateHint = computed(() => permissions.canOpenAdmin && versionStore.adminUpdateAvailable)
const showRules = () => { rulesOpen.value = true }
const showNotifications = () => { notifyDialogOpen.value = true }
// The header carries the community's own name (Admin > Welcome & rules).
const serverName = useCommunityName()
const subtitle = computed(() => t('sidebar.subtitle'))
const mark = computed(() => Array.from(serverName.value)[0]?.toUpperCase() ?? 'M')

const menuOpen = ref(false)
const menuRoot = ref<HTMLElement | null>(null)
const menuButton = ref<HTMLButtonElement | null>(null)
const menuEl = ref<HTMLElement | null>(null)

function menuItems() {
  return menuEl.value ? [...menuEl.value.querySelectorAll<HTMLElement>('[role="menuitem"]')] : []
}

function openMenu(focusIndex = 0) {
  menuOpen.value = true
  nextTick(() => {
    const items = menuItems()
    items[(focusIndex + items.length) % items.length]?.focus()
  })
}

function closeMenu(returnFocus = false) {
  if (!menuOpen.value) return
  menuOpen.value = false
  if (returnFocus) menuButton.value?.focus()
}

function toggleMenu() {
  if (menuOpen.value) closeMenu()
  else openMenu()
}

function onMenuButtonKeydown(e: KeyboardEvent) {
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    openMenu(0)
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    openMenu(-1)
  }
}

function onMenuKeydown(e: KeyboardEvent) {
  const items = menuItems()
  const i = items.findIndex(el => el === document.activeElement)
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    items[(i + 1) % items.length]?.focus()
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    items[(i - 1 + items.length) % items.length]?.focus()
  } else if (e.key === 'Home') {
    e.preventDefault()
    items[0]?.focus()
  } else if (e.key === 'End') {
    e.preventDefault()
    items[items.length - 1]?.focus()
  } else if (e.key === 'Tab') {
    closeMenu()
  }
}

function runMenuAction(action: () => void) {
  closeMenu(true)
  action()
}

// A press elsewhere closes the menu; Escape also returns focus to its button.
useDismissable(menuRoot, (e, reason) => closeMenu(reason === 'escape'), { active: menuOpen })

watch(() => permissions.can('manage_channels'), () => closeMenu())

const menuItemClass = 'w-full h-8 px-2 flex items-center justify-between gap-3 rounded-sm text-sm text-left text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text focus:outline-hidden focus-visible:bg-mnema-hover focus-visible:text-mnema-text transition-colors'
</script>

<template>
  <div ref="menuRoot" class="relative shrink-0">
    <button
      ref="menuButton"
      type="button"
      aria-haspopup="menu"
      :aria-expanded="menuOpen ? 'true' : 'false'"
      aria-controls="server-menu"
      :class="[
        'relative w-full h-12 pl-3 pr-3 border-b border-mnema-hairline flex items-center gap-2.5 text-left transition-colors focus:outline-hidden focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-mnema-accent',
        menuOpen ? 'bg-mnema-hover' : 'hover:bg-mnema-hover/70'
      ]"
      v-tooltip.visual="serverName"
      @click="toggleMenu"
      @keydown="onMenuButtonKeydown"
    >
      <!-- Mnema Forest Mark -->
      <span class="w-7 h-7 rounded-md bg-mnema-band border border-mnema-mint/30 flex items-center justify-center text-mnema-mint font-semibold text-sm shadow-xs shrink-0">
        {{ mark }}
      </span>
      <span class="flex flex-col min-w-0 flex-1">
        <span class="font-semibold text-nav text-mnema-text truncate">{{ serverName }}</span>
        <span class="text-xs text-mnema-tertiary truncate">{{ subtitle }}</span>
      </span>
      <X v-if="menuOpen" class="w-4 h-4 text-mnema-muted shrink-0" />
      <ChevronDown v-else class="w-4 h-4 text-mnema-muted shrink-0" />
      <span
        v-if="updateHint"
        data-testid="admin-update-dot"
        class="absolute right-9 top-3 h-2 w-2 rounded-full bg-mnema-accent ring-2 ring-mnema-raised"
        aria-hidden="true"
      ></span>
    </button>

    <div
      v-if="menuOpen"
      id="server-menu"
      ref="menuEl"
      role="menu"
      :aria-label="$t('sidebar.menu')"
      class="absolute left-2 top-[52px] z-40 w-max min-w-[calc(100%-16px)] max-w-[calc(100vw-16px)] p-1.5 rounded-lg bg-mnema-elevated border border-mnema-border shadow-xl"
      @keydown="onMenuKeydown"
    >
      <template v-if="permissions.canOpenAdmin || permissions.can('manage_channels')">
        <button v-if="permissions.canOpenAdmin" type="button" role="menuitem" tabindex="-1" :class="menuItemClass" @click="runMenuAction(() => emit('open-admin'))">
          <span class="truncate">{{ $t('menu.adminConsole') }}</span>
          <span
            v-if="updateHint"
            data-testid="admin-update-badge"
            class="ml-auto rounded-full bg-mnema-accent/15 px-2 py-0.5 text-xs font-semibold text-mnema-accent"
          >{{ $t('update.badge', { version: versionStore.adminUpdate?.latest_version }) }}</span>
          <Crown class="w-4 h-4 text-mnema-amber shrink-0" />
        </button>
        <template v-if="permissions.can('manage_channels')">
          <button type="button" role="menuitem" tabindex="-1" :class="menuItemClass" @click="runMenuAction(() => emit('create-channel'))">
            <span class="truncate">{{ $t('channel.create') }}</span>
            <Plus class="w-4 h-4 shrink-0" />
          </button>
          <button type="button" role="menuitem" tabindex="-1" :class="menuItemClass" @click="runMenuAction(() => emit('create-category'))">
            <span class="truncate">{{ $t('sidebar.createCategory') }}</span>
            <FolderPlus class="w-4 h-4 shrink-0" />
          </button>
        </template>
        <div class="my-1 h-px bg-mnema-hairline" role="separator"></div>
      </template>
      <button type="button" role="menuitem" tabindex="-1" :class="menuItemClass" @click="runMenuAction(() => emit('mark-all-read'))">
        <span class="truncate">{{ $t('sidebar.markAllRead') }}</span>
        <CheckCheck class="w-4 h-4 shrink-0" />
      </button>
      <button v-if="features.notifyLevelsV2" type="button" role="menuitem" tabindex="-1" :class="menuItemClass" data-testid="menu-notification-settings" @click="runMenuAction(showNotifications)">
        <span class="truncate">{{ $t('menu.notificationSettings') }}</span>
        <Bell class="w-4 h-4 shrink-0" />
      </button>
      <button v-if="onboarding.state?.rules_text" type="button" role="menuitem" tabindex="-1" :class="menuItemClass" data-testid="menu-community-rules" @click="runMenuAction(showRules)">
        <span class="truncate">{{ $t('menu.rules') }}</span>
        <ScrollText class="w-4 h-4 shrink-0" />
      </button>
      <button type="button" role="menuitem" tabindex="-1" :class="menuItemClass" @click="runMenuAction(() => emit('open-legal'))">
        <span class="truncate">{{ $t('menu.legal') }}</span>
        <ShieldCheck class="w-4 h-4 shrink-0" />
      </button>
    </div>
  </div>
</template>
