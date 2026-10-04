<script setup>
import { ref, onMounted } from 'vue'
import { X, Trash2, HardDrive, RefreshCw, Link, Copy, Check } from 'lucide-vue-next'
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
  <div class="fixed inset-0 bg-black/75 backdrop-blur-sm z-50 flex items-center justify-center p-4">
    <div class="bg-mnema-elevated w-full max-w-4xl max-h-[90vh] rounded-xl flex flex-col shadow-2xl border border-mnema-border overflow-hidden">
      <!-- Modal Header -->
      <header class="px-6 py-4 border-b border-mnema-hairline flex items-center justify-between bg-mnema-raised">
        <div class="flex items-center gap-2.5">
          <div class="w-6 h-6 rounded-md bg-mnema-band text-mnema-mint flex items-center justify-center text-xs font-semibold">
            M
          </div>
          <div>
            <h2 class="text-xs font-semibold text-mnema-text">Mnema Storage & Administration</h2>
            <p class="text-[10px] text-mnema-tertiary font-mono">Angemeldet als Herzog (Admin)</p>
          </div>
        </div>
        <button 
          @click="emit('close')" 
          class="text-mnema-tertiary hover:text-mnema-text p-1 rounded-md hover:bg-mnema-surface transition"
        >
          <X class="w-4 h-4" />
        </button>
      </header>

      <!-- Modal Body -->
      <div class="flex-1 overflow-y-auto p-6 space-y-6">
        <!-- 1. S3 Storage Metrics & Retention Pruning -->
        <section class="bg-mnema-surface p-5 rounded-lg border border-mnema-hairline flex flex-wrap items-center justify-between gap-4">
          <div class="space-y-1">
            <span class="text-[10px] uppercase font-semibold font-mono text-mnema-tertiary tracking-wider">
              S3 Objektspeicher-Status
            </span>
            <div class="text-xl font-bold text-mnema-text flex items-baseline gap-2">
              <span>{{ formatBytes(stats.total_size_bytes) }}</span>
              <span class="text-xs font-normal text-mnema-muted">({{ stats.total_files }} Dateien, {{ stats.pruned_files }} bereinigt)</span>
            </div>
            <p class="text-xs text-mnema-muted">Aktive Retention-Policy: Dateien älter als 30 Tage werden auf Wunsch endgültig entfernt.</p>
          </div>

          <button
            @click="triggerPrune"
            :disabled="isPruning"
            class="flex items-center gap-2 border border-mnema-danger/40 bg-mnema-danger/10 text-mnema-danger hover:bg-mnema-danger hover:text-white font-medium px-3.5 py-2 rounded-md text-xs transition disabled:opacity-50"
          >
            <RefreshCw :class="['w-3.5 h-3.5', isPruning ? 'animate-spin' : '']" />
            <span>{{ isPruning ? 'Bereinigung läuft...' : 'Medien > 30 Tage bereinigen' }}</span>
          </button>
        </section>

        <!-- 2. Invite Codes Manager -->
        <section class="space-y-3">
          <div class="flex items-center justify-between">
            <h3 class="text-xs font-semibold text-mnema-text uppercase font-mono tracking-wider flex items-center gap-2">
              <Link class="w-3.5 h-3.5 text-mnema-accent" />
              <span>Einladungslinks</span>
            </h3>
            <span class="text-[10px] text-mnema-tertiary">Registrierung nur mit gültigem Einladungscode</span>
          </div>

          <div class="flex gap-2">
            <input
              v-model="newInviteUses"
              type="number"
              placeholder="Max. Nutzungen (leer = unbegrenzt)"
              class="bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-1.5 text-xs text-mnema-text placeholder-mnema-tertiary outline-none w-64 focus:border-mnema-accent transition"
            />
            <button
              @click="createInvite"
              class="bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold px-3.5 py-1.5 rounded-md text-xs transition shadow-sm"
            >
              Neuen Einladungscode generieren
            </button>
          </div>

          <div class="bg-mnema-surface rounded-lg border border-mnema-hairline overflow-hidden divide-y divide-mnema-hairline">
            <div v-if="!invites.length" class="p-3 text-xs text-mnema-tertiary">Noch keine Einladungscodes hinterlegt.</div>
            <div
              v-for="inv in invites"
              :key="inv.id"
              class="px-4 py-2.5 flex items-center justify-between text-xs"
            >
              <div class="flex items-center gap-3">
                <span class="font-mono bg-mnema-elevated border border-mnema-border px-2 py-0.5 rounded text-mnema-text font-semibold text-xs">
                  {{ inv.code }}
                </span>
                <span class="text-mnema-muted text-xs">Nutzungen: {{ inv.uses_count }} / {{ inv.max_uses ?? '∞' }}</span>
              </div>

              <button
                @click="copyInviteLink(inv.code)"
                class="flex items-center gap-1.5 text-mnema-accent hover:text-mnema-accent-hover transition font-medium text-xs"
              >
                <Check v-if="copiedCode === inv.code" class="w-3.5 h-3.5 text-mnema-accent" />
                <Copy v-else class="w-3.5 h-3.5" />
                <span>{{ copiedCode === inv.code ? 'Kopiert!' : 'Link kopieren' }}</span>
              </button>
            </div>
          </div>
        </section>

        <!-- 3. Image Gallery Raster & Selective Delete -->
        <section class="space-y-3">
          <h3 class="text-xs font-semibold text-mnema-text uppercase font-mono tracking-wider">
            Hochgeladene Medien (Galerie & Selektives Löschen)
          </h3>

          <div v-if="!mediaItems.length" class="p-6 text-center text-xs text-mnema-tertiary bg-mnema-surface rounded-lg border border-mnema-hairline">
            Keine Medien vorhanden.
          </div>

          <div v-else class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-3">
            <div
              v-for="item in mediaItems"
              :key="item.id"
              class="group relative bg-mnema-surface rounded-md border border-mnema-hairline overflow-hidden aspect-square flex flex-col justify-between"
            >
              <!-- Image Preview -->
              <div class="w-full h-full absolute inset-0 bg-mnema-canvas flex items-center justify-center overflow-hidden">
                <img
                  v-if="item.mime_type.startsWith('image/')"
                  :src="item.url"
                  :alt="item.original_filename"
                  class="w-full h-full object-cover group-hover:scale-105 transition duration-200"
                  loading="lazy"
                />
                <div v-else class="text-xs text-mnema-tertiary p-2 text-center break-all font-mono">
                  {{ item.original_filename }}
                </div>
              </div>

              <!-- Overlay Info & Action -->
              <div class="relative z-10 p-2 bg-gradient-to-t from-black/85 via-black/40 to-transparent flex items-end justify-between mt-auto">
                <div class="text-[10px] text-white truncate max-w-[120px]">
                  <p class="truncate font-medium">{{ item.original_filename }}</p>
                  <p class="text-mnema-muted">{{ formatBytes(item.size_bytes) }} • {{ item.uploader_name }}</p>
                </div>

                <button
                  @click.stop="deleteSingleMedia(item.id)"
                  class="p-1 rounded bg-mnema-danger/80 hover:bg-mnema-danger text-white transition flex-shrink-0"
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
