<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { 
  X, Crown, Shield, User, Calendar, Volume2, 
  Camera, AtSign, Check, Loader2, Sparkles, AlertCircle 
} from 'lucide-vue-next'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import UserAvatar from './UserAvatar.vue'

const props = defineProps({
  user: {
    type: Object,
    default: null
  }
})

const emit = defineEmits(['close', 'mention'])

const authStore = useAuthStore()
const chatStore = useChatStore()
const voiceStore = useVoiceStore()

const fileInput = ref(null)
const isUploading = ref(false)
const uploadError = ref('')
const uploadSuccess = ref(false)

// Determine the active user object
const profileUser = computed(() => {
  return props.user || chatStore.selectedUserProfile || authStore.user || {}
})

const isSelf = computed(() => {
  return authStore.user && profileUser.value.id === authStore.user.id
})

const isOnline = computed(() => {
  return chatStore.onlineUserIds.has(profileUser.value.id)
})

const isSpeaking = computed(() => {
  return !!voiceStore.speakingUsers[profileUser.value.id]
})

// Current voice hangout channel of this user
const voiceHangout = computed(() => {
  if (!profileUser.value.id) return null
  for (const [chId, users] of Object.entries(voiceStore.channelUsers || {})) {
    if (users && users[profileUser.value.id]) {
      const allChannels = [
        ...chatStore.categories.flatMap(c => c.channels || []),
        ...chatStore.uncategorized
      ]
      return allChannels.find(c => c.id === chId) || { id: chId, name: 'Talk' }
    }
  }
  return null
})

// Formatted join date
const memberSinceFormatted = computed(() => {
  const dateStr = profileUser.value.created_at
  if (!dateStr) return 'Unbekannt'
  try {
    const d = new Date(dateStr)
    return new Intl.DateTimeFormat('de-DE', {
      day: 'numeric',
      month: 'long',
      year: 'numeric'
    }).format(d)
  } catch {
    return dateStr
  }
})

function triggerAvatarUpload() {
  if (fileInput.value) {
    fileInput.value.click()
  }
}

async function onAvatarSelected(e) {
  const file = e.target.files?.[0]
  if (!file) return

  // Validate size (max 10MB)
  if (file.size > 10 * 1024 * 1024) {
    uploadError.value = 'Bild darf maximal 10 MB groß sein.'
    return
  }

  isUploading.value = true
  uploadError.value = ''
  uploadSuccess.value = false

  try {
    const updated = await authStore.uploadAvatar(file)
    // Update profile view immediately
    if (chatStore.selectedUserProfile) {
      chatStore.selectedUserProfile = { ...chatStore.selectedUserProfile, ...updated }
    }
    uploadSuccess.value = true
    setTimeout(() => {
      uploadSuccess.value = false
    }, 2500)
  } catch (err) {
    uploadError.value = err.message || 'Avatar-Upload fehlgeschlagen.'
  } finally {
    isUploading.value = false
    if (fileInput.value) fileInput.value.value = ''
  }
}

function handleMention() {
  emit('mention', profileUser.value.username)
  emit('close')
}

function handleKeydown(e) {
  if (e.key === 'Escape') {
    emit('close')
  }
}

onMounted(() => {
  window.addEventListener('keydown', handleKeydown)
})

onUnmounted(() => {
  window.removeEventListener('keydown', handleKeydown)
})
</script>

<template>
  <div 
    class="fixed inset-0 bg-black/80 backdrop-blur-sm z-50 flex items-center justify-center p-4 select-none"
    @click.self="emit('close')"
  >
    <div 
      class="bg-mnema-surface w-full max-w-sm rounded-2xl flex flex-col shadow-2xl border border-mnema-border overflow-hidden transform transition-all animate-in fade-in zoom-in-95 duration-150"
    >
      <!-- Top Decorative Banner -->
      <div class="h-28 bg-gradient-to-br from-emerald-950 via-mnema-raised to-emerald-900 border-b border-mnema-hairline relative flex items-start justify-end p-3 overflow-hidden">
        <!-- Subtle background pattern/mesh -->
        <div class="absolute inset-0 opacity-20 bg-[radial-gradient(#10b981_1px,transparent_1px)] [background-size:12px_12px] pointer-events-none"></div>

        <button 
          @click="emit('close')"
          class="relative z-10 w-7 h-7 rounded-full bg-black/50 hover:bg-black/80 text-mnema-muted hover:text-mnema-text flex items-center justify-center transition border border-white/10"
          title="Schließen (Esc)"
        >
          <X class="w-4 h-4" />
        </button>
      </div>

      <!-- Profile Header (Avatar overlap & badges) -->
      <div class="px-5 pb-5 pt-0 relative bg-mnema-surface">
        <div class="flex justify-between items-end -mt-12 mb-3">
          <!-- Large Avatar with overlap -->
          <div class="relative group">
            <div 
              class="w-20 h-20 rounded-full border-4 border-mnema-surface shadow-xl bg-mnema-canvas overflow-hidden flex items-center justify-center relative cursor-pointer"
              @click="isSelf ? triggerAvatarUpload() : null"
            >
              <img 
                v-if="profileUser.avatar_url" 
                :src="profileUser.avatar_url" 
                :alt="profileUser.display_name"
                class="w-full h-full object-cover"
              />
              <span v-else class="text-2xl font-bold text-mnema-mint">
                {{ (profileUser.display_name || profileUser.username || '?').charAt(0).toUpperCase() }}
              </span>

              <!-- Hover overlay for own profile -->
              <div 
                v-if="isSelf"
                class="absolute inset-0 bg-black/60 opacity-0 group-hover:opacity-100 flex flex-col items-center justify-center text-white transition-opacity duration-150"
              >
                <Camera class="w-5 h-5 mb-0.5" />
                <span class="text-[9px] font-bold tracking-wider uppercase">Ändern</span>
              </div>

              <!-- Uploading Spinner -->
              <div 
                v-if="isUploading"
                class="absolute inset-0 bg-black/75 flex flex-col items-center justify-center text-mnema-accent"
              >
                <Loader2 class="w-6 h-6 animate-spin" />
              </div>
            </div>

            <!-- Online / Offline Dot -->
            <span 
              :class="[
                'absolute bottom-1 right-1 w-4 h-4 rounded-full border-2 border-mnema-surface shadow-sm',
                isOnline ? 'bg-mnema-accent' : 'bg-mnema-muted/60'
              ]"
              :title="isOnline ? 'Online' : 'Offline'"
            ></span>

            <!-- Hidden File Input for Avatar Upload -->
            <input 
              v-if="isSelf"
              ref="fileInput"
              type="file"
              accept="image/png,image/jpeg,image/webp,image/gif"
              class="hidden"
              @change="onAvatarSelected"
            />
          </div>

          <!-- Action Buttons in Top Right -->
          <div class="flex items-center gap-2">
            <button 
              v-if="!isSelf"
              @click="handleMention"
              class="px-3 py-1.5 rounded-lg bg-mnema-band hover:bg-mnema-raised text-mnema-text text-xs font-medium border border-mnema-border flex items-center gap-1.5 transition active:scale-95 shadow-sm"
              title="Nutzer im Chat erwähnen"
            >
              <AtSign class="w-3.5 h-3.5 text-mnema-accent" />
              <span>Erwähnen</span>
            </button>

            <button 
              v-if="isSelf"
              @click="triggerAvatarUpload"
              :disabled="isUploading"
              class="px-3 py-1.5 rounded-lg bg-mnema-accent/15 hover:bg-mnema-accent/25 text-mnema-accent text-xs font-medium border border-mnema-accent/30 flex items-center gap-1.5 transition active:scale-95 disabled:opacity-50"
            >
              <Camera class="w-3.5 h-3.5" />
              <span>Avatar ändern</span>
            </button>
          </div>
        </div>

        <!-- Name & Badges -->
        <div class="bg-mnema-canvas/70 border border-mnema-hairline rounded-xl p-3.5 shadow-inner">
          <div class="flex items-center justify-between">
            <div>
              <h2 class="text-base font-bold text-mnema-text leading-tight flex items-center gap-1.5">
                {{ profileUser.display_name || profileUser.username }}
                <Crown 
                  v-if="profileUser.role === 'admin'" 
                  class="w-4 h-4 text-amber-400 inline-block drop-shadow-[0_0_8px_rgba(251,191,36,0.5)]" 
                  title="Server-Inhaber / Administrator"
                />
              </h2>
              <p class="text-xs text-mnema-tertiary font-mono">@{{ profileUser.username }}</p>
            </div>

            <!-- Role Badge -->
            <div 
              :class="[
                'px-2 py-0.5 rounded-md text-[10px] font-semibold border flex items-center gap-1 uppercase tracking-wider',
                profileUser.role === 'admin'
                  ? 'bg-amber-500/10 text-amber-400 border-amber-500/30'
                  : 'bg-mnema-band text-mnema-muted border-mnema-border'
              ]"
            >
              <Shield v-if="profileUser.role === 'admin'" class="w-3 h-3" />
              <User v-else class="w-3 h-3" />
              <span>{{ profileUser.role === 'admin' ? 'Admin' : 'Mitglied' }}</span>
            </div>
          </div>

          <!-- Feedback Banners -->
          <div v-if="uploadSuccess" class="mt-2.5 p-2 rounded-lg bg-emerald-500/15 border border-emerald-500/30 text-emerald-400 text-xs flex items-center gap-2">
            <Check class="w-4 h-4 flex-shrink-0" />
            <span>Neuer Avatar wurde erfolgreich gespeichert!</span>
          </div>

          <div v-if="uploadError" class="mt-2.5 p-2 rounded-lg bg-red-500/15 border border-red-500/30 text-red-400 text-xs flex items-center gap-2">
            <AlertCircle class="w-4 h-4 flex-shrink-0" />
            <span>{{ uploadError }}</span>
          </div>

          <!-- Divider -->
          <div class="h-px bg-mnema-hairline my-3"></div>

          <!-- Voice Activity Status -->
          <div class="mb-3">
            <div class="text-[10px] font-bold uppercase tracking-wider text-mnema-tertiary mb-1">Aktivität</div>
            <div 
              v-if="voiceHangout" 
              class="flex items-center gap-2 p-2 rounded-lg bg-mnema-accent/10 border border-mnema-accent/25 text-mnema-accent text-xs"
            >
              <Volume2 class="w-4 h-4 animate-pulse flex-shrink-0" />
              <div class="truncate">
                <span class="font-medium">Im Sprachkanal:</span>
                <span class="font-bold ml-1 text-mnema-text">#{{ voiceHangout.name }}</span>
              </div>
            </div>
            <div 
              v-else 
              class="flex items-center gap-2 p-2 rounded-lg bg-mnema-raised/60 border border-mnema-hairline text-mnema-muted text-xs"
            >
              <span class="w-2 h-2 rounded-full bg-mnema-tertiary"></span>
              <span>Aktuell in keinem Sprachkanal</span>
            </div>
          </div>

          <!-- Details / Metadata Grid -->
          <div>
            <div class="text-[10px] font-bold uppercase tracking-wider text-mnema-tertiary mb-1">Mitgliedschaft</div>
            <div class="flex items-center gap-2 text-xs text-mnema-muted">
              <Calendar class="w-3.5 h-3.5 text-mnema-tertiary" />
              <span>Mnema-Talk Mitglied seit <strong class="text-mnema-text font-medium">{{ memberSinceFormatted }}</strong></span>
            </div>
          </div>
        </div>

        <!-- Hint Footer -->
        <div v-if="isSelf" class="mt-3 text-center">
          <p class="text-[10px] text-mnema-tertiary">
            Tipp: Klicke auf dein Profilbild, um ein neues Avatar-Bild (PNG, JPG, WEBP, GIF) hochzuladen.
          </p>
        </div>
      </div>
    </div>
  </div>
</template>
