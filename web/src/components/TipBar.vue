<script setup lang="ts">
// One dismissible tip under the channel header (see useOnboardingTips).
import { onBeforeUnmount, onMounted, watch } from 'vue'
import { Lightbulb, X } from '@lucide/vue'
import { useOnboardingTips } from '../composables/useOnboardingTips'
import { searchOpen } from '../lib/globalSearch'
import { useVoiceStore } from '../stores/voice'
import type { TipId } from '../lib/onboardingTips'

const { activeTip, dismissTip } = useOnboardingTips()
const voice = useVoiceStore()

// A tip has done its job once the member does what it says: opens the search,
// types @ or /, joins a Talk. Then it makes room for the next one.
function done(id: TipId) {
  if (activeTip.value === id) dismissTip(id)
}

watch(searchOpen, open => { if (open) done('search') })
watch(() => voice.currentChannelId, id => { if (id) done('talk') })

function onInput(event: Event) {
  const target = event.target
  const typed = event instanceof InputEvent ? event.data : null
  if (target instanceof HTMLElement && target.hasAttribute('data-composer-input') && (typed === '@' || typed === '/')) done('mention')
}
onMounted(() => document.addEventListener('input', onInput))
onBeforeUnmount(() => document.removeEventListener('input', onInput))
</script>

<template>
  <div
    v-if="activeTip"
    class="max-md:hidden px-4 py-2 border-b border-mnema-hairline bg-mnema-raised flex items-center gap-3 text-sm shrink-0"
    role="region"
    :aria-label="$t('tips.label')"
    data-testid="tip-bar"
  >
    <Lightbulb class="w-4 h-4 text-mnema-accent shrink-0" />
    <p class="min-w-0 flex-1 text-mnema-muted">{{ $t(`tips.${activeTip}`) }}</p>
    <button
      type="button"
      class="btn btn-ghost btn-sm shrink-0"
      data-testid="tip-dismiss"
      @click="dismissTip(activeTip)"
    >
      {{ $t('tips.gotIt') }}
    </button>
    <button
      type="button"
      class="btn btn-icon btn-sm hover:text-mnema-text shrink-0"
      :aria-label="$t('common.close')"
      @click="dismissTip(activeTip)"
    >
      <X class="w-4 h-4" />
    </button>
  </div>
</template>
