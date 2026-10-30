<script setup lang="ts">
import type { PropType } from 'vue'
import type PickerElement from 'emoji-picker-element/picker.js'
// Searchable emoji picker (emoji-picker-element). It is loaded on first use
// and reads its emoji data from our own server, never from a CDN.
import { ref, onMounted, onBeforeUnmount, watch } from 'vue'
import { locale } from '../i18n'
import { styleEmojiPicker } from '../lib/emojiPickerSheet'
import { shieldEscape, useDismissable } from '../composables/useDismissable'
import { emojiImage, emojiToken } from '../lib/customEmoji'
import { enhanceEmojiPicker, type EmojiPreview } from '../lib/emojiPickerExtras'
import { prefersReducedMotion, useEmojiStore } from '../stores/emoji'

const emit = defineEmits<{ pick: [emoji: string]; close: [reason?: 'outside' | 'escape'] }>()
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
// slide it back inside the viewport with a margin. It is placed as soon as it
// is mounted (the loading box is already on screen) and again when the window
// changes. The right edge comes from the layout width, not the painted box: the
// pop-in animation scales the painted box while it runs.
const EDGE = 8
function keepInViewport() {
  const el = host.value
  if (!el) return
  el.style.translate = ''
  const { left } = el.getBoundingClientRect()
  const right = left + el.offsetWidth
  const over = right - (window.innerWidth - EDGE)
  const shift = left < EDGE ? EDGE - left : over > 0 ? -Math.min(over, left - EDGE) : 0
  if (shift !== 0) el.style.translate = `${Math.round(shift)}px 0`
}
const loading = ref(true)
let picker: PickerElement | null = null
let extras: ReturnType<typeof enhanceEmojiPicker> | null = null
// The emoji under the pointer or the keyboard focus, with its :shortcode:.
const preview = ref<EmojiPreview | null>(null)

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
  host.value.prepend(picker)
  extras = enhanceEmojiPicker(picker, value => { preview.value = value })
  styleEmojiPicker(picker)
  loading.value = false
  keepInViewport()
  // Search field first, like any other picker.
  requestAnimationFrame(() => picker?.shadowRoot?.querySelector('input')?.focus())
}

// Presses on the picker or on its trigger don't close it; Escape does.
// Escape first clears a search, a second press closes.
useDismissable(() => [host.value, props.trigger], (_event, reason) => emit('close', reason))
let unshield: (() => void) | null = null
// Typing in the search is not a shortcut for the page behind it.
function keepTyping(e: KeyboardEvent) {
  if (!e.ctrlKey && !e.metaKey && !e.altKey) e.stopPropagation()
}

onMounted(() => {
  unshield = shieldEscape(() => extras?.clearSearch() ?? false)
  keepInViewport()
  window.addEventListener('resize', keepInViewport)
  create().catch(err => {
    console.warn('Emoji picker failed to load:', err)
    emit('close')
  })
})
onBeforeUnmount(() => {
  unshield?.()
  extras?.detach()
  extras = null
  window.removeEventListener('resize', keepInViewport)
  picker?.remove()
  picker = null
})
</script>

<template>
  <div
    ref="host"
    data-testid="emoji-picker"
    role="dialog"
    :aria-label="$t('emoji.suggestTitle')"
    class="z-40 overflow-hidden rounded-xl bg-mnema-elevated shadow-popover pop-in"
    @keydown="keepTyping"
  >
    <div v-if="loading" class="flex h-[377px] w-[340px] max-w-[calc(100vw-16px)] items-center justify-center text-sm text-mnema-tertiary">
      {{ $t('emoji.loading') }}
    </div>
    <div
      v-if="!loading"
      data-testid="emoji-preview"
      aria-live="off"
      class="flex h-11 items-center gap-2 border-t border-mnema-hairline px-3 text-sm text-mnema-muted"
    >
      <template v-if="preview">
        <img v-if="preview.src" :src="preview.src" alt="" class="h-7 w-7 object-contain">
        <span v-else class="text-2xl leading-none" aria-hidden="true">{{ preview.glyph }}</span>
        <span class="truncate font-mono" data-testid="emoji-preview-code">{{ preview.code }}</span>
      </template>
    </div>
  </div>
</template>

<style>
emoji-picker.mnema-emoji-picker {
  --background: #1c1e1d;
  --border-color: transparent;
  --border-radius: 16px;
  --button-active-background: #2b2f2d;
  --button-hover-background: #202423;
  --category-font-color: #8e9c94;
  --category-font-size: 0.75rem;
  --emoji-size: 1.5rem;
  --emoji-padding: 0.45rem;
  --indicator-color: #2da771;
  --input-border-color: #2b2f2d;
  --input-border-radius: 9999px;
  --input-font-color: #e6eae8;
  --input-placeholder-color: #8e9c94;
  --outline-color: transparent;
  --num-columns: 8;
  --skintone-border-radius: 6px;
  width: 340px;
  max-width: calc(100vw - 16px);
  /* Six whole rows of the grid between the tabs and the frequent row. */
  height: 377px;
  font-family: Inter, system-ui, sans-serif;
}
</style>
