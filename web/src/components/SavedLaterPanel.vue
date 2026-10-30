<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { Bookmark, X } from '@lucide/vue'
import { useLaterStore } from '../stores/later'
import { useOnboardingStore } from '../stores/onboarding'
import { useSavedStore } from '../stores/saved'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useDismissable } from '../composables/useDismissable'
import { pendingPanel, SAVED_HINT_TIP, type PanelTab } from '../lib/savedPanel'
import Badge from './Badge.vue'
import HeaderPopover from './HeaderPopover.vue'
import LaterList from './LaterList.vue'
import SavedList from './SavedList.vue'

// One header button for what the member keeps for themselves: their saved
// messages (private bookmarks) and what they asked to be reminded of or
// scheduled to send (Later), as two tabs of one panel. The button carries the
// count of reminders that are due. After the member's first save a small hint
// under it says where saved messages live.
// Open from the header's More menu when the header is narrow: no button of its own then.
defineProps<{ bare?: boolean }>()
const open = defineModel<boolean>('open', { default: false })

const saved = useSavedStore()
const later = useLaterStore()
const features = useServerFeaturesStore()
const onboarding = useOnboardingStore()
const root = ref<HTMLElement | null>(null)
const tab = ref<PanelTab>('saved')
const focusId = ref<string | undefined>(undefined)

const hasSaved = computed(() => features.has('saved'))
const hasReminders = computed(() => features.has('reminders'))
const hasLater = computed(() => hasReminders.value || features.has('messages.scheduled'))
const enabled = computed(() => hasSaved.value || hasLater.value)
const tabs = computed(() => [
  ...(hasSaved.value ? [{ id: 'saved' as const, label: 'panel.tabSaved' }] : []),
  ...(hasLater.value ? [{ id: 'later' as const, label: 'panel.tabLater' }] : [])
])
const current = computed<PanelTab>(() => tabs.value.some(x => x.id === tab.value) ? tab.value : tabs.value[0]?.id ?? 'saved')
const tooltip = computed(() => hasSaved.value && hasLater.value ? 'panel.tooltip' : hasSaved.value ? 'panel.tooltipSaved' : 'panel.tooltipLater')

const showHint = computed(() =>
  saved.hintPending && !open.value && !!onboarding.state && !onboarding.state.dismissed_tips.includes(SAVED_HINT_TIP)
)
function dismissHint() {
  saved.hintPending = false
  void onboarding.dismiss(SAVED_HINT_TIP)
}

useDismissable(root, () => { open.value = false }, { active: open })

function refreshLater() {
  void later.load({ reminders: hasReminders.value, scheduled: features.has('messages.scheduled') })
}

// The badge and the saved marks need the lists before the panel is first opened.
onMounted(() => {
  void onboarding.ensureLoaded()
  void features.load().then(() => {
    if (hasSaved.value) void saved.ensureLoaded()
    if (hasLater.value) refreshLater()
  })
})

watch(open, isOpen => {
  if (!isOpen) { focusId.value = undefined; return }
  if (saved.hintPending) dismissHint()
  if (hasLater.value) refreshLater()
})

// The toast after saving, a due reminder: open on the tab and entry it names.
watch(pendingPanel, request => {
  if (!request || !enabled.value) return
  pendingPanel.value = null
  tab.value = request.tab
  focusId.value = request.messageId
  open.value = true
}, { immediate: true })

async function onTabKey(e: KeyboardEvent) {
  const ids = tabs.value.map(x => x.id)
  const at = ids.indexOf(current.value)
  const step = e.key === 'ArrowRight' ? 1 : e.key === 'ArrowLeft' ? -1 : 0
  if (!step || ids.length < 2) return
  e.preventDefault()
  const next = ids[(at + step + ids.length) % ids.length]
  if (!next) return
  tab.value = next
  await nextTick()
  root.value?.querySelector<HTMLElement>(`#panel-tab-${next}`)?.focus()
}
</script>

<template>
  <div v-if="enabled" ref="root" class="relative">
    <button
      v-if="!bare"
      type="button"
      data-testid="saved-button"
      class="btn btn-icon relative max-md:h-11 max-md:w-11"
      :class="open ? 'bg-mnema-surface text-mnema-accent' : ''"
      :aria-label="$t(tooltip)"
      :aria-expanded="open ? 'true' : 'false'"
      aria-haspopup="dialog"
      v-tooltip="$t(tooltip)"
      @click="open = !open"
    >
      <Bookmark class="h-5 w-5" />
      <Badge v-if="later.attention && hasLater" variant="info" data-testid="later-badge" class="absolute -right-1 -top-1 min-w-4 px-1 py-0 text-[10px] leading-4">{{ later.attention }}</Badge>
    </button>

    <div
      v-if="showHint"
      role="status"
      data-testid="saved-hint"
      class="absolute right-0 top-full z-30 mt-2 flex w-[min(18rem,calc(100vw-1.5rem))] items-start gap-2 rounded-xl border border-mnema-border bg-mnema-elevated p-3 text-sm shadow-popover pop-in max-md:fixed max-md:inset-x-3 max-md:top-[calc(3rem+env(safe-area-inset-top))] max-md:mt-0 max-md:w-auto"
    >
      <Bookmark class="mt-0.5 h-4 w-4 shrink-0 text-mnema-accent" aria-hidden="true" />
      <p class="min-w-0 flex-1 text-mnema-muted">
        <span class="block font-medium text-mnema-text">{{ $t('saved.hintTitle') }}</span>
        {{ $t('saved.hintBody') }}
      </p>
      <button type="button" data-testid="saved-hint-dismiss" class="btn btn-icon h-7 w-7 shrink-0 max-md:h-11 max-md:w-11" :aria-label="$t('saved.hintDismiss')" v-tooltip="$t('saved.hintDismiss')" @click="dismissHint">
        <X class="h-4 w-4" />
      </button>
    </div>

    <HeaderPopover v-if="open" data-testid="saved-panel" :title="$t('panel.title')">
      <div v-if="tabs.length > 1" role="tablist" :aria-label="$t('panel.title')" class="mb-1 flex gap-1 px-1" @keydown="onTabKey">
        <button
          v-for="item in tabs"
          :id="`panel-tab-${item.id}`"
          :key="item.id"
          type="button"
          role="tab"
          :data-testid="`panel-tab-${item.id}`"
          :aria-selected="current === item.id ? 'true' : 'false'"
          :aria-controls="`panel-body-${item.id}`"
          :tabindex="current === item.id ? 0 : -1"
          :class="['flex items-center gap-1.5 rounded-md px-3 py-1 text-sm transition max-md:py-2.5', current === item.id ? 'bg-mnema-surface font-medium text-mnema-text' : 'text-mnema-tertiary hover:text-mnema-text']"
          @click="tab = item.id"
        >
          {{ $t(item.label) }}
          <Badge v-if="item.id === 'later' && later.attention" variant="info" class="min-w-4 px-1 py-0 text-[10px] leading-4">{{ later.attention }}</Badge>
        </button>
      </div>
      <p class="px-3 pb-1.5 text-xs text-mnema-tertiary" data-testid="panel-description">{{ $t(current === 'saved' ? 'panel.descSaved' : 'panel.descLater') }}</p>

      <div :id="`panel-body-${current}`" :role="tabs.length > 1 ? 'tabpanel' : undefined" :aria-labelledby="tabs.length > 1 ? `panel-tab-${current}` : undefined">
        <SavedList v-if="current === 'saved'" :focus-id="focusId" @navigate="open = false" />
        <LaterList v-else @navigate="open = false" @retry="refreshLater" />
      </div>
    </HeaderPopover>
  </div>
</template>
