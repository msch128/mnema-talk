<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { 
  X, Crown, Shield, User, Calendar, Volume2, 
  Camera, AtSign, Check, Loader2, AlertCircle,
  Edit3, Save
} from 'lucide-vue-next'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'

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

// Bio editing state
const isEditingBio = ref(false)
const editDisplayName = ref('')
const editBio = ref('')
const isSavingProfile = ref(false)
const profileSaveSuccess = ref(false)
const profileSaveError = ref('')

// Determine the active user object
const profileUser = computed(() => {
  return props.user || chatStore.selectedUserProfile || authStore.user || {}
})

// Initialize edit fields
editDisplayName.value = profileUser.value.display_name || ''
editBio.value = profileUser.value.bio || ''

async function saveProfile() {
  isSavingProfile.value = true
  profileSaveError.value = ''
  profileSaveSuccess.value = false
  try {
    const updated = await authStore.updateProfile({
      displayName: editDisplayName.value,
      bio: editBio.value
    })
    if (chatStore.selectedUserProfile) {
      chatStore.selectedUserProfile = { ...chatStore.selectedUserProfile, ...updated }
    }
    profileSaveSuccess.value = true
    isEditingBio.value = false
    setTimeout(() => {
      profileSaveSuccess.value = false
    }, 2500)
  } catch (err) {
    profileSaveError.value = err.message || 'Speichern fehlgeschlagen'
  } finally {
    isSavingProfile.value = false
  }
}

// Password change state (own profile only)
const isChangingPassword = ref(false)
const currentPassword = ref('')
const newPassword = ref('')
const newPasswordRepeat = ref('')
const isSavingPassword = ref(false)
const passwordError = ref('')
const passwordSuccess = ref(false)

async function savePassword() {
  passwordError.value = ''
  if (newPassword.value.length < 10) {
    passwordError.value = 'Das neue Passwort muss mindestens 10 Zeichen haben'
    return
  }
  if (newPassword.value !== newPasswordRepeat.value) {
    passwordError.value = 'Die Passwörter stimmen nicht überein'
    return
  }
  isSavingPassword.value = true
  try {
    await authStore.changePassword(currentPassword.value, newPassword.value)
    currentPassword.value = newPassword.value = newPasswordRepeat.value = ''
    isChangingPassword.value = false
    passwordSuccess.value = true
    setTimeout(() => { passwordSuccess.value = false }, 2500)
  } catch (err) {
    passwordError.value = err.message
  } finally {
    isSavingPassword.value = false
  }
}

const isSelf = computed(() => {
  return authStore.user && profileUser.value.id === authStore.user.id
})

const isOnline = computed(() => {
  return chatStore.onlineUserIds.has(profileUser.value.id)
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
    class="fixed inset-0 bg-black/80 z-50 flex items-center justify-center p-4 select-none"
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
                <span class="text-xs font-bold tracking-wider uppercase">Ändern</span>
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
              class="px-3 py-1.5 rounded-lg bg-mnema-band hover:bg-mnema-raised text-mnema-text text-sm font-medium border border-mnema-border flex items-center gap-1.5 transition active:scale-95 shadow-sm"
              title="Nutzer im Chat erwähnen"
            >
              <AtSign class="w-4 h-4 text-mnema-accent" />
              <span>Erwähnen</span>
            </button>

            <button 
              v-if="isSelf"
              @click="triggerAvatarUpload"
              :disabled="isUploading"
              class="px-3 py-1.5 rounded-lg bg-mnema-accent/15 hover:bg-mnema-accent/25 text-mnema-accent text-sm font-medium border border-mnema-accent/30 flex items-center gap-1.5 transition active:scale-95 disabled:opacity-50"
            >
              <Camera class="w-4 h-4" />
              <span>Avatar ändern</span>
            </button>
          </div>
        </div>

        <!-- Name & Badges -->
        <div class="bg-mnema-canvas/70 border border-mnema-hairline rounded-xl p-3.5 shadow-inner">
          <div class="flex items-center justify-between">
            <div>
              <h2 class="text-lg font-bold text-mnema-text leading-tight flex items-center gap-1.5">
                {{ profileUser.display_name || profileUser.username }}
                <Crown 
                  v-if="profileUser.role === 'admin'" 
                  class="w-4 h-4 text-amber-400 inline-block drop-shadow-[0_0_8px_rgba(251,191,36,0.5)]" 
                  title="Server-Inhaber / Administrator"
                />
              </h2>
              <p class="text-sm text-mnema-tertiary font-mono">@{{ profileUser.username }}</p>
            </div>

            <!-- Role Badge -->
            <div 
              :class="[
                'px-2 py-0.5 rounded-md text-xs font-semibold border flex items-center gap-1 uppercase tracking-wider',
                profileUser.role === 'admin'
                  ? 'bg-amber-500/10 text-amber-400 border-amber-500/30'
                  : 'bg-mnema-band text-mnema-muted border-mnema-border'
              ]"
            >
              <Shield v-if="profileUser.role === 'admin'" class="w-3.5 h-3.5" />
              <User v-else class="w-3.5 h-3.5" />
              <span>{{ profileUser.role === 'admin' ? 'Admin' : 'Mitglied' }}</span>
            </div>
          </div>

          <!-- Feedback Banners -->
          <div v-if="uploadSuccess" class="mt-2.5 p-2 rounded-lg bg-emerald-500/15 border border-emerald-500/30 text-emerald-400 text-sm flex items-center gap-2">
            <Check class="w-4 h-4 flex-shrink-0" />
            <span>Neuer Avatar wurde erfolgreich gespeichert!</span>
          </div>

          <div v-if="uploadError" class="mt-2.5 p-2 rounded-lg bg-red-500/15 border border-red-500/30 text-red-400 text-sm flex items-center gap-2">
            <AlertCircle class="w-4 h-4 flex-shrink-0" />
            <span>{{ uploadError }}</span>
          </div>

          <!-- Divider -->
          <div class="h-px bg-mnema-hairline my-3"></div>

          <!-- Über mich (Bio) Section -->
          <div class="mb-3">
            <div class="flex items-center justify-between mb-1">
              <span class="text-xs font-bold uppercase tracking-wider text-mnema-tertiary">Über mich</span>
              <button 
                v-if="isSelf && !isEditingBio" 
                @click="isEditingBio = true"
                class="text-xs text-mnema-accent hover:underline flex items-center gap-1"
              >
                <Edit3 class="w-3.5 h-3.5" />
                <span>Bearbeiten</span>
              </button>
            </div>

            <!-- View Mode -->
            <div v-if="!isEditingBio">
              <p v-if="profileUser.bio" class="text-sm text-mnema-text leading-relaxed whitespace-pre-wrap bg-mnema-band/40 p-2.5 rounded-lg border border-mnema-hairline">
                {{ profileUser.bio }}
              </p>
              <p v-else class="text-sm text-mnema-tertiary italic bg-mnema-band/20 p-2.5 rounded-lg border border-mnema-hairline">
                Keine Biografie hinterlegt.
              </p>
            </div>

            <!-- Edit Mode (Own Profile) -->
            <div v-else class="space-y-2 mt-1">
              <div>
                <label class="text-xs text-mnema-tertiary block mb-0.5">Anzeigename</label>
                <input 
                  v-model="editDisplayName" 
                  type="text" 
                  maxlength="64"
                  class="w-full text-sm px-2.5 py-1.5 rounded-lg bg-mnema-canvas border border-mnema-border text-mnema-text focus:outline-none focus:border-mnema-accent"
                />
              </div>
              <div>
                <label class="text-xs text-mnema-tertiary block mb-0.5">Biografie (max. 250 Zeichen)</label>
                <textarea 
                  v-model="editBio" 
                  rows="3" 
                  maxlength="250"
                  class="w-full text-sm px-2.5 py-1.5 rounded-lg bg-mnema-canvas border border-mnema-border text-mnema-text focus:outline-none focus:border-mnema-accent resize-none"
                  placeholder="Erzähle etwas über dich..."
                ></textarea>
                <div class="flex justify-between items-center text-xs text-mnema-tertiary mt-0.5">
                  <span>{{ editBio.length }} / 250</span>
                  <div class="flex items-center gap-1.5">
                    <button 
                      @click="isEditingBio = false" 
                      class="px-2 py-0.5 rounded text-mnema-muted hover:text-mnema-text"
                    >
                      Abbrechen
                    </button>
                    <button 
                      @click="saveProfile" 
                      :disabled="isSavingProfile"
                      class="px-2.5 py-1 rounded bg-mnema-accent text-mnema-canvas font-bold flex items-center gap-1 hover:brightness-110 active:scale-95 disabled:opacity-50"
                    >
                      <Loader2 v-if="isSavingProfile" class="w-3.5 h-3.5 animate-spin" />
                      <Save v-else class="w-3.5 h-3.5" />
                      <span>Speichern</span>
                    </button>
                  </div>
                </div>
              </div>
            </div>

            <div v-if="profileSaveSuccess" class="mt-2 p-1.5 rounded bg-emerald-500/15 border border-emerald-500/30 text-emerald-400 text-sm flex items-center gap-1.5">
              <Check class="w-4 h-4" />
              <span>Profil aktualisiert!</span>
            </div>
            <div v-if="profileSaveError" class="mt-2 p-1.5 rounded bg-red-500/15 border border-red-500/30 text-red-400 text-sm flex items-center gap-1.5">
              <AlertCircle class="w-4 h-4" />
              <span>{{ profileSaveError }}</span>
            </div>
          </div>

          <!-- Password Section (Own Profile) -->
          <div v-if="isSelf" class="mb-3">
            <div class="flex items-center justify-between mb-1">
              <span class="text-xs font-bold uppercase tracking-wider text-mnema-tertiary">Passwort</span>
              <button
                v-if="!isChangingPassword"
                @click="isChangingPassword = true; passwordError = ''"
                class="text-xs text-mnema-accent hover:underline flex items-center gap-1"
              >
                <Edit3 class="w-3.5 h-3.5" />
                <span>Ändern</span>
              </button>
            </div>

            <form v-if="isChangingPassword" class="space-y-2 mt-1" @submit.prevent="savePassword">
              <input
                v-model="currentPassword"
                type="password"
                autocomplete="current-password"
                placeholder="Aktuelles Passwort"
                class="w-full text-sm px-2.5 py-1.5 rounded-lg bg-mnema-canvas border border-mnema-border text-mnema-text focus:outline-none focus:border-mnema-accent"
              />
              <input
                v-model="newPassword"
                type="password"
                autocomplete="new-password"
                placeholder="Neues Passwort (min. 10 Zeichen)"
                class="w-full text-sm px-2.5 py-1.5 rounded-lg bg-mnema-canvas border border-mnema-border text-mnema-text focus:outline-none focus:border-mnema-accent"
              />
              <input
                v-model="newPasswordRepeat"
                type="password"
                autocomplete="new-password"
                placeholder="Neues Passwort wiederholen"
                class="w-full text-sm px-2.5 py-1.5 rounded-lg bg-mnema-canvas border border-mnema-border text-mnema-text focus:outline-none focus:border-mnema-accent"
              />
              <div class="flex justify-end items-center gap-1.5 text-xs">
                <button
                  type="button"
                  @click="isChangingPassword = false; currentPassword = newPassword = newPasswordRepeat = ''"
                  class="px-2 py-0.5 rounded text-mnema-muted hover:text-mnema-text"
                >
                  Abbrechen
                </button>
                <button
                  type="submit"
                  :disabled="isSavingPassword"
                  class="px-2.5 py-1 rounded bg-mnema-accent text-mnema-canvas font-bold flex items-center gap-1 hover:brightness-110 active:scale-95 disabled:opacity-50"
                >
                  <Loader2 v-if="isSavingPassword" class="w-3.5 h-3.5 animate-spin" />
                  <Save v-else class="w-3.5 h-3.5" />
                  <span>Speichern</span>
                </button>
              </div>
            </form>

            <div v-if="passwordSuccess" class="mt-2 p-1.5 rounded bg-emerald-500/15 border border-emerald-500/30 text-emerald-400 text-sm flex items-center gap-1.5">
              <Check class="w-4 h-4" />
              <span>Passwort geändert!</span>
            </div>
            <div v-if="passwordError" class="mt-2 p-1.5 rounded bg-red-500/15 border border-red-500/30 text-red-400 text-sm flex items-center gap-1.5">
              <AlertCircle class="w-4 h-4" />
              <span>{{ passwordError }}</span>
            </div>
          </div>

          <!-- Divider -->
          <div class="h-px bg-mnema-hairline my-3"></div>

          <!-- Voice Activity Status -->
          <div class="mb-3">
            <div class="text-xs font-bold uppercase tracking-wider text-mnema-tertiary mb-1">Aktivität</div>
            <div 
              v-if="voiceHangout" 
              class="flex items-center gap-2 p-2 rounded-lg bg-mnema-accent/10 border border-mnema-accent/25 text-mnema-accent text-sm"
            >
              <Volume2 class="w-4 h-4 flex-shrink-0" />
              <div class="truncate">
                <span class="font-medium">Im Sprachkanal:</span>
                <span class="font-bold ml-1 text-mnema-text">#{{ voiceHangout.name }}</span>
              </div>
            </div>
            <div 
              v-else 
              class="flex items-center gap-2 p-2 rounded-lg bg-mnema-raised/60 border border-mnema-hairline text-mnema-muted text-sm"
            >
              <span class="w-2 h-2 rounded-full bg-mnema-tertiary"></span>
              <span>Aktuell in keinem Sprachkanal</span>
            </div>
          </div>

          <!-- Details / Metadata Grid -->
          <div>
            <div class="text-xs font-bold uppercase tracking-wider text-mnema-tertiary mb-1">Mitgliedschaft</div>
            <div class="flex items-center gap-2 text-sm text-mnema-muted">
              <Calendar class="w-4 h-4 text-mnema-tertiary" />
              <span>Mnema-Talk Mitglied seit <strong class="text-mnema-text font-medium">{{ memberSinceFormatted }}</strong></span>
            </div>
          </div>
        </div>

        <!-- Hint Footer -->
        <div v-if="isSelf" class="mt-3 text-center">
          <p class="text-xs text-mnema-tertiary">
            Tipp: Klicke auf dein Profilbild, um ein neues Avatar-Bild (PNG, JPG, WEBP, GIF) hochzuladen.
          </p>
        </div>
      </div>
    </div>
  </div>
</template>
