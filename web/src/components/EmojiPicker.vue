<script setup>
// Searchable emoji picker (emoji-picker-element). It is loaded on first use
// and reads its emoji data from our own server, never from a CDN.
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { locale } from '../i18n'
import { useDismissable } from '../composables/useDismissable'

const emit = defineEmits(['pick', 'close'])
const props = defineProps({
  // Element that opened the picker; clicks on it do not count as "outside".
  trigger: { type: Object, default: null }
})

const host = ref(null)
const loading = ref(true)
let picker = null

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
    if (unicode) emit('pick', unicode)
  })
  host.value.appendChild(picker)
  loading.value = false
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
