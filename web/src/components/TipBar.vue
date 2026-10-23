<script setup lang="ts">
// One dismissible tip under the channel header (see useOnboardingTips).
import { Lightbulb, X } from '@lucide/vue'
import { useOnboardingTips } from '../composables/useOnboardingTips'

const { activeTip, dismissTip } = useOnboardingTips()
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
      class="px-2 py-1 rounded-md text-mnema-muted hover:text-mnema-text hover:bg-mnema-hover transition shrink-0"
      data-testid="tip-dismiss"
      @click="dismissTip(activeTip)"
    >
      {{ $t('tips.gotIt') }}
    </button>
    <button
      type="button"
      class="p-1 rounded-md text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-hover transition shrink-0"
      :aria-label="$t('common.close')"
      @click="dismissTip(activeTip)"
    >
      <X class="w-4 h-4" />
    </button>
  </div>
</template>
