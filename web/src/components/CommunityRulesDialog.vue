<script setup lang="ts">
// The community rules, read-only: the text the welcome flow asked every member
// to accept, reachable from the server menu at any time afterwards.
import { useOnboardingStore } from '../stores/onboarding'
import { rulesOpen } from '../lib/communityRules'
import BaseDialog from './BaseDialog.vue'
import MarkdownContent from './MarkdownContent.vue'

const onboarding = useOnboardingStore()
const close = () => { rulesOpen.value = false }
</script>

<template>
  <BaseDialog :title="$t('rules.title')" panel-class="max-w-lg" @close="close">
    <div class="min-h-0 overflow-y-auto p-5 text-sm text-mnema-muted" data-testid="community-rules">
      <MarkdownContent :content="onboarding.state?.rules_text ?? ''" />
    </div>
  </BaseDialog>
</template>
