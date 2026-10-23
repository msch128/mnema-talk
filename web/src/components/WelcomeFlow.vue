<script setup lang="ts">
// First run: welcome text, rules, profile and notifications, one step at a
// time. The rules step blocks until they are accepted and has no way out;
// everything else can be skipped. Mounted once in App.vue; it decides itself
// whether anything is due (see useWelcomeFlow).
import { Bell, X } from '@lucide/vue'
import { useWelcomeFlow } from '../composables/useWelcomeFlow'
import { useChatStore } from '../stores/chat'
import { useOnboardingStore } from '../stores/onboarding'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'
import MarkdownContent from './MarkdownContent.vue'
import WelcomeProfileStep from './WelcomeProfileStep.vue'

const { step, stepNumber, total, open, canClose, accepting, next, close, accept } = useWelcomeFlow()
const onboarding = useOnboardingStore()
const chat = useChatStore()
const toasts = useToastStore()

async function enableNotifications() {
  const result = await chat.requestNotificationPermission()
  if (result === 'granted') toasts.success(t('notifications.granted'))
  else if (result === 'denied') toasts.error(t('notifications.denied'))
  next()
}

const primary = 'btn btn-primary'
const quiet = 'btn btn-ghost'
</script>

<template>
  <BaseDialog v-if="open && onboarding.state" panel-class="max-w-lg" :close-on-scrim="false" @close="close">
    <template #default="{ titleId }">
      <header class="flex items-start justify-between gap-3 border-b border-mnema-hairline bg-mnema-raised px-5 py-4">
        <div class="min-w-0">
          <p class="font-mono text-xs text-mnema-tertiary" data-testid="welcome-progress">{{ $t('welcome.progress', { current: stepNumber, total }) }}</p>
          <h2 :id="titleId" class="text-lg font-semibold text-mnema-text" data-testid="welcome-title">{{ $t(`welcome.${step}.title`, { name: onboarding.state.community_name || 'Mnema Talk' }) }}</h2>
        </div>
        <button v-if="canClose" type="button" data-dialog-close class="btn btn-icon shrink-0" :aria-label="$t('welcome.close')" @click="close">
          <X class="h-[18px] w-[18px]" />
        </button>
      </header>

      <div class="min-h-0 flex-1 overflow-y-auto px-5 py-4 text-sm text-mnema-muted">
        <div v-if="step === 'welcome'" data-testid="welcome-text">
          <MarkdownContent v-if="onboarding.state.welcome_text" :content="onboarding.state.welcome_text" />
          <p v-else>{{ $t('welcome.welcome.fallback') }}</p>
        </div>

        <div v-else-if="step === 'rules'" data-testid="welcome-rules">
          <p class="mb-3">{{ $t('welcome.rules.intro') }}</p>
          <div class="max-h-72 overflow-y-auto rounded-md border border-mnema-hairline bg-mnema-canvas/40 p-3"><MarkdownContent :content="onboarding.state.rules_text" /></div>
        </div>

        <WelcomeProfileStep v-else-if="step === 'profile'" @done="next" @skip="next" />

        <div v-else-if="step === 'notifications'" class="flex items-start gap-3" data-testid="welcome-notifications">
          <Bell class="mt-0.5 h-5 w-5 shrink-0 text-mnema-accent" />
          <p>{{ $t('welcome.notifications.body') }}</p>
        </div>
      </div>

      <footer v-if="step !== 'profile'" class="flex justify-end gap-2 border-t border-mnema-hairline px-5 py-3">
        <template v-if="step === 'welcome'">
          <button type="button" :class="primary" data-testid="welcome-next" @click="next">{{ $t('welcome.continue') }}</button>
        </template>
        <template v-else-if="step === 'rules'">
          <button type="button" :class="primary" :disabled="accepting" data-testid="welcome-accept" @click="accept">{{ $t('welcome.rules.accept') }}</button>
        </template>
        <template v-else-if="step === 'notifications'">
          <button type="button" :class="quiet" data-testid="welcome-skip" @click="next">{{ $t('notifications.notNow') }}</button>
          <button type="button" :class="primary" data-testid="welcome-enable" @click="enableNotifications">{{ $t('notifications.enable') }}</button>
        </template>
      </footer>
    </template>
  </BaseDialog>
</template>
