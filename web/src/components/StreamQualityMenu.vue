<script setup>
// The gear on my own screen share: its quality menu (see useStreamQuality).
import { ref, watch } from 'vue'
import { Settings } from '@lucide/vue'
import ContextMenu from './ContextMenu.vue'
import { useMenuState } from '../composables/useNavMenus'
import { useStreamQuality } from '../composables/useStreamQuality'
import { useWebRTC } from '../composables/useWebRTC'
import { t } from '../i18n'

// stage: on the dark video overlay; card: in the own share's card.
const props = defineProps({ variant: { type: String, default: 'stage' } })

const rtc = useWebRTC()
const quality = useStreamQuality({ getStats: () => rtc.getScreenSendStats?.() ?? null })
const menu = useMenuState()
const button = ref(null)
// The menu closes on pointer down outside it (also on this button): a click
// on the button while it was open only closes it.
let wasOpen = false

function onPointerDown() {
  wasOpen = menu.state.open
}

function toggle() {
  if (wasOpen || menu.state.open) {
    wasOpen = false
    menu.state.open = false
    return
  }
  menu.show({ currentTarget: button.value }, () => quality.menuItems({ onChangeSource: () => rtc.startScreenShare() }))
}

watch(() => menu.state.open, open => { if (!open) quality.stopStats() })
</script>

<template>
  <button
    ref="button"
    type="button"
    data-testid="stream-quality-button"
    aria-haspopup="menu"
    :aria-expanded="menu.state.open ? 'true' : 'false'"
    :aria-label="t('talk.quality.title')"
    v-tooltip="t('talk.quality.title')"
    :class="props.variant === 'card'
      ? 'w-7 h-7 flex items-center justify-center rounded-md text-mnema-muted hover:text-mnema-text hover:bg-mnema-hover transition flex-shrink-0'
      : 'p-2 rounded-lg bg-black/75 hover:bg-black/90 text-white transition'"
    @pointerdown="onPointerDown"
    @click="toggle"
  >
    <Settings class="w-4 h-4" />
  </button>
  <ContextMenu
    v-model="menu.state.open"
    :x="menu.state.x"
    :y="menu.state.y"
    :anchor="menu.state.anchor"
    :items="menu.items.value"
    :min-width="260"
    :aria-label="t('talk.quality.title')"
  />
</template>
