<script setup>
import { ref, onMounted } from 'vue'
import { LogIn, UserPlus } from 'lucide-vue-next'
import { useAuthStore } from '../stores/auth'

const authStore = useAuthStore()

const isRegister = ref(false)
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
    <div class="bg-discord-dark w-full max-w-md p-8 rounded-xl shadow-2xl border border-discord-darkest flex flex-col space-y-6">
      <div class="text-center space-y-1">
        <h1 class="text-2xl font-black text-white">Mnema Talk</h1>
        <p class="text-xs text-discord-muted">
          {{ isRegister ? 'Tritt der Mnema Talk Community bei' : 'Willkommen zurück!' }}
        </p>
      </div>

      <div v-if="errorMsg" class="bg-discord-red/20 border border-discord-red/50 text-discord-red text-xs p-3 rounded-lg">
        {{ errorMsg }}
      </div>

      <form @submit.prevent="handleSubmit" class="space-y-4">
        <div>
          <label class="block text-xs font-bold text-discord-muted uppercase tracking-wider mb-1.5">Benutzername</label>
          <input
            v-model="username"
            type="text"
            required
            class="w-full bg-discord-darkest border border-discord-light rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-discord-accent transition"
          />
        </div>

        <div v-if="isRegister">
          <label class="block text-xs font-bold text-discord-muted uppercase tracking-wider mb-1.5">Anzeigename (Optional)</label>
          <input
            v-model="displayName"
            type="text"
            placeholder="z. B. Herzog"
            class="w-full bg-discord-darkest border border-discord-light rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-discord-accent transition"
          />
        </div>

        <div>
          <label class="block text-xs font-bold text-discord-muted uppercase tracking-wider mb-1.5">Passwort</label>
          <input
            v-model="password"
            type="password"
            required
            class="w-full bg-discord-darkest border border-discord-light rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-discord-accent transition"
          />
        </div>

        <div v-if="isRegister">
          <label class="block text-xs font-bold text-discord-muted uppercase tracking-wider mb-1.5">Einladungscode</label>
          <input
            v-model="inviteCode"
            type="text"
            required
            placeholder="Einladungslink / Code eingeben"
            class="w-full bg-discord-darkest border border-discord-light rounded-lg px-3 py-2 text-sm text-white outline-none focus:border-discord-accent transition font-mono"
          />
        </div>

        <button
          type="submit"
          :disabled="isLoading"
          class="w-full bg-discord-accent hover:bg-discord-accent/90 text-white font-semibold py-2.5 rounded-lg text-sm transition flex items-center justify-center gap-2 disabled:opacity-50"
        >
          <UserPlus v-if="isRegister" class="w-4 h-4" />
          <LogIn v-else class="w-4 h-4" />
          <span>{{ isLoading ? 'Laden...' : (isRegister ? 'Registrieren' : 'Anmelden') }}</span>
        </button>
      </form>

      <div class="text-center pt-2 border-t border-discord-darkest text-xs text-discord-muted">
        <span v-if="isRegister">
          Bereits einen Account?
          <button @click="isRegister = false; errorMsg = ''" class="text-discord-accent hover:underline font-medium">Anmelden</button>
        </span>
        <span v-else>
          Hast du einen Einladungscode?
          <button @click="isRegister = true; errorMsg = ''" class="text-discord-accent hover:underline font-medium">Mit Einladung registrieren</button>
        </span>
      </div>
    </div>
  </div>
</template>
