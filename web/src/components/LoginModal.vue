<script setup lang="ts">
import { api, caughtErrorMessage } from '../lib/api'
import { decodeAuthAuthOptions } from '../types/rest'
import { computed, ref, onMounted, useId } from 'vue'
import { LogIn, UserPlus } from '@lucide/vue'
import { useAuthStore } from '../stores/auth'
import LegalModal from './LegalModal.vue'
import ForgotPasswordDialog from './ForgotPasswordDialog.vue'
import { MIN_PASSWORD_LENGTH } from '../lib/passwordPolicy'
import SecondFactorStep from './SecondFactorStep.vue'
import PasswordInput from './PasswordInput.vue'
import { useDialog } from '../composables/useDialog'
import { useCommunityName } from '../composables/useCommunityName'
import { communityIconUrl } from '../lib/communityIcon'
import { t, locale, chooseLocale, SUPPORTED } from '../i18n'
import type { InviteContext } from '../lib/inviteLink'
import { normalizeUsername } from '../lib/username'

const props = defineProps<{ invite?: InviteContext }>()

const authStore = useAuthStore()

// The login screen is a dialog that cannot be dismissed: it traps focus but
// has no Escape-to-close.
const panel = ref<HTMLElement | null>(null)
const titleId = useId()
useDialog(panel, { initialFocus: 'input' })

const isRegister = ref(false)
const showLegalModal = ref(false)
// Offered only when the server can send mail (GET /api/auth/options).
const canResetPassword = ref(false)
const optionsLoaded = ref(false)
const showForgot = ref(false)
// Set after a correct password for an account with two-factor sign-in.
const challenge = ref('')
const username = ref('')
const displayName = ref('')
const password = ref('')
const inviteCode = ref('')
const usernameInput = ref<HTMLInputElement | null>(null)
const displayNameInput = ref<HTMLInputElement | null>(null)
const passwordField = ref<{ el: HTMLInputElement | null } | null>(null)
const inviteCodeInput = ref<HTMLInputElement | null>(null)
const errorMsg = ref('')
const isLoading = ref(false)

// Opened from /invite/{code}: the code is filled in and, when the server says
// it works, the heading names the community. A dead code falls back to sign-in.
const communityName = useCommunityName()
// The sign-in options carry the server's own name for the pre-login heading.
const optionsName = ref('')
// The icon is as public as the name: it shows before anyone has signed in.
const optionsIcon = ref('')
const communityIcon = computed(() => communityIconUrl(optionsIcon.value, 128))
const heading = computed(() => (props.invite?.valid && props.invite.communityName) || optionsName.value || communityName.value)
const inviteDead = computed(() => props.invite?.valid === false)
// A vanity link (/join/name) that takes sign-ups signs up with the link itself; one that does not
// only greets the visitor on the sign-in page.
// A refused vanity code (the Owner switched it off meanwhile) falls back to asking for a code.
const vanityRefused = ref(false)
const vanityCode = computed(() => !vanityRefused.value && !!props.invite?.vanity && props.invite.code !== '')
const vanityGreeting = computed(() => !!props.invite?.vanity && props.invite.code === '' && props.invite.valid === true)
const subtitle = computed(() => {
  if (!isRegister.value) return t(vanityGreeting.value ? 'login.subtitleVanity' : 'login.subtitleLogin', { name: heading.value })
  return t(props.invite?.valid ? 'login.subtitleInvited' : 'login.subtitleRegister')
})
const expiry = computed(() => {
  const at = props.invite?.valid ? props.invite.expiresAt : ''
  return at ? new Date(at).toLocaleString(locale.value, { dateStyle: 'medium', timeStyle: 'short' }) : ''
})

onMounted(() => {
  void loadOptions()
  if (props.invite) {
    inviteCode.value = props.invite.code
    isRegister.value = props.invite.valid !== false && !vanityGreeting.value
    return
  }
  // Check if URL has ?invite=...
  const urlParams = new URLSearchParams(window.location.search)
  const hashParams = new URLSearchParams(window.location.hash.split('?')[1])
  const code = urlParams.get('invite') || hashParams.get('invite')
  if (code) {
    inviteCode.value = code
    isRegister.value = true
  }
})

async function loadOptions() {
  try {
    const options = await api('/api/auth/options', { decode: decodeAuthAuthOptions })
    canResetPassword.value = options.password_reset
    optionsName.value = options.community_name?.trim() ?? ''
    optionsIcon.value = options.community_icon ?? ''
    optionsLoaded.value = true
  } catch {
    // Offline or limited: the form works without the link.
  }
}

async function handleSubmit() {
  // Autofill and native accessibility may update input.value without an input
  // event. Snapshot the displayed fields before any reactive/async work, then
  // synchronize the models so a pending render cannot restore older values.
  // Password bytes are preserved exactly; native validation stays authoritative.
  const rawUsername = usernameInput.value?.value ?? ''
  const submittedPassword = passwordField.value?.el?.value ?? ''
  const submittedDisplayName = displayNameInput.value?.value ?? ''
  const submittedInviteCode = inviteCodeInput.value?.value ?? inviteCode.value
  const submittedUsername = isRegister.value ? normalizeUsername(rawUsername.trim()) : rawUsername
  username.value = submittedUsername
  password.value = submittedPassword
  if (isRegister.value) {
    displayName.value = submittedDisplayName
    inviteCode.value = submittedInviteCode
  }
  errorMsg.value = ''
  isLoading.value = true
  try {
    if (isRegister.value) {
      if (!submittedInviteCode.trim()) {
        errorMsg.value = t('login.inviteRequired')
        isLoading.value = false
        return
      }
      if ([...submittedPassword].length < MIN_PASSWORD_LENGTH) {
        errorMsg.value = t('errors.detail.passwordMin', { count: MIN_PASSWORD_LENGTH })
        isLoading.value = false
        return
      }
      await authStore.register(submittedUsername, submittedDisplayName || submittedUsername, submittedPassword, submittedInviteCode)
    } else {
      const outcome = await authStore.login(submittedUsername, submittedPassword)
      if (outcome.kind === 'challenge') {
        challenge.value = outcome.challenge
        password.value = ''
      }
    }
  } catch (err) {
    if (isRegister.value && vanityCode.value) {
      vanityRefused.value = true
      inviteCode.value = ''
    }
    errorMsg.value = caughtErrorMessage(err, t('login.failed'))
  } finally {
    isLoading.value = false
  }
}
</script>

<template>
  <div class="scrim fixed inset-0 flex items-center justify-center p-4 z-50">
    <div
      ref="panel"
      role="dialog"
      aria-modal="true"
      :aria-labelledby="titleId"
      tabindex="-1"
      class="dialog-panel bg-mnema-elevated w-full max-w-sm p-7 rounded-xl shadow-2xl border border-mnema-border flex flex-col space-y-5 focus:outline-hidden"
    >
      <!-- Brand & Title -->
      <div class="text-center space-y-2">
        <img v-if="communityIcon" :src="communityIcon" alt="" class="w-9 h-9 rounded-lg object-cover mx-auto shadow-xs" data-testid="login-icon" />
        <div v-else class="w-9 h-9 rounded-lg bg-mnema-band border border-mnema-mint/30 flex items-center justify-center text-mnema-mint font-bold text-base mx-auto shadow-xs">
          M
        </div>
        <div>
          <h1 :id="titleId" class="text-lg font-bold tracking-tight text-mnema-text" data-testid="login-heading">{{ heading }}</h1>
          <p class="text-sm text-mnema-muted mt-0.5">
            {{ subtitle }}
          </p>
          <p v-if="expiry" class="text-xs text-mnema-tertiary mt-1" data-testid="invite-expiry">{{ $t('login.inviteExpires', { when: expiry }) }}</p>
        </div>
      </div>

      <div v-if="inviteDead" role="status" class="bg-mnema-warning/10 border border-mnema-warning/30 text-mnema-text text-sm p-2.5 rounded-md" data-testid="invite-invalid">
        {{ $t('login.inviteInvalid') }}
      </div>

      <!-- Error Alert -->
      <div v-if="errorMsg" role="alert" class="bg-mnema-danger/10 border border-mnema-danger/30 text-mnema-danger text-sm p-2.5 rounded-md">
        {{ errorMsg }}
      </div>

      <!-- Auth Form -->
      <SecondFactorStep v-if="challenge" :challenge="challenge" @back="challenge = ''" />
      <form v-else @submit.prevent="handleSubmit" class="space-y-3.5">
        <div>
          <label for="login-username" class="block text-sm font-medium text-mnema-body-ink mb-1">{{ $t('login.username') }}</label>
          <input
            id="login-username"
            ref="usernameInput"
            v-model="username"
            type="text"
            required
            autocomplete="username"
            autocapitalize="none"
            spellcheck="false"
            :aria-describedby="isRegister ? 'login-username-hint' : undefined"
            @input="isRegister && (username = normalizeUsername(username))"
            :placeholder="$t('login.usernamePlaceholder')"
            class="w-full bg-mnema-canvas border border-mnema-border-field rounded-lg px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary"
          />
          <p v-if="isRegister" id="login-username-hint" class="mt-1.5 text-xs text-mnema-tertiary" data-testid="username-hint">{{ $t('login.usernameHint') }}</p>
        </div>

        <div v-if="isRegister">
          <label for="login-displayname" class="block text-sm font-medium text-mnema-body-ink mb-1">{{ $t('login.displayName') }}</label>
          <input
            id="login-displayname"
            ref="displayNameInput"
            v-model="displayName"
            type="text"
            :placeholder="$t('login.displayNamePlaceholder')"
            class="w-full bg-mnema-canvas border border-mnema-border-field rounded-lg px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary"
          />
        </div>

        <div>
          <label for="login-password" class="block text-sm font-medium text-mnema-body-ink mb-1">{{ $t('login.password') }}</label>
          <PasswordInput
            id="login-password"
            ref="passwordField"
            v-model="password"
            required
            :autocomplete="isRegister ? 'new-password' : 'current-password'"
            :aria-describedby="isRegister ? 'login-password-hint' : undefined"
            class="w-full"
          />
          <p v-if="isRegister" id="login-password-hint" class="mt-1.5 text-xs text-mnema-tertiary" data-testid="password-hint">{{ $t('login.passwordHint', { count: MIN_PASSWORD_LENGTH }) }}</p>
          <button
            v-if="!isRegister && canResetPassword"
            type="button"
            data-testid="forgot-password-link"
            class="mt-1.5 text-xs text-mnema-tertiary hover:text-mnema-accent transition cursor-pointer"
            @click="showForgot = true"
          >{{ $t('recovery.forgotLink') }}</button>
          <p v-else-if="!isRegister && optionsLoaded" class="mt-1.5 text-xs text-mnema-tertiary" data-testid="forgot-password-hint">{{ $t('login.forgotNoMail') }}</p>
        </div>

        <div v-if="isRegister && !vanityCode">
          <label for="login-invite" class="block text-sm font-medium text-mnema-body-ink mb-1">{{ $t('login.inviteCode') }}</label>
          <input
            id="login-invite"
            ref="inviteCodeInput"
            v-model="inviteCode"
            type="text"
            required
            :placeholder="$t('login.inviteCodePlaceholder')"
            class="w-full bg-mnema-canvas border border-mnema-border-field rounded-lg px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary font-mono"
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
          class="btn btn-primary w-full mt-2 flex items-center justify-center gap-2"
        >
          <component :is="isRegister ? UserPlus : LogIn" class="w-3.5 h-3.5" />
          <span>{{ isLoading ? $t('login.working') : (isRegister ? $t('login.register') : $t('login.signIn')) }}</span>
        </button>
      </form>

      <!-- Toggle Mode Footer -->
      <div class="text-center pt-2 border-t border-mnema-hairline space-y-2">
        <button
          @click="isRegister = !isRegister; errorMsg = ''; challenge = ''"
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
            :class="['px-2 py-0.5 rounded-sm text-xs transition', locale === l ? 'bg-mnema-accent-subtle text-mnema-accent font-semibold' : 'text-mnema-tertiary hover:text-mnema-text']"
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
    <ForgotPasswordDialog v-if="showForgot" @close="showForgot = false" />
  </div>
</template>
