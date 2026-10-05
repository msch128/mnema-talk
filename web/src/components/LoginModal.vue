<script setup>
import { ref, onMounted, useId } from 'vue'
import { LogIn, UserPlus } from '@lucide/vue'
import { useAuthStore } from '../stores/auth'
import LegalModal from './LegalModal.vue'
import { useDialog } from '../composables/useDialog'
import { t, locale, chooseLocale, SUPPORTED } from '../i18n'

const authStore = useAuthStore()

// The login screen is a dialog that cannot be dismissed: it traps focus but
// has no Escape-to-close.
const panel = ref(null)
const titleId = useId()
useDialog(panel, { initialFocus: 'input' })

const isRegister = ref(false)
const showLegalModal = ref(false)
const username = ref('')
const displayName = ref('')
const password = ref('')
const inviteCode = ref('')
const errorMsg = ref('')
const isLoading = ref(false)

onMounted(() => {
  // Check if URL has ?invite=...
  const urlParams = new URLSearchParams(window.location.search)
  const hashParams = new URLSearchParams(window.location.hash.split('?')[1])
  const code = urlParams.get('invite') || hashParams.get('invite')
  if (code) {
    inviteCode.value = code
    isRegister.value = true
  }
})

async function handleSubmit() {
  errorMsg.value = ''
  isLoading.value = true
  try {
    if (isRegister.value) {
      if (!inviteCode.value.trim()) {
        errorMsg.value = t('login.inviteRequired')
        isLoading.value = false
        return
      }
      await authStore.register(username.value, displayName.value || username.value, password.value, inviteCode.value)
    } else {
      await authStore.login(username.value, password.value)
    }
  } catch (err) {
    errorMsg.value = err.message || t('login.failed')
  } finally {
    isLoading.value = false
  }
}
</script>

<template>
  <div class="fixed inset-0 bg-black/60 flex items-center justify-center p-4 z-50">
    <div
      ref="panel"
      role="dialog"
      aria-modal="true"
      :aria-labelledby="titleId"
      tabindex="-1"
      class="bg-mnema-elevated w-full max-w-sm p-7 rounded-[14px] shadow-2xl border border-mnema-border flex flex-col space-y-5 focus:outline-none"
    >
      <!-- Brand & Title -->
      <div class="text-center space-y-2">
        <div class="w-9 h-9 rounded-lg bg-mnema-band border border-mnema-mint/30 flex items-center justify-center text-mnema-mint font-bold text-base mx-auto shadow-sm">
          M
        </div>
        <div>
          <h1 :id="titleId" class="text-lg font-bold tracking-tight text-mnema-text">Mnema Talk</h1>
          <p class="text-sm text-mnema-muted mt-0.5">
            {{ isRegister ? $t('login.subtitleRegister') : $t('login.subtitleLogin') }}
          </p>
        </div>
      </div>

      <!-- Error Alert -->
      <div v-if="errorMsg" role="alert" class="bg-mnema-danger/10 border border-mnema-danger/30 text-mnema-danger text-sm p-2.5 rounded-md">
        {{ errorMsg }}
      </div>

      <!-- Auth Form -->
      <form @submit.prevent="handleSubmit" class="space-y-3.5">
        <div>
          <label for="login-username" class="block text-xs font-medium text-mnema-muted mb-1 font-mono uppercase tracking-wider">{{ $t('login.username') }}</label>
          <input
            id="login-username"
            v-model="username"
            type="text"
            required
            autocomplete="username"
            :placeholder="$t('login.usernamePlaceholder')"
            class="w-full bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary outline-none focus:border-mnema-accent transition"
          />
        </div>

        <div v-if="isRegister">
          <label for="login-displayname" class="block text-xs font-medium text-mnema-muted mb-1 font-mono uppercase tracking-wider">{{ $t('login.displayName') }}</label>
          <input
            id="login-displayname"
            v-model="displayName"
            type="text"
            :placeholder="$t('login.displayNamePlaceholder')"
            class="w-full bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary outline-none focus:border-mnema-accent transition"
          />
        </div>

        <div>
          <label for="login-password" class="block text-xs font-medium text-mnema-muted mb-1 font-mono uppercase tracking-wider">{{ $t('login.password') }}</label>
          <input
            id="login-password"
            v-model="password"
            type="password"
            required
            autocomplete="current-password"
            placeholder="••••••••"
            class="w-full bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary outline-none focus:border-mnema-accent transition"
          />
        </div>

        <div v-if="isRegister">
          <label for="login-invite" class="block text-xs font-medium text-mnema-muted mb-1 font-mono uppercase tracking-wider">{{ $t('login.inviteCode') }}</label>
          <input
            id="login-invite"
            v-model="inviteCode"
            type="text"
            required
            :placeholder="$t('login.inviteCodePlaceholder')"
            class="w-full bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary outline-none focus:border-mnema-accent transition font-mono"
          />
        </div>

        <div v-if="isRegister" class="text-xs text-mnema-tertiary text-center leading-snug pt-1">
          {{ $t('login.acceptPrefix') }}
          <button 
            type="button" 
            @click="showLegalModal = true" 
            class="text-mnema-accent hover:underline font-medium cursor-pointer"
          >
            {{ $t('login.acceptLink') }}
          </button>.
        </div>

        <button
          type="submit"
          :disabled="isLoading"
          class="w-full mt-2 bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold py-2 rounded-md text-sm transition disabled:opacity-50 flex items-center justify-center gap-2 shadow-sm"
        >
          <component :is="isRegister ? UserPlus : LogIn" class="w-3.5 h-3.5" />
          <span>{{ isLoading ? $t('login.working') : (isRegister ? $t('login.register') : $t('login.signIn')) }}</span>
        </button>
      </form>

      <!-- Toggle Mode Footer -->
      <div class="text-center pt-2 border-t border-mnema-hairline space-y-2">
        <button
          @click="isRegister = !isRegister; errorMsg = ''"
          type="button"
          class="text-sm text-mnema-muted hover:text-mnema-accent transition cursor-pointer"
        >
          {{ isRegister ? $t('login.toLogin') : $t('login.toRegister') }}
        </button>

        <div class="flex items-center justify-center gap-1.5 pb-1" role="group" :aria-label="$t('account.language')">
          <button
            v-for="l in SUPPORTED"
            :key="l"
            type="button"
            :lang="l"
            :aria-pressed="locale === l ? 'true' : 'false'"
            :class="['px-2 py-0.5 rounded text-xs transition', locale === l ? 'bg-mnema-accent-subtle text-mnema-accent font-semibold' : 'text-mnema-tertiary hover:text-mnema-text']"
            @click="chooseLocale(l)"
          >{{ $t('language.' + l) }}</button>
        </div>

        <div>
          <button
            @click="showLegalModal = true"
            type="button"
            class="text-xs text-mnema-tertiary hover:text-mnema-text transition underline underline-offset-2 cursor-pointer"
          >
            {{ $t('login.legal') }}
          </button>
        </div>
      </div>
    </div>

    <!-- Legal & privacy dialog -->
    <LegalModal v-if="showLegalModal" @close="showLegalModal = false" />
  </div>
</template>
