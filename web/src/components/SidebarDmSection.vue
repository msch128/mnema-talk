<script setup lang="ts">
// The "Direct messages" section at the top of the sidebar (and so of the
// phone drawer): the member's conversations, newest first, with unread
// badges, and the button that starts one. Nothing shows on a server without
// direct messages.
import { computed, ref } from 'vue'
import { ChevronDown, ChevronRight, Plus, Users } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useVoiceStore } from '../stores/voice'
import { useCompactLayout } from '../composables/useCompactLayout'
import { useDmActions } from '../composables/useDmActions'
import { dmPeer, dmTitle } from '../lib/dm'
import UserAvatar from './UserAvatar.vue'
import DmCreateDialog from './DmCreateDialog.vue'

const COLLAPSED_KEY = 'mnema_dm_collapsed'
// Longer lists show the newest conversations and a button for the rest.
const SHOWN = 8

const chatStore = useChatStore()
const authStore = useAuthStore()
const features = useServerFeaturesStore()
const voiceStore = useVoiceStore()
const { closeNav } = useCompactLayout()
const { canStart } = useDmActions()

function readCollapsed() {
  try {
    return localStorage.getItem(COLLAPSED_KEY) === '1'
  } catch {
    return false
  }
}

const collapsed = ref(readCollapsed())
const showAll = ref(false)
const creating = ref(false)

function toggle() {
  collapsed.value = !collapsed.value
  try {
    localStorage.setItem(COLLAPSED_KEY, collapsed.value ? '1' : '0')
  } catch {
    // Not remembered; it still works for this visit.
  }
}

const visible = computed(() => features.dm && (chatStore.dms.length > 0 || canStart.value))
const unreadOf = (id: string) => chatStore.readStates[id]?.unread_count || 0
const isOpen = (id: string) => chatStore.activeChannel?.id === id && voiceStore.activeView === 'chat'
const unreadTotal = computed(() => chatStore.dms.reduce((sum, dm) => sum + unreadOf(dm.id), 0))

// A collapsed section still shows the open conversation and the ones with news.
const rows = computed(() => {
  const all = chatStore.dms
  if (collapsed.value) return all.filter(dm => isOpen(dm.id) || unreadOf(dm.id) > 0)
  return showAll.value ? all : all.slice(0, SHOWN)
})
const hiddenCount = computed(() => (collapsed.value || showAll.value ? 0 : Math.max(0, chatStore.dms.length - SHOWN)))

function open(id: string) {
  closeNav()
  void chatStore.openDm(id)
}
</script>

<template>
  <section v-if="visible" class="mb-3" data-testid="dm-section" :aria-label="$t('dm.section')">
    <div class="relative flex h-6 items-center gap-1 pr-1 max-md:h-11">
      <button
        type="button"
        class="flex h-6 min-w-0 flex-1 items-center gap-0.5 rounded-sm pl-0.5 text-left text-xs font-semibold uppercase tracking-wide text-mnema-tertiary transition-colors hover:text-mnema-muted focus:outline-hidden focus-visible:text-mnema-text focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-mnema-accent max-md:h-11"
        :aria-expanded="collapsed ? 'false' : 'true'"
        data-testid="dm-toggle"
        @click="toggle"
      >
        <ChevronRight v-if="collapsed" class="h-3 w-3 shrink-0" />
        <ChevronDown v-else class="h-3 w-3 shrink-0" />
        <span class="truncate">{{ $t('dm.section') }}</span>
        <span
          v-if="collapsed && unreadTotal > 0"
          class="ml-auto min-w-[18px] rounded-full bg-mnema-danger px-1.5 py-0.5 text-center text-xs font-bold normal-case leading-none tracking-normal text-white"
          :aria-label="$t('dm.unreadTotal', { n: unreadTotal })"
        >{{ unreadTotal }}</span>
      </button>
      <button
        v-if="canStart"
        type="button"
        data-testid="dm-new"
        class="btn btn-icon h-5 w-5 shrink-0 max-md:h-11 max-md:w-11"
        v-tooltip="$t('dm.new')"
        :aria-label="$t('dm.new')"
        @click="creating = true"
      >
        <Plus class="h-4 w-4" />
      </button>
    </div>

    <div class="mt-0.5">
      <div
        v-for="dm in rows"
        :key="dm.id"
        role="button"
        tabindex="0"
        :data-dm="dm.id"
        :aria-current="isOpen(dm.id) ? 'true' : undefined"
        :class="[
          'group relative mb-px flex h-[34px] w-full min-w-0 cursor-pointer items-center justify-between gap-1.5 rounded-md px-2 text-left text-nav transition-colors focus:outline-hidden focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-mnema-accent max-md:h-11 motion-reduce:transition-none',
          isOpen(dm.id)
            ? 'bg-mnema-hover font-medium text-mnema-text shadow-[inset_2px_0_0_0_#2DA771]'
            : unreadOf(dm.id) > 0
              ? 'font-semibold text-mnema-text hover:bg-mnema-hover/70'
              : 'text-mnema-muted hover:bg-mnema-hover/70 hover:text-mnema-text'
        ]"
        @click="open(dm.id)"
        @keydown.enter.self.prevent="open(dm.id)"
        @keydown.space.self.prevent="open(dm.id)"
      >
        <div class="flex min-w-0 items-center gap-2">
          <UserAvatar
            v-if="dmPeer(dm, authStore.user?.id)"
            :user="dmPeer(dm, authStore.user?.id)"
            size="xs"
            show-status
            :status="chatStore.presenceOf(dmPeer(dm, authStore.user?.id)?.id ?? null)"
            ring-class="bg-mnema-raised"
          />
          <span v-else class="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-mnema-surface text-mnema-tertiary" aria-hidden="true">
            <Users class="h-3.5 w-3.5" />
          </span>
          <span class="truncate" data-testid="dm-title">{{ dmTitle(dm, authStore.user?.id) }}</span>
        </div>
        <span
          v-if="unreadOf(dm.id) > 0 && !isOpen(dm.id)"
          data-testid="dm-unread"
          class="min-w-[18px] shrink-0 rounded-full bg-mnema-danger px-1.5 py-0.5 text-center text-xs font-bold leading-none text-white"
        >{{ unreadOf(dm.id) }}</span>
      </div>

      <p v-if="!chatStore.dms.length" class="truncate px-2 py-1 text-sm italic text-mnema-tertiary" data-testid="dm-empty">{{ $t('dm.empty') }}</p>
      <button
        v-if="hiddenCount > 0"
        type="button"
        class="w-full rounded-md px-2 py-1 text-left text-xs text-mnema-tertiary transition hover:text-mnema-text focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-mnema-accent max-md:h-11"
        data-testid="dm-more"
        @click="showAll = true"
      >{{ $t('dm.showMore', { n: hiddenCount }) }}</button>
    </div>

    <DmCreateDialog v-if="creating" @close="creating = false" />
  </section>
</template>
