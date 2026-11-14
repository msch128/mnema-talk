<script setup lang="ts">
// The calm landing card when a vanity link exists but is inactive (/join/{name}).
// Instead of a sign-in dialog, the visitor is told the link is not active and
// offered a way to the regular sign-in at "/".
import { useId } from 'vue'
import { LogIn } from '@lucide/vue'
import { locale, chooseLocale, SUPPORTED } from '../../i18n'

const titleId = useId()
</script>

<template>
  <div class="scrim fixed inset-0 flex justify-center overflow-y-auto p-4 z-50">
    <div
      role="region"
      :aria-labelledby="titleId"
      class="dialog-panel my-auto bg-mnema-elevated w-full max-w-sm p-7 rounded-xl shadow-2xl border border-mnema-border flex flex-col space-y-5 text-center focus:outline-hidden"
      data-testid="invite-inactive"
    >
      <div class="space-y-2">
        <div class="w-9 h-9 rounded-lg bg-mnema-band border border-mnema-mint/30 flex items-center justify-center text-mnema-mint font-bold text-base mx-auto shadow-xs">
          M
        </div>
        <h1 :id="titleId" class="text-lg font-bold tracking-tight text-mnema-text" data-testid="invite-inactive-title">
          {{ $t('login.inviteInactiveTitle') }}
        </h1>
        <p class="text-sm text-mnema-muted leading-relaxed" data-testid="invite-inactive-hint">
          {{ $t('login.inviteInactiveHint') }}
        </p>
      </div>

      <a
        href="/"
        class="btn btn-primary w-full flex items-center justify-center gap-2 cursor-pointer"
        data-testid="invite-inactive-signin"
      >
        <LogIn class="w-3.5 h-3.5" aria-hidden="true" />
        <span>{{ $t('login.inviteInactiveSignIn') }}</span>
      </a>

      <div class="text-center pt-2 border-t border-mnema-hairline">
        <div class="flex items-center justify-center gap-1.5" role="group" :aria-label="$t('account.language')">
          <button
            v-for="l in SUPPORTED"
            :key="l"
            type="button"
            :lang="l"
            :aria-pressed="locale === l ? 'true' : 'false'"
            :class="['px-2 py-0.5 rounded-sm text-xs transition cursor-pointer', locale === l ? 'bg-mnema-accent-subtle text-mnema-accent font-semibold' : 'text-mnema-tertiary hover:text-mnema-text']"
            @click="chooseLocale(l)"
          >{{ $t('language.' + l) }}</button>
        </div>
      </div>
    </div>
  </div>
</template>
