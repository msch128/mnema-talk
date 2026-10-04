<script setup>
import { ref, onMounted } from 'vue'
import { X, Trash2, HardDrive, RefreshCw, Link, Copy, Check, ShieldAlert } from 'lucide-vue-next'
import { useAuthStore } from '../stores/auth'

const emit = defineEmits(['close'])
const authStore = useAuthStore()

const stats = ref({ total_files: 0, total_size_bytes: 0, pruned_files: 0 })
const mediaItems = ref([])
const invites = ref([])
const isPruning = ref(false)
const newInviteUses = ref('')
const copiedCode = ref('')

async function fetchStats() {
  try {
    const res = await fetch('/api/admin/media/stats', {
      headers: { 'Authorization': `Bearer ${authStore.token}` }
    })
    if (res.ok) stats.value = await res.json()
  } catch (e) {
    console.error('Failed to fetch stats:', e)
  }
}

async function fetchMedia() {
  try {
    const res = await fetch('/api/admin/media?limit=50', {
      headers: { 'Authorization': `Bearer ${authStore.token}` }
    })
    if (res.ok) mediaItems.value = await res.json()
  } catch (e) {
    console.error('Failed to fetch media:', e)
  }
}

async function fetchInvites() {
  try {
    const res = await fetch('/api/admin/invites', {
      headers: { 'Authorization': `Bearer ${authStore.token}` }
    })
    if (res.ok) invites.value = await res.json()
  } catch (e) {
    console.error('Failed to fetch invites:', e)
  }
}

async function triggerPrune() {
  if (!confirm('Möchtest du wirklich alle Medien bereinigen, die älter als 30 Tage sind? Physische S3-Dateien werden endgültig gelöscht.')) return
  isPruning.value = true
  try {
    const res = await fetch('/api/admin/media/prune?days=30', {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${authStore.token}` }
    })
    if (res.ok) {
      const data = await res.json()
      alert(`Erfolgreich bereinigt: ${data.pruned_count} Datei(en) wurden gelöscht.`)
      await fetchStats()
      await fetchMedia()
    }
  } catch (e) {
    alert('Fehler beim Bereinigen: ' + e.message)
  } finally {
    isPruning.value = false
  }
}

async function deleteSingleMedia(id) {
  if (!confirm('Diese Datei endgültig vom S3-Speicher und aus der Datenbank löschen?')) return
  try {
    const res = await fetch(`/api/admin/media/${id}`, {
      method: 'DELETE',
      headers: { 'Authorization': `Bearer ${authStore.token}` }
    })
    if (res.ok) {
      mediaItems.value = mediaItems.value.filter(m => m.id !== id)
      await fetchStats()
    }
  } catch (e) {
    alert('Löschen fehlgeschlagen: ' + e.message)
  }
}

async function createInvite() {
  try {
    const uses = newInviteUses.value ? parseInt(newInviteUses.value) : null
    const res = await fetch('/api/admin/invites', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${authStore.token}`
      },
      body: JSON.stringify({ max_uses: uses })
    })
    if (res.ok) {
      newInviteUses.value = ''
      await fetchInvites()
    }
  } catch (e) {
    alert('Fehler beim Erstellen des Einladungslinks: ' + e.message)
  }
}

function copyInviteLink(code) {
  const url = `${window.location.origin}/#/register?invite=${code}`
  navigator.clipboard.writeText(url)
  copiedCode.value = code
  setTimeout(() => { copiedCode.value = '' }, 2000)
}

function formatBytes(bytes) {
  if (!bytes) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
}

onMounted(() => {
  fetchStats()
  fetchMedia()
  fetchInvites()
})
</script>

<template>
  <div class="fixed inset-0 bg-black/70 backdrop-blur-sm z-50 flex items-center justify-center p-4">
    <div class="bg-discord-dark w-full max-w-4xl max-h-[90vh] rounded-xl flex flex-col shadow-2xl border border-discord-darkest overflow-hidden">
      <!-- Modal Header -->
      <header class="px-6 py-4 border-b border-discord-darkest flex items-center justify-between bg-discord-darker">
        <div class="flex items-center gap-2">
          <HardDrive class="w-5 h-5 text-discord-accent" />
          <h2 class="text-lg font-bold text-white">Herzog Admin Dashboard</h2>
        </div>
        <button @click="emit('close')" class="text-discord-muted hover:text-white p-1 rounded transition">
          <X class="w-5 h-5" />
        </button>
      </header>

      <!-- Modal Body -->
      <div class="flex-1 overflow-y-auto p-6 space-y-6">
        <!-- 1. Storage Stats & 30-Day Prune -->
        <section class="bg-discord-darker p-5 rounded-lg border border-discord-darkest flex flex-wrap items-center justify-between gap-4">
          <div class="space-y-1">
            <span class="text-xs uppercase font-bold text-discord-muted tracking-wider">S3 Speicher-Status</span>
            <div class="text-2xl font-black text-white flex items-baseline gap-2">
              <span>{{ formatBytes(stats.total_size_bytes) }}</span>
              <span class="text-xs font-normal text-discord-muted">({{ stats.total_files }} Dateien, {{ stats.pruned_files }} bereinigt)</span>
            </div>
            <p class="text-xs text-discord-muted">Aktive Retention-Policy: Dateien älter als 30 Tage können entfernt werden.</p>
          </div>

          <button
            @click="triggerPrune"
            :disabled="isPruning"
            class="flex items-center gap-2 bg-discord-red hover:bg-discord-red/80 text-white font-semibold px-4 py-2 rounded-lg text-sm transition disabled:opacity-50"
          >
            <RefreshCw :class="['w-4 h-4', isPruning ? 'animate-spin' : '']" />
            <span>{{ isPruning ? 'Bereinigung läuft...' : 'Medien > 30 Tage bereinigen' }}</span>
          </button>
        </section>

        <!-- 2. Invite Codes Manager -->
        <section class="space-y-3">
          <h3 class="text-sm font-bold text-white uppercase tracking-wider flex items-center gap-2">
            <Link class="w-4 h-4 text-discord-accent" />
            <span>Einladungslinks für Freunde</span>
          </h3>

          <div class="flex gap-2">
            <input
              v-model="newInviteUses"
              type="number"
              placeholder="Max. Nutzungen (leer = unbegrenzt)"
              class="bg-discord-darkest border border-discord-light rounded-lg px-3 py-1.5 text-sm text-white placeholder-discord-muted outline-none w-64"
            />
            <button
              @click="createInvite"
              class="bg-discord-accent hover:bg-discord-accent/80 text-white font-medium px-4 py-1.5 rounded-lg text-sm transition"
            >
              Neuen Einladungslink generieren
            </button>
          </div>

          <div class="bg-discord-darker rounded-lg border border-discord-darkest overflow-hidden divide-y divide-discord-darkest">
            <div v-if="!invites.length" class="p-3 text-xs text-discord-muted">Noch keine Einladungslinks erstellt.</div>
            <div
              v-for="inv in invites"
              :key="inv.id"
              class="px-4 py-2.5 flex items-center justify-between text-xs"
            >
              <div class="flex items-center gap-3">
                <span class="font-mono bg-discord-darkest px-2 py-0.5 rounded text-white font-semibold">{{ inv.code }}</span>
                <span class="text-discord-muted">Nutzungen: {{ inv.uses_count }} / {{ inv.max_uses ?? '∞' }}</span>
              </div>

              <button
                @click="copyInviteLink(inv.code)"
                class="flex items-center gap-1.5 text-discord-accent hover:text-white transition"
              >
                <Check v-if="copiedCode === inv.code" class="w-3.5 h-3.5 text-discord-green" />
                <Copy v-else class="w-3.5 h-3.5" />
                <span>{{ copiedCode === inv.code ? 'Kopiert!' : 'Link kopieren' }}</span>
              </button>
            </div>
          </div>
        </section>

        <!-- 3. Image Gallery Raster -->
        <section class="space-y-3">
          <h3 class="text-sm font-bold text-white uppercase tracking-wider">
            Hochgeladene Medien (Galerie & Selektives Löschen)
          </h3>

          <div class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-3">
            <div
              v-for="item in mediaItems"
              :key="item.id"
              class="group relative bg-discord-darker rounded-lg border border-discord-darkest overflow-hidden aspect-square flex flex-col justify-between"
            >
              <!-- Image Preview -->
              <div class="w-full h-full absolute inset-0 bg-discord-darkest flex items-center justify-center overflow-hidden">
                <img
                  v-if="item.mime_type.startsWith('image/')"
                  :src="item.url"
                  :alt="item.original_filename"
                  class="w-full h-full object-cover group-hover:scale-105 transition"
                  loading="lazy"
                />
                <div v-else class="text-xs text-discord-muted p-2 text-center break-all">
                  {{ item.original_filename }}
                </div>
              </div>

              <!-- Overlay Bar -->
              <div class="relative z-10 p-2 bg-gradient-to-t from-black/80 via-black/40 to-transparent flex items-end justify-between mt-auto">
                <div class="text-[10px] text-white truncate max-w-[120px]">
                  <p class="truncate font-semibold">{{ item.original_filename }}</p>
                  <p class="text-discord-muted">{{ formatBytes(item.size_bytes) }} • {{ item.uploader_name }}</p>
                </div>

                <button
                  @click.stop="deleteSingleMedia(item.id)"
                  class="p-1 rounded bg-discord-red/80 hover:bg-discord-red text-white transition flex-shrink-0"
                  title="Datei endgültig löschen"
                >
                  <Trash2 class="w-3.5 h-3.5" />
                </button>
              </div>
            </div>
          </div>
        </section>
      </div>
    </div>
  </div>
</template>
