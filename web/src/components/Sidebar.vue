<script setup>
import { ref, computed } from 'vue'
import { Hash, Volume2, ShieldCheck, Crown, Radio, RadioTower, Plus, Trash2, MessageCircle } from 'lucide-vue-next'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { useAuthStore } from '../stores/auth'
import { useWebRTC } from '../composables/useWebRTC'
import CreateChannelModal from './CreateChannelModal.vue'
import UserAvatar from './UserAvatar.vue'

const emit = defineEmits(['open-admin', 'open-legal'])

const chatStore = useChatStore()
const voiceStore = useVoiceStore()
const authStore = useAuthStore()
const { joinVoiceChannel } = useWebRTC()

const showCreateChannelModal = ref(false)
const modalChannelType = ref('text')
const modalCategoryId = ref('')

function openCreateChannel(type = 'text', categoryId = '') {
  modalChannelType.value = type
  modalCategoryId.value = categoryId
  showCreateChannelModal.value = true
}

async function handleDeleteChannel(channel) {
  const icon = channel.type === 'voice' ? '🔊' : '#'
  if (!confirm(`Möchtest du den Kanal "${icon} ${channel.name}" wirklich unwiderruflich löschen?`)) return
  try {
    await chatStore.deleteChannel(channel.id)
  } catch (err) {
    alert(err.message || 'Löschen fehlgeschlagen')
  }
}

async function handleDeleteCategory(category) {
  if (!confirm(`Möchtest du die Kategorie "${category.name}" löschen? (Enthaltene Kanäle bleiben erhalten)`)) return
  try {
    await chatStore.deleteCategory(category.id)
  } catch (err) {
    alert(err.message || 'Löschen fehlgeschlagen')
  }
}

// Separate channels into Voice Hangouts and Text Channels
const voiceChannels = computed(() => {
  const list = []
  for (const cat of chatStore.categories) {
    for (const ch of cat.channels || []) {
      if (ch.type === 'voice') list.push({ ...ch, categoryName: cat.name })
    }
  }
  for (const ch of chatStore.uncategorized || []) {
    if (ch.type === 'voice') list.push({ ...ch, categoryName: 'Unkategorisiert' })
  }
  return list
})

const textCategories = computed(() => {
  return chatStore.categories.map(cat => ({
    ...cat,
    channels: (cat.channels || []).filter(c => c.type === 'text')
  }))
})

const uncategorizedText = computed(() => {
  return (chatStore.uncategorized || []).filter(c => c.type === 'text')
})

function handleVoiceClick(channel) {
  // Select channel messages for side-chat
  chatStore.selectChannel(channel)
  // Join voice and switch to voice stage
  joinVoiceChannel(channel.id)
  voiceStore.activeView = 'voice'
}

function handleTextClick(channel) {
  chatStore.selectChannel(channel)
  voiceStore.activeView = 'chat'
}

function handleDMClick(dm) {
  chatStore.selectChannel({
    id: dm.id,
    name: dm.recipient.display_name || dm.recipient.username,
    type: 'dm',
    topic: dm.recipient.bio || '',
    recipient: dm.recipient
  })
  voiceStore.activeView = 'chat'
}
</script>

<template>
  <aside class="w-64 bg-mnema-raised flex flex-col h-full select-none">
    <!-- Server / Workspace Header -->
    <header class="h-14 px-4 border-b border-mnema-hairline flex items-center justify-between">
      <div class="flex items-center gap-2.5 min-w-0">
        <!-- Mnema Forest Mark -->
        <div class="w-7 h-7 rounded-md bg-mnema-band border border-mnema-mint/30 flex items-center justify-center text-mnema-mint font-semibold text-xs shadow-sm flex-shrink-0">
          M
        </div>
        <div class="flex flex-col min-w-0">
          <div class="flex items-center gap-1.5">
            <span class="font-semibold text-xs tracking-tight text-mnema-text truncate">Mnema Talk</span>
            <Crown v-if="authStore.isAdmin" class="w-3.5 h-3.5 text-mnema-amber flex-shrink-0" />
          </div>
          <span class="text-[10px] text-mnema-tertiary font-mono">Private Community</span>
        </div>
      </div>

      <!-- Header Action Buttons -->
      <div class="flex items-center gap-1.5">
        <button 
          @click.stop="emit('open-legal')"
          title="Rechtliches & Datenschutzerklärung (DSGVO)"
          class="p-1 rounded text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition cursor-pointer"
        >
          <ShieldCheck class="w-3.5 h-3.5" />
        </button>

        <!-- Admin Dashboard Button -->
        <button 
          v-if="authStore.isAdmin" 
          @click.stop="emit('open-admin')"
          title="Admin Konsole & S3 Speicher"
          class="text-[11px] font-medium px-2 py-0.5 rounded border border-mnema-accent/40 bg-mnema-accent-subtle text-mnema-accent hover:bg-mnema-accent hover:text-mnema-accent-ink transition cursor-pointer"
        >
          Admin
        </button>
      </div>
    </header>

    <!-- Navigation Scroll Area -->
    <div class="flex-1 overflow-y-auto px-2.5 py-3 space-y-5">
      <!-- 1. Active Voice Room Jump Button (if connected) -->
      <div v-if="voiceStore.isConnected" class="px-1">
        <button
          @click="voiceStore.activeView = 'voice'"
          :class="[
            'w-full flex items-center justify-between p-2 rounded-lg border transition shadow-sm text-left',
            voiceStore.activeView === 'voice'
              ? 'bg-mnema-accent-subtle border-mnema-accent text-mnema-accent'
              : 'bg-mnema-surface border-mnema-hairline text-mnema-text hover:border-mnema-border-strong'
          ]"
        >
          <div class="flex items-center gap-2 min-w-0">
            <span class="w-2 h-2 rounded-full bg-mnema-accent shadow-[0_0_6px_rgba(45,167,113,0.8)] flex-shrink-0"></span>
            <div class="flex flex-col min-w-0">
              <span class="text-[11px] font-semibold truncate">Talk-Bühne öffnen</span>
              <span class="text-[9px] text-mnema-tertiary font-mono">Aktiver Hangout</span>
            </div>
          </div>
          <Volume2 class="w-4 h-4 text-mnema-accent flex-shrink-0" />
        </button>
      </div>

      <!-- Direktnachrichten (DMs) Section -->
      <div class="space-y-1.5">
        <div class="px-2 flex items-center justify-between text-[10px] font-semibold tracking-wider uppercase text-mnema-tertiary font-mono">
          <span class="flex items-center gap-1.5">
            <MessageCircle class="w-3 h-3 text-mnema-accent" />
            <span>Direktnachrichten</span>
          </span>
          <span class="text-[9px] text-mnema-tertiary font-mono">{{ chatStore.dms.length }}</span>
        </div>

        <div v-if="!chatStore.dms.length" class="px-2 py-1 text-[11px] text-mnema-tertiary italic">
          Keine DMs vorhanden.
        </div>

        <div v-else class="space-y-0.5">
          <div
            v-for="dm in chatStore.dms"
            :key="dm.id"
            @click="handleDMClick(dm)"
            :class="[
              'w-full flex items-center justify-between px-2.5 py-1.5 rounded-md text-xs transition-colors group cursor-pointer text-left',
              chatStore.activeChannel?.id === dm.id
                ? 'bg-mnema-surface text-mnema-accent font-semibold border-l-2 border-mnema-accent pl-2'
                : 'text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text'
            ]"
          >
            <div class="flex items-center gap-2 min-w-0">
              <div class="relative flex-shrink-0">
                <UserAvatar :user="dm.recipient" size="sm" />
                <span
                  :class="[
                    'absolute -bottom-0.5 -right-0.5 w-2 h-2 rounded-full border border-mnema-canvas',
                    chatStore.onlineUserIds.has(dm.recipient.id) ? 'bg-mnema-accent' : 'bg-mnema-tertiary'
                  ]"
                ></span>
              </div>
              <div class="flex flex-col min-w-0">
                <span class="truncate text-xs">{{ dm.recipient.display_name || dm.recipient.username }}</span>
                <span v-if="dm.last_message" class="text-[10px] text-mnema-tertiary truncate max-w-[130px]">
                  {{ dm.last_message.content }}
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- 2. Voice Hangouts Section -->
      <div class="space-y-1.5">
        <div class="px-2 flex items-center justify-between text-[10px] font-semibold tracking-wider uppercase text-mnema-tertiary font-mono">
          <span class="flex items-center gap-1.5">
            <Radio class="w-3 h-3 text-mnema-accent" />
            <span>Voice Hangouts</span>
          </span>
          <div class="flex items-center gap-1">
            <span class="text-[9px] text-mnema-tertiary font-mono mr-1">{{ voiceChannels.length }}</span>
            <button
              v-if="authStore.isAdmin"
              @click.stop="openCreateChannel('voice')"
              title="Voice-Hangout hinzufügen"
              class="p-0.5 rounded text-mnema-tertiary hover:text-mnema-accent hover:bg-mnema-surface transition"
            >
              <Plus class="w-3.5 h-3.5" />
            </button>
          </div>
        </div>

        <div class="space-y-0.5">
          <div v-for="channel in voiceChannels" :key="channel.id">
            <!-- Voice Channel Row -->
            <div
              @click="handleVoiceClick(channel)"
              :class="[
                'w-full flex items-center justify-between px-2.5 py-2 rounded-md text-xs transition-colors group cursor-pointer text-left',
                voiceStore.currentChannelId === channel.id && voiceStore.activeView === 'voice'
                  ? 'bg-mnema-surface text-mnema-accent font-semibold border-l-2 border-mnema-accent pl-2'
                  : 'text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text'
              ]"
            >
              <div class="flex items-center gap-2 min-w-0">
                <Volume2 
                  :class="[
                    'w-3.5 h-3.5 flex-shrink-0 transition-colors',
                    voiceStore.currentChannelId === channel.id ? 'text-mnema-accent' : 'text-mnema-tertiary group-hover:text-mnema-text'
                  ]" 
                />
                <span class="truncate">{{ channel.name }}</span>
              </div>

              <div class="flex items-center gap-1.5 flex-shrink-0">
                <!-- Participant Count Indicator -->
                <span 
                  v-if="voiceStore.channelUsers[channel.id] && Object.keys(voiceStore.channelUsers[channel.id]).length"
                  class="text-[9px] px-1.5 py-0.2 rounded-full bg-mnema-accent-subtle text-mnema-accent font-mono font-bold"
                >
                  {{ Object.keys(voiceStore.channelUsers[channel.id]).length }}
                </span>

                <!-- Delete Channel Button for Herzog (Admin) -->
                <button
                  v-if="authStore.isAdmin"
                  @click.stop="handleDeleteChannel(channel)"
                  title="Voice-Hangout löschen"
                  class="opacity-0 group-hover:opacity-100 p-0.5 rounded text-mnema-tertiary hover:text-mnema-danger hover:bg-mnema-surface transition"
                >
                  <Trash2 class="w-3 h-3" />
                </button>
              </div>
            </div>

            <!-- Nested Connected Voice Users -->
            <div 
              v-if="voiceStore.channelUsers[channel.id] && Object.keys(voiceStore.channelUsers[channel.id]).length"
              class="pl-6 py-1 space-y-1"
            >
              <div
                v-for="user in Object.values(voiceStore.channelUsers[channel.id])"
                :key="user.id"
                class="flex items-center gap-2 text-xs py-0.5 px-1.5 rounded-md hover:bg-mnema-hover/60 transition"
              >
                <div 
                  :class="[
                    'w-4 h-4 rounded-full bg-mnema-surface border border-mnema-border flex items-center justify-center text-[9px] text-mnema-accent font-bold transition-all',
                    voiceStore.speakingUsers[user.id] ? 'ring-2 ring-mnema-accent ring-offset-1 ring-offset-mnema-raised' : ''
                  ]"
                >
                  {{ user.display_name?.charAt(0).toUpperCase() }}
                </div>
                <span class="truncate text-mnema-text text-[11px]">{{ user.display_name }}</span>
                <span v-if="user.role === 'admin'" class="text-[8px] px-1 rounded bg-amber-500/10 text-amber-400 font-mono ml-auto">
                  Admin
                </span>
              </div>
            </div>
          </div>

          <div v-if="!voiceChannels.length" class="px-2 py-2 text-[11px] text-mnema-tertiary italic">
            Keine Voice-Hangouts vorhanden.
          </div>
        </div>
      </div>

      <!-- 3. Text Discussions Section -->
      <div class="space-y-3">
        <div class="px-2 flex items-center justify-between text-[10px] font-semibold tracking-wider uppercase text-mnema-tertiary font-mono">
          <span class="flex items-center gap-1.5">
            <Hash class="w-3 h-3 text-mnema-tertiary" />
            <span>Text Kanäle</span>
          </span>
          <button
            v-if="authStore.isAdmin"
            @click.stop="openCreateChannel('text')"
            title="Text-Kanal hinzufügen"
            class="p-0.5 rounded text-mnema-tertiary hover:text-mnema-accent hover:bg-mnema-surface transition"
          >
            <Plus class="w-3.5 h-3.5" />
          </button>
        </div>

        <!-- Render categories -->
        <div v-for="category in textCategories" :key="category.id" class="space-y-1 group/cat">
          <div class="px-2 flex items-center justify-between text-[9px] font-medium uppercase tracking-wider text-mnema-tertiary font-mono">
            <span>{{ category.name }}</span>
            <div v-if="authStore.isAdmin" class="flex items-center gap-1 opacity-0 group-hover/cat:opacity-100 transition">
              <button
                @click.stop="openCreateChannel('text', category.id)"
                title="Kanal zu Kategorie hinzufügen"
                class="p-0.5 rounded text-mnema-tertiary hover:text-mnema-accent hover:bg-mnema-surface transition"
              >
                <Plus class="w-3 h-3" />
              </button>
              <button
                @click.stop="handleDeleteCategory(category)"
                title="Kategorie löschen"
                class="p-0.5 rounded text-mnema-tertiary hover:text-mnema-danger hover:bg-mnema-surface transition"
              >
                <Trash2 class="w-3 h-3" />
              </button>
            </div>
          </div>

          <div class="space-y-0.5">
            <div
              v-for="channel in category.channels"
              :key="channel.id"
              @click="handleTextClick(channel)"
              :class="[
                'w-full flex items-center justify-between px-2.5 py-1.5 rounded-md text-xs transition-colors group cursor-pointer text-left',
                chatStore.activeChannel?.id === channel.id && voiceStore.activeView === 'chat'
                  ? 'bg-mnema-surface text-mnema-text font-semibold border-l-2 border-mnema-accent pl-2'
                  : 'text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text'
              ]"
            >
              <div class="flex items-center gap-2 min-w-0">
                <Hash 
                  :class="[
                    'w-3.5 h-3.5 flex-shrink-0 transition-colors',
                    chatStore.activeChannel?.id === channel.id ? 'text-mnema-accent' : 'text-mnema-tertiary group-hover:text-mnema-text'
                  ]" 
                />
                <span class="truncate">{{ channel.name }}</span>
              </div>

              <!-- Delete Channel Button for Admin -->
              <button
                v-if="authStore.isAdmin"
                @click.stop="handleDeleteChannel(channel)"
                title="Kanal löschen"
                class="opacity-0 group-hover:opacity-100 p-0.5 rounded text-mnema-tertiary hover:text-mnema-danger hover:bg-mnema-surface transition flex-shrink-0"
              >
                <Trash2 class="w-3 h-3" />
              </button>
            </div>

            <!-- Empty category indicator with add button for admin -->
            <div v-if="!category.channels.length" class="px-2.5 py-1 text-[10px] text-mnema-tertiary italic flex items-center justify-between">
              <span>Keine Kanäle</span>
              <button
                v-if="authStore.isAdmin"
                @click.stop="openCreateChannel('text', category.id)"
                class="text-[10px] text-mnema-accent hover:underline font-mono"
              >
                + Kanal
              </button>
            </div>
          </div>
        </div>

        <!-- Uncategorized Text Channels -->
        <div v-if="uncategorizedText.length" class="space-y-0.5">
          <div class="px-2 text-[9px] font-medium uppercase tracking-wider text-mnema-tertiary font-mono">
            Unkategorisiert
          </div>
          <div
            v-for="channel in uncategorizedText"
            :key="channel.id"
            @click="handleTextClick(channel)"
            :class="[
              'w-full flex items-center justify-between px-2.5 py-1.5 rounded-md text-xs transition-colors group cursor-pointer text-left',
              chatStore.activeChannel?.id === channel.id && voiceStore.activeView === 'chat'
                ? 'bg-mnema-surface text-mnema-text font-semibold border-l-2 border-mnema-accent pl-2'
                : 'text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text'
            ]"
          >
            <div class="flex items-center gap-2 min-w-0">
              <Hash class="w-3.5 h-3.5 flex-shrink-0 text-mnema-tertiary group-hover:text-mnema-text" />
              <span class="truncate">{{ channel.name }}</span>
            </div>

            <button
              v-if="authStore.isAdmin"
              @click.stop="handleDeleteChannel(channel)"
              title="Kanal löschen"
              class="opacity-0 group-hover:opacity-100 p-0.5 rounded text-mnema-tertiary hover:text-mnema-danger hover:bg-mnema-surface transition flex-shrink-0"
            >
              <Trash2 class="w-3 h-3" />
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- Create Channel Modal Dialog -->
    <CreateChannelModal
      v-if="showCreateChannelModal"
      :initial-type="modalChannelType"
      :initial-category-id="modalCategoryId"
      @close="showCreateChannelModal = false"
    />
  </aside>
</template>
