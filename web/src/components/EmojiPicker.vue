<script setup lang="ts">
import type { PropType } from 'vue'
import type PickerElement from 'emoji-picker-element/picker.js'
// Searchable emoji picker (emoji-picker-element). It is loaded on first use
// and reads its emoji data from our own server, never from a CDN.
import { ref, onMounted, onBeforeUnmount, watch } from 'vue'
import { locale } from '../i18n'
import { useDismissable } from '../composables/useDismissable'
import { emojiImage, emojiToken } from '../lib/customEmoji'
import { prefersReducedMotion, useEmojiStore } from '../stores/emoji'

const emit = defineEmits<{ pick: [emoji: string]; close: [] }>()
const props = defineProps({
  // Element that opened the picker; clicks on it do not count as "outside".
  trigger: { type: Object as PropType<HTMLElement | null>, default: null },
  // A custom emoji is picked as its :name: (composers turn it into the token
  // when the message is sent) or, by default, as the token itself.
  shortcode: { type: Boolean, default: false }
})
const store = useEmojiStore()

// The picker lists the instance's custom emoji in a section of their own.
function syncCustomEmoji() {
  if (!picker) return
  const still = prefersReducedMotion()
  picker.customEmoji = store.items.map(e => ({ name: e.name, shortcodes: [e.name], url: emojiImage(e, still).src }))
}
watch(() => store.items, syncCustomEmoji)

const host = ref<HTMLElement | null>(null)

// Anchored to its button, the picker can end up partly off screen on a phone:
// slide it back inside the viewport with a margin.
const EDGE = 8
function keepInViewport() {
  const el = host.value
  if (!el) return
  el.style.translate = ''
  const { left, right } = el.getBoundingClientRect()
  const over = right - (window.innerWidth - EDGE)
  const shift = left < EDGE ? EDGE - left : over > 0 ? -Math.min(over, left - EDGE) : 0
  if (shift !== 0) el.style.translate = `${Math.round(shift)}px 0`
}
const loading = ref(true)
let picker: PickerElement | null = null

async function create() {
  const lang = locale.value === 'en' ? 'en' : 'de'
  const [{ default: Picker }, i18n, data] = await Promise.all([
    import('emoji-picker-element/picker.js'),
    lang === 'en'
      ? import('emoji-picker-element/i18n/en.js')
      : import('emoji-picker-element/i18n/de.js'),
    lang === 'en'
      ? import('emoji-picker-element-data/en/cldr/data.json?url')
      : import('emoji-picker-element-data/de/cldr/data.json?url')
  ])
  if (!host.value) return
  picker = new Picker({ locale: lang, dataSource: data.default, i18n: i18n.default })
  picker.classList.add('dark', 'mnema-emoji-picker')
  picker.addEventListener('emoji-click', e => {
    const unicode = e.detail?.unicode
    if (unicode) {
      emit('pick', unicode)
      return
    }
    const custom = store.byName.get(e.detail?.name ?? '')
    if (custom) emit('pick', props.shortcode ? `:${custom.name}:` : emojiToken(custom))
  })
  syncCustomEmoji()
  host.value.appendChild(picker)
  loading.value = false
  keepInViewport()
  // Search field first, like any other picker.
  requestAnimationFrame(() => picker?.shadowRoot?.querySelector('input')?.focus())
}

// Presses on the picker or on its trigger don't close it; Escape does.
useDismissable(() => [host.value, props.trigger], () => emit('close'))

onMounted(() => {
  create().catch(err => {
    console.warn('Emoji picker failed to load:', err)
    emit('close')
  })
})
onBeforeUnmount(() => {
  picker?.remove()
  picker = null
})
</script>

<template>
  <div
    ref="host"
    data-testid="emoji-picker"
    class="z-40 overflow-hidden rounded-[10px] bg-mnema-elevated shadow-[inset_0_0_0_1px_#2B2F2D,0_12px_32px_rgba(0,0,0,0.5)]"
  >
    <div v-if="loading" class="flex h-[360px] w-[340px] max-w-[calc(100vw-16px)] items-center justify-center text-sm text-mnema-tertiary">
      {{ $t('emoji.loading') }}
    </div>
  </div>
</template>

<style>
emoji-picker.mnema-emoji-picker {
  --background: #1c1e1d;
  --border-color: transparent;
  --border-radius: 10px;
  --button-active-background: #2b2f2d;
  --button-hover-background: #202423;
  --category-font-color: #8e9c94;
  --category-font-size: 0.75rem;
  --emoji-size: 1.375rem;
  --emoji-padding: 0.4rem;
  --indicator-color: #2da771;
  --input-border-color: #2b2f2d;
  --input-border-radius: 6px;
  --input-font-color: #e6eae8;
  --input-placeholder-color: #8e9c94;
  --outline-color: #2da771;
  --num-columns: 8;
  --skintone-border-radius: 6px;
  width: 340px;
  max-width: calc(100vw - 16px);
  height: 360px;
  font-family: Inter, system-ui, sans-serif;
}
</style>
