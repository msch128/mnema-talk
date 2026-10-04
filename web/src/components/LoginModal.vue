<script setup>
import { ref, onMounted } from 'vue'
import { LogIn, UserPlus } from 'lucide-vue-next'
import { useAuthStore } from '../stores/auth'
import LegalModal from './LegalModal.vue'

const authStore = useAuthStore()

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
        errorMsg.value = 'Einladungscode erforderlich'
        isLoading.value = false
        return
      }
      await authStore.register(username.value, displayName.value || username.value, password.value, inviteCode.value)
    } else {
      await authStore.login(username.value, password.value)
    }
  } catch (err) {
    errorMsg.value = err.message || 'Aktion fehlgeschlagen'
  } finally {
    isLoading.value = false
  }
}
</script>

<template>
  <div class="fixed inset-0 bg-black/80 flex items-center justify-center p-4 z-50">
    <div class="bg-mnema-elevated w-full max-w-sm p-7 rounded-xl shadow-2xl border border-mnema-border flex flex-col space-y-5">
      <!-- Brand & Title -->
      <div class="text-center space-y-2">
        <div class="w-9 h-9 rounded-lg bg-mnema-band border border-mnema-mint/30 flex items-center justify-center text-mnema-mint font-bold text-sm mx-auto shadow-sm">
          M
        </div>
        <div>
          <h1 class="text-base font-bold tracking-tight text-mnema-text">Mnema Talk</h1>
          <p class="text-xs text-mnema-muted mt-0.5">
            {{ isRegister ? 'Private Community beitreten' : 'Anmelden bei Mnema Talk' }}
          </p>
        </div>
      </div>

      <!-- Error Alert -->
      <div v-if="errorMsg" class="bg-mnema-danger/10 border border-mnema-danger/30 text-mnema-danger text-xs p-2.5 rounded-md">
        {{ errorMsg }}
      </div>

      <!-- Auth Form -->
      <form @submit.prevent="handleSubmit" class="space-y-3.5">
        <div>
          <label class="block text-[11px] font-medium text-mnema-muted mb-1 font-mono uppercase tracking-wider">Benutzername</label>
          <input
            v-model="username"
            type="text"
            required
            autocomplete="username"
            placeholder="z.B. herzog"
            class="w-full bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-2 text-xs text-mnema-text placeholder-mnema-tertiary outline-none focus:border-mnema-accent transition"
          />
        </div>

        <div v-if="isRegister">
          <label class="block text-[11px] font-medium text-mnema-muted mb-1 font-mono uppercase tracking-wider">Anzeigename</label>
          <input
            v-model="displayName"
            type="text"
            placeholder="z.B. Herzog"
            class="w-full bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-2 text-xs text-mnema-text placeholder-mnema-tertiary outline-none focus:border-mnema-accent transition"
          />
        </div>

        <div>
          <label class="block text-[11px] font-medium text-mnema-muted mb-1 font-mono uppercase tracking-wider">Passwort</label>
          <input
            v-model="password"
            type="password"
            required
            autocomplete="current-password"
            placeholder="••••••••"
            class="w-full bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-2 text-xs text-mnema-text placeholder-mnema-tertiary outline-none focus:border-mnema-accent transition"
          />
        </div>

        <div v-if="isRegister">
          <label class="block text-[11px] font-medium text-mnema-muted mb-1 font-mono uppercase tracking-wider">Einladungscode</label>
          <input
            v-model="inviteCode"
            type="text"
            required
            placeholder="Einladungscode eingeben"
            class="w-full bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-2 text-xs text-mnema-text placeholder-mnema-tertiary outline-none focus:border-mnema-accent transition font-mono"
          />
        </div>

        <div v-if="isRegister" class="text-[11px] text-mnema-tertiary text-center leading-snug pt-1">
          Mit der Registrierung akzeptierst du die
          <button 
            type="button" 
            @click="showLegalModal = true" 
            class="text-mnema-accent hover:underline font-medium cursor-pointer"
          >
            Datenschutzerklärung & Nutzungsbedingungen
          </button>.
        </div>

        <button
          type="submit"
          :disabled="isLoading"
          class="w-full mt-2 bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold py-2 rounded-md text-xs transition disabled:opacity-50 flex items-center justify-center gap-2 shadow-sm"
        >
          <component :is="isRegister ? UserPlus : LogIn" class="w-3.5 h-3.5" />
          <span>{{ isLoading ? 'Verarbeite...' : (isRegister ? 'Registrieren' : 'Anmelden') }}</span>
        </button>
      </form>

      <!-- Toggle Mode Footer -->
      <div class="text-center pt-2 border-t border-mnema-hairline space-y-2">
        <button
          @click="isRegister = !isRegister; errorMsg = ''"
          type="button"
          class="text-xs text-mnema-muted hover:text-mnema-accent transition cursor-pointer"
        >
          {{ isRegister ? 'Bereits registriert? Hier anmelden' : 'Hast du einen Einladungscode? Hier registrieren' }}
        </button>

        <div>
          <button
            @click="showLegalModal = true"
            type="button"
            class="text-[11px] text-mnema-tertiary hover:text-mnema-text transition underline underline-offset-2 cursor-pointer"
          >
            Datenschutzerklärung & Rechtliches
          </button>
        </div>
      </div>
    </div>

    <!-- Legal & Privacy Policy Modal -->
    <LegalModal v-if="showLegalModal" @close="showLegalModal = false" />
  </div>
</template>
