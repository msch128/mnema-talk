<script setup>
import { ref, computed } from 'vue'
import { X, Hash, Volume2, FolderPlus, Plus, AlertCircle } from 'lucide-vue-next'
import { useChatStore } from '../stores/chat'

const props = defineProps({
  initialType: {
    type: String,
    default: 'text' // 'text' | 'voice'
  },
  initialCategoryId: {
    type: String,
    default: ''
  }
})

const emit = defineEmits(['close', 'created'])
const chatStore = useChatStore()

const channelType = ref(props.initialType || 'text')
const channelName = ref('')
const selectedCategoryId = ref(props.initialCategoryId || '')
const channelTopic = ref('')
const isCreatingCategory = ref(false)
const newCategoryName = ref('')
const error = ref('')
const isSubmitting = ref(false)

// Slugify helper for text channels
function formatName(val) {
  if (channelType.value === 'text') {
    return val.toLowerCase().replace(/\s+/g, '-').replace(/[^a-z0-9-_]/g, '')
  }
  return val
}

function handleNameInput(e) {
  if (channelType.value === 'text') {
    channelName.value = formatName(e.target.value)
  } else {
    channelName.value = e.target.value
  }
}

async function handleSubmit() {
  error.value = ''
  if (!channelName.value.trim()) {
    error.value = 'Bitte gib einen Kanalnamen an.'
    return
  }

  isSubmitting.value = true
  try {
    let catId = selectedCategoryId.value || null

    // If Herzog chose to create a new category inline
    if (isCreatingCategory.value && newCategoryName.value.trim()) {
      const newCat = await chatStore.createCategory(newCategoryName.value.trim(), chatStore.categories.length)
      catId = newCat.id
    }

    const createdChannel = await chatStore.createChannel({
      categoryId: catId,
      name: channelName.value.trim(),
      type: channelType.value,
      topic: channelTopic.value.trim()
    })

    emit('created', createdChannel)
    emit('close')
  } catch (err) {
    error.value = err.message || 'Kanal konnte nicht erstellt werden.'
  } finally {
    isSubmitting.value = false
  }
}
</script>

<template>
  <div class="fixed inset-0 bg-black/80 z-50 flex items-center justify-center p-4">
    <div class="bg-mnema-elevated w-full max-w-md rounded-xl flex flex-col shadow-2xl border border-mnema-border overflow-hidden">
      <!-- Header -->
      <header class="px-5 py-4 border-b border-mnema-hairline flex items-center justify-between bg-mnema-raised">
        <div class="flex items-center gap-2">
          <div class="w-6 h-6 rounded-md bg-mnema-band text-mnema-mint flex items-center justify-center text-xs font-semibold">
            M
          </div>
          <div>
            <h2 class="text-xs font-semibold text-mnema-text">Neuen Kanal erstellen</h2>
            <p class="text-[10px] text-mnema-tertiary font-mono">Mnema Talk Strukturverwaltung</p>
          </div>
        </div>
        <button 
          @click="emit('close')"
          class="p-1 rounded-md text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition"
        >
          <X class="w-4 h-4" />
        </button>
      </header>

      <!-- Form Body -->
      <form @submit.prevent="handleSubmit" class="p-5 space-y-4">
        <!-- Error Alert -->
        <div v-if="error" class="p-2.5 rounded-lg bg-mnema-danger/10 border border-mnema-danger/30 text-mnema-danger text-xs flex items-center gap-2">
          <AlertCircle class="w-4 h-4 flex-shrink-0" />
          <span>{{ error }}</span>
        </div>

        <!-- 1. Channel Type Segmented Pills -->
        <div class="space-y-1.5">
          <label class="text-[11px] font-medium text-mnema-tertiary uppercase tracking-wider font-mono">Kanal-Typ</label>
          <div class="grid grid-cols-2 gap-2">
            <button
              type="button"
              @click="channelType = 'text'"
              :class="[
                'flex items-center gap-2.5 p-3 rounded-lg border text-left transition',
                channelType === 'text'
                  ? 'bg-mnema-surface border-mnema-accent text-mnema-text ring-1 ring-mnema-accent/40'
                  : 'bg-mnema-canvas border-mnema-hairline text-mnema-muted hover:border-mnema-border'
              ]"
            >
              <div :class="['w-7 h-7 rounded-md flex items-center justify-center flex-shrink-0', channelType === 'text' ? 'bg-mnema-accent/20 text-mnema-accent' : 'bg-mnema-raised text-mnema-tertiary']">
                <Hash class="w-4 h-4" />
              </div>
              <div>
                <div class="text-xs font-semibold">Text-Kanal</div>
                <div class="text-[10px] text-mnema-tertiary">Chat & S3 Medien</div>
              </div>
            </button>

            <button
              type="button"
              @click="channelType = 'voice'"
              :class="[
                'flex items-center gap-2.5 p-3 rounded-lg border text-left transition',
                channelType === 'voice'
                  ? 'bg-mnema-surface border-mnema-accent text-mnema-text ring-1 ring-mnema-accent/40'
                  : 'bg-mnema-canvas border-mnema-hairline text-mnema-muted hover:border-mnema-border'
              ]"
            >
              <div :class="['w-7 h-7 rounded-md flex items-center justify-center flex-shrink-0', channelType === 'voice' ? 'bg-mnema-accent/20 text-mnema-accent' : 'bg-mnema-raised text-mnema-tertiary']">
                <Volume2 class="w-4 h-4" />
              </div>
              <div>
                <div class="text-xs font-semibold">Voice-Hangout</div>
                <div class="text-[10px] text-mnema-tertiary">Talk & 4K Screen</div>
              </div>
            </button>
          </div>
        </div>

        <!-- 2. Category Selection -->
        <div class="space-y-1.5">
          <div class="flex items-center justify-between">
            <label class="text-[11px] font-medium text-mnema-tertiary uppercase tracking-wider font-mono">Kategorie</label>
            <button
              type="button"
              @click="isCreatingCategory = !isCreatingCategory"
              class="text-[10px] text-mnema-accent hover:underline flex items-center gap-1 font-mono"
            >
              <FolderPlus class="w-3 h-3" />
              <span>{{ isCreatingCategory ? 'Bestehende wählen' : '+ Neue Kategorie' }}</span>
            </button>
          </div>

          <!-- Existing Category Selector -->
          <select
            v-if="!isCreatingCategory"
            v-model="selectedCategoryId"
            class="w-full bg-mnema-canvas border border-mnema-border rounded-lg px-3 py-2 text-xs text-mnema-text focus:outline-none focus:border-mnema-accent focus:ring-1 focus:ring-mnema-accent"
          >
            <option value="">(Keine Kategorie / Unkategorisiert)</option>
            <option v-for="cat in chatStore.categories" :key="cat.id" :value="cat.id">
              {{ cat.name }}
            </option>
          </select>

          <!-- Inline New Category Input -->
          <div v-else class="space-y-1">
            <input
              v-model="newCategoryName"
              type="text"
              placeholder="z.B. Projekte, Gaming, Archiv..."
              class="w-full bg-mnema-canvas border border-mnema-border rounded-lg px-3 py-2 text-xs text-mnema-text placeholder-mnema-tertiary focus:outline-none focus:border-mnema-accent focus:ring-1 focus:ring-mnema-accent"
            />
            <p class="text-[10px] text-mnema-tertiary">
              Diese neue Kategorie wird automatisch angelegt und der Kanal darin platziert.
            </p>
          </div>
        </div>

        <!-- 3. Channel Name Input -->
        <div class="space-y-1.5">
          <label class="text-[11px] font-medium text-mnema-tertiary uppercase tracking-wider font-mono">Kanalname</label>
          <div class="relative flex items-center">
            <span class="absolute left-3 text-mnema-tertiary select-none">
              <Hash v-if="channelType === 'text'" class="w-3.5 h-3.5" />
              <Volume2 v-else class="w-3.5 h-3.5" />
            </span>
            <input
              v-model="channelName"
              @input="handleNameInput"
              type="text"
              :placeholder="channelType === 'text' ? 'z.B. off-topic' : 'z.B. Team Hangout'"
              required
              class="w-full bg-mnema-canvas border border-mnema-border rounded-lg pl-9 pr-3 py-2 text-xs text-mnema-text placeholder-mnema-tertiary focus:outline-none focus:border-mnema-accent focus:ring-1 focus:ring-mnema-accent font-mono"
            />
          </div>
          <p class="text-[10px] text-mnema-tertiary">
            {{ channelType === 'text' ? 'Kleinbuchstaben, Ziffern und Bindestriche empfohlen.' : 'Beliebiger Titel für den Sprachraum.' }}
          </p>
        </div>

        <!-- 4. Topic / Description Input (Optional) -->
        <div class="space-y-1.5">
          <label class="text-[11px] font-medium text-mnema-tertiary uppercase tracking-wider font-mono">Thema / Beschreibung (Optional)</label>
          <input
            v-model="channelTopic"
            type="text"
            placeholder="Worüber wird in diesem Raum gesprochen?"
            class="w-full bg-mnema-canvas border border-mnema-border rounded-lg px-3 py-2 text-xs text-mnema-text placeholder-mnema-tertiary focus:outline-none focus:border-mnema-accent focus:ring-1 focus:ring-mnema-accent"
          />
        </div>

        <!-- Actions -->
        <div class="pt-2 flex items-center justify-end gap-2 border-t border-mnema-hairline">
          <button
            type="button"
            @click="emit('close')"
            class="px-4 py-2 rounded-lg border border-mnema-border hover:bg-mnema-surface text-mnema-muted hover:text-mnema-text text-xs transition"
          >
            Abbrechen
          </button>
          <button
            type="submit"
            :disabled="isSubmitting || !channelName.trim()"
            class="px-5 py-2 rounded-lg bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold text-xs transition disabled:opacity-30 disabled:cursor-not-allowed shadow-sm flex items-center gap-1.5"
          >
            <Plus class="w-3.5 h-3.5" />
            <span>{{ isSubmitting ? 'Erstelle...' : 'Kanal erstellen' }}</span>
          </button>
        </div>
      </form>
    </div>
  </div>
</template>
