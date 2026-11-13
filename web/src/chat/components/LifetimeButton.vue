<script setup lang="ts">
// The composer's timer: one calm icon button that opens the lifetimes (Off,
// the presets, Custom). The chosen lifetime shows in the button itself, next
// to the send button, and stays for this conversation on this device
// (stores/lifetime.ts). Nothing shows while the community allows none.
import { computed, ref, watch } from 'vue'
import { Timer, Check } from '@lucide/vue'
import type { ContextMenuItem } from '../../shared/components/menuTypes'
import ContextMenu from '../../shared/components/ContextMenu.vue'
import LifetimeCustomDialog from './LifetimeCustomDialog.vue'
import { useLifetimeStore } from '../../stores/lifetime'
import { useServerFeaturesStore } from '../../stores/serverFeatures'
import { LIFETIME_PRESETS, lifetimeLabel, validLifetime } from '../../shared/lib/lifetime'
import { t } from '../../i18n'

const props = defineProps({
  channelId: { type: String, required: true },
  disabled: { type: Boolean, default: false }
})

const lifetimes = useLifetimeStore()
const features = useServerFeaturesStore()
// Read once the server says it knows lifetimes; a change comes with channels_changed.
watch(() => features.has('messages.lifetime'), known => { if (known) void lifetimes.ensure() }, { immediate: true })

const chosen = computed(() => lifetimes.forChannel(props.channelId))
const button = ref<HTMLElement | null>(null)
const open = ref(false)
const custom = ref(false)
const label = computed(() => (chosen.value ? t('lifetime.chosen', { time: lifetimeLabel(chosen.value) }) : t('lifetime.button')))

// The chosen row carries a check mark.
const mark = (s: number) => (chosen.value === s ? { icon: Check } : {})
const items = computed((): ContextMenuItem[] => [
  { type: 'label', label: t('lifetime.menuTitle') },
  { label: t('lifetime.off'), ...mark(0), action: () => lifetimes.choose(props.channelId, 0) },
  ...LIFETIME_PRESETS.filter(s => validLifetime(s, lifetimes.maxSeconds)).map((s): ContextMenuItem => ({
    label: lifetimeLabel(s), ...mark(s), action: () => lifetimes.choose(props.channelId, s)
  })),
  { label: t('lifetime.custom'), action: () => { custom.value = true } },
  { type: 'separator' },
  { type: 'info', label: t('lifetime.notProtection') }
])
</script>

<template>
  <template v-if="lifetimes.available">
    <button
      ref="button"
      type="button"
      data-testid="lifetime-button"
      :disabled="disabled"
      aria-haspopup="menu"
      :aria-expanded="open ? 'true' : 'false'"
      :aria-label="label"
      v-tooltip="label"
      :class="[
        'focus-ring h-8 max-md:h-11 flex items-center justify-center gap-1 shrink-0 rounded-full transition disabled:opacity-50',
        chosen ? 'px-2 bg-mnema-surface text-mnema-text' : 'w-8 max-md:w-11 text-mnema-tertiary hover:bg-mnema-surface hover:text-mnema-text'
      ]"
      @click="open = !open"
    >
      <Timer class="w-4 h-4 shrink-0" aria-hidden="true" />
      <span v-if="chosen" class="text-xs tabular-nums whitespace-nowrap" data-testid="lifetime-chip">{{ lifetimeLabel(chosen) }}</span>
    </button>
    <ContextMenu v-model="open" :anchor="button" :items="items" :aria-label="$t('lifetime.menuTitle')" />
    <LifetimeCustomDialog
      v-if="custom"
      :initial="chosen"
      :max="lifetimes.maxSeconds"
      @pick="s => { lifetimes.choose(channelId, s); custom = false }"
      @close="custom = false"
    />
  </template>
</template>
