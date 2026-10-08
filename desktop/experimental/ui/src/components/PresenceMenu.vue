<script setup lang="ts">
import type { PropType } from 'vue'
import type { ChosenPresence } from '../types/domain'
import { caughtErrorMessage } from '../lib/api'
// Opened from the avatar in the user bar: choose online, away, do not
// disturb or focus. Offline is deliberately not offered.
import { ref, nextTick, onMounted, onBeforeUnmount } from 'vue'
import { MessageSquareText } from '@lucide/vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { CHOOSABLE } from '../lib/presence'
import { t } from '../i18n'
import PresenceDot from './PresenceDot.vue'

const props = defineProps({ trigger: { type: Object as PropType<HTMLElement | null>, default: null } })
const emit = defineEmits<{ close: [] }>()

const authStore = useAuthStore()
const chatStore = useChatStore()
const toasts = useToastStore()
const root = ref<HTMLElement | null>(null)

function items() {
  return root.value ? [...root.value.querySelectorAll<HTMLElement>('[role^="menuitem"]')] : []
}

function close(returnFocus = true) {
  emit('close')
  if (returnFocus) props.trigger?.focus()
}

async function choose(p: ChosenPresence) {
  close()
  if (p === (authStore.user?.presence || 'online')) return
  try {
    await chatStore.setMyPresence(p)
  } catch (err) {
    toasts.error(caughtErrorMessage(err, t('presence.saveFailed')))
  }
}

function editStatus() {
  close()
  if (authStore.user) chatStore.openUserProfile(authStore.user)
}

function onKeydown(e: KeyboardEvent) {
  const list = items()
  const i = list.findIndex(el => el === document.activeElement)
  if (e.key === 'Escape') {
    e.preventDefault()
    e.stopPropagation()
    close()
  } else if (e.key === 'ArrowDown') {
    e.preventDefault()
    list[(i + 1) % list.length]?.focus()
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    list[(i - 1 + list.length) % list.length]?.focus()
  } else if (e.key === 'Tab') {
    close(false)
  }
}

function onPointerDown(e: PointerEvent) {
  if (root.value && !(e.target instanceof Node && (root.value.contains(e.target) || props.trigger?.contains(e.target)))) close(false)
}

onMounted(() => {
  document.addEventListener('pointerdown', onPointerDown)
  nextTick(() => {
    const current = root.value?.querySelector<HTMLElement>('[aria-checked="true"]')
    ;(current || items()[0])?.focus()
  })
})
onBeforeUnmount(() => document.removeEventListener('pointerdown', onPointerDown))

const item = 'flex w-full items-start gap-2.5 rounded-md px-2 py-1.5 text-left text-sm text-mnema-body-ink transition-colors hover:bg-mnema-hover hover:text-mnema-text focus:outline-none focus-visible:bg-mnema-hover focus-visible:text-mnema-text'
</script>

<template>
  <div
    ref="root"
    role="menu"
    data-testid="presence-menu"
    :aria-label="$t('presence.choose')"
    class="absolute bottom-full left-2 z-40 mb-1 flex w-[260px] max-w-[calc(100vw-16px)] flex-col rounded-[10px] bg-mnema-elevated p-1.5 shadow-[inset_0_0_0_1px_#2B2F2D,0_12px_32px_rgba(0,0,0,0.5)]"
    @keydown="onKeydown"
  >
    <button
      v-for="p in CHOOSABLE"
      :key="p"
      type="button"
      role="menuitemradio"
      tabindex="-1"
      :data-presence="p"
      :aria-checked="(authStore.user?.presence || 'online') === p ? 'true' : 'false'"
      :class="[item, (authStore.user?.presence || 'online') === p ? 'bg-mnema-hover/60' : '']"
      @click="choose(p)"
    >
      <PresenceDot :status="p" :size="10" :ring="false" class="mt-[4px]" />
      <span class="flex min-w-0 flex-col leading-[18px]">
        <span class="font-medium text-mnema-text">{{ $t(`presence.${p}`) }}</span>
        <span class="text-xs text-mnema-tertiary">{{ $t(`presence.hint.${p}`) }}</span>
      </span>
    </button>
    <div class="mx-0.5 my-1 h-px bg-mnema-hairline" role="separator"></div>
    <button type="button" role="menuitem" tabindex="-1" :class="[item, 'items-center']" @click="editStatus">
      <MessageSquareText class="h-4 w-4 flex-shrink-0 text-mnema-tertiary" />
      <span class="truncate">{{ authStore.user?.status_text ? $t('presence.editStatus') : $t('presence.setStatus') }}</span>
    </button>
  </div>
</template>
