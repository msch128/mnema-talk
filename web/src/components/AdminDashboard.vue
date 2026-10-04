<script setup>
import { ref, onMounted } from 'vue'
import { X, Trash2, Link, Copy, Check, AlertCircle, RefreshCw } from '@lucide/vue'
import { api } from '../lib/api'
import { useAuthStore } from '../stores/auth'

const emit = defineEmits(['close'])
const authStore = useAuthStore()

const stats = ref({ total_files: 0, total_size_bytes: 0, deleted_files: 0 })
const mediaItems = ref([])
const invites = ref([])
const errorMsg = ref('')
const notice = ref('')

const newInviteUses = ref('')
const newInviteHours = ref('')
const copiedCode = ref('')

const pruneDays = ref('')
const confirmPrune = ref(false)
const isPruning = ref(false)
const pendingDelete = ref(null)

function showError(e) {
  errorMsg.value = e?.message || 'Unbekannter Fehler'
  setTimeout(() => { errorMsg.value = '' }, 5000)
}

function showNotice(text) {
  notice.value = text
  setTimeout(() => { notice.value = '' }, 4000)
}

async function refresh() {
  try {
    const [s, m, i] = await Promise.all([
      api('/api/admin/media/stats'),
      api('/api/admin/media?limit=50'),
      api('/api/admin/invites')
    ])
    stats.value = s
    mediaItems.value = m
    invites.value = i
  } catch (e) {
    showError(e)
  }
}

async function runPrune() {
  const days = parseInt(pruneDays.value, 10)
  if (!days || days < 1) return
  isPruning.value = true
  try {
    const data = await api(`/api/admin/media/prune?days=${days}`, { method: 'POST' })
    showNotice(`${data.pruned_count} Datei(en) älter als ${days} Tage gelöscht.`)
    confirmPrune.value = false
    pruneDays.value = ''
    await refresh()
  } catch (e) {
    showError(e)
  } finally {
    isPruning.value = false
  }
}

async function deleteMedia(id) {
  try {
    await api(`/api/admin/media/${id}`, { method: 'DELETE' })
    pendingDelete.value = null
    await refresh()
  } catch (e) {
    showError(e)
  }
}

async function createInvite() {
  try {
    const body = {}
    if (newInviteUses.value) body.max_uses = parseInt(newInviteUses.value, 10)
    if (newInviteHours.value) body.expires_in_hours = parseInt(newInviteHours.value, 10)
    await api('/api/admin/invites', { method: 'POST', json: body })
    newInviteUses.value = ''
    newInviteHours.value = ''
    invites.value = await api('/api/admin/invites')
  } catch (e) {
    showError(e)
  }
}

async function deleteInvite(id) {
  try {
    await api(`/api/admin/invites/${id}`, { method: 'DELETE' })
    invites.value = invites.value.filter(i => i.id !== id)
  } catch (e) {
    showError(e)
  }
}

async function copyInviteLink(code) {
  const url = `${window.location.origin}/?invite=${encodeURIComponent(code)}`
  try {
    await navigator.clipboard.writeText(url)
    copiedCode.value = code
    setTimeout(() => { copiedCode.value = '' }, 2000)
  } catch {
    showError({ message: 'Kopieren nicht möglich – Link: ' + url })
  }
}

function inviteStatus(inv) {
  if (inv.expires_at && new Date(inv.expires_at) < new Date()) return 'abgelaufen'
  if (inv.max_uses != null && inv.uses_count >= inv.max_uses) return 'aufgebraucht'
  return inv.expires_at ? `gültig bis ${new Date(inv.expires_at).toLocaleString('de-DE')}` : 'unbefristet'
}

function formatBytes(bytes) {
  if (!bytes) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
}

onMounted(refresh)
</script>

<template>
  <div class="fixed inset-0 bg-black/80 z-50 flex items-center justify-center p-4">
    <div class="bg-mnema-elevated w-full max-w-4xl max-h-[90vh] rounded-xl flex flex-col shadow-2xl border border-mnema-border overflow-hidden">
      <header class="px-6 py-4 border-b border-mnema-hairline flex items-center justify-between bg-mnema-raised">
        <div class="flex items-center gap-2.5">
          <div class="w-6 h-6 rounded-md bg-mnema-band text-mnema-mint flex items-center justify-center text-sm font-semibold">M</div>
          <div>
            <h2 class="text-lg font-semibold text-mnema-text">Speicher & Administration</h2>
            <p class="text-xs text-mnema-tertiary font-mono">Angemeldet als {{ authStore.user?.display_name }} (Admin)</p>
          </div>
        </div>
        <button class="text-mnema-tertiary hover:text-mnema-text p-1 rounded-md hover:bg-mnema-surface transition" title="Schließen" @click="emit('close')">
          <X class="w-4 h-4" />
        </button>
      </header>

      <div class="flex-1 overflow-y-auto p-6 space-y-6">
        <div v-if="errorMsg" class="p-2.5 rounded-lg bg-red-500/15 border border-red-500/30 text-red-400 text-sm flex items-center gap-2">
          <AlertCircle class="w-4 h-4 flex-shrink-0" />
          <span>{{ errorMsg }}</span>
        </div>
        <div v-if="notice" class="p-2.5 rounded-lg bg-emerald-500/15 border border-emerald-500/30 text-emerald-400 text-sm flex items-center gap-2">
          <Check class="w-4 h-4 flex-shrink-0" />
          <span>{{ notice }}</span>
        </div>

        <!-- Storage -->
        <section class="bg-mnema-surface p-5 rounded-lg border border-mnema-hairline space-y-3">
          <div class="flex flex-wrap items-center justify-between gap-4">
            <div class="space-y-1">
              <span class="text-xs uppercase font-semibold font-mono text-mnema-tertiary tracking-wider">Objektspeicher</span>
              <div class="text-xl font-bold text-mnema-text flex items-baseline gap-2">
                <span>{{ formatBytes(stats.total_size_bytes) }}</span>
                <span class="text-sm font-normal text-mnema-muted">({{ stats.total_files }} Dateien, {{ stats.deleted_files }} gelöscht)</span>
              </div>
              <p class="text-sm text-mnema-muted">Automatische Löschung ist deaktiviert. Medien bleiben gespeichert, bis du sie löschst.</p>
            </div>
            <button class="p-2 rounded-md text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-elevated transition" title="Aktualisieren" @click="refresh">
              <RefreshCw class="w-4 h-4" />
            </button>
          </div>

          <div class="flex flex-wrap items-center gap-2 pt-2 border-t border-mnema-hairline">
            <span class="text-sm text-mnema-muted">Manuell bereinigen: Medien älter als</span>
            <input
              v-model="pruneDays"
              type="number"
              min="1"
              placeholder="Tage"
              class="bg-mnema-canvas border border-mnema-border-field rounded-md px-2 py-1 text-sm text-mnema-text w-20 outline-none focus:border-mnema-accent"
            />
            <span class="text-sm text-mnema-muted">Tage</span>
            <button
              v-if="!confirmPrune"
              :disabled="!pruneDays || pruneDays < 1"
              class="border border-mnema-danger/40 bg-mnema-danger/10 text-mnema-danger hover:bg-mnema-danger hover:text-white font-medium px-3 py-1 rounded-md text-sm transition disabled:opacity-40"
              @click="confirmPrune = true"
            >
              Löschen…
            </button>
            <template v-else>
              <span class="text-sm text-mnema-danger font-medium">Wirklich endgültig löschen?</span>
              <button :disabled="isPruning" class="bg-mnema-danger text-white font-semibold px-3 py-1 rounded-md text-sm disabled:opacity-50" @click="runPrune">
                {{ isPruning ? 'Lösche…' : 'Ja, löschen' }}
              </button>
              <button class="text-sm text-mnema-muted hover:text-mnema-text px-2" @click="confirmPrune = false">Abbrechen</button>
            </template>
          </div>
        </section>

        <!-- Invites -->
        <section class="space-y-3">
          <div class="flex items-center justify-between">
            <h3 class="text-sm font-semibold text-mnema-text uppercase font-mono tracking-wider flex items-center gap-2">
              <Link class="w-4 h-4 text-mnema-accent" />
              <span>Einladungen</span>
            </h3>
            <span class="text-xs text-mnema-tertiary">Registrierung nur mit gültigem Einladungscode</span>
          </div>

          <div class="flex flex-wrap gap-2">
            <input
              v-model="newInviteUses"
              type="number"
              min="1"
              placeholder="Max. Nutzungen (leer = unbegrenzt)"
              class="bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-1.5 text-sm text-mnema-text placeholder-mnema-tertiary outline-none w-60 focus:border-mnema-accent transition"
            />
            <input
              v-model="newInviteHours"
              type="number"
              min="1"
              placeholder="Gültig für Stunden (leer = unbefristet)"
              class="bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-1.5 text-sm text-mnema-text placeholder-mnema-tertiary outline-none w-64 focus:border-mnema-accent transition"
            />
            <button class="bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold px-3.5 py-1.5 rounded-md text-sm transition shadow-sm" @click="createInvite">
              Einladung erstellen
            </button>
          </div>

          <div class="bg-mnema-surface rounded-lg border border-mnema-hairline overflow-hidden divide-y divide-mnema-hairline">
            <div v-if="!invites.length" class="p-3 text-sm text-mnema-tertiary">Noch keine Einladungen.</div>
            <div v-for="inv in invites" :key="inv.id" class="px-4 py-2.5 flex items-center justify-between gap-3 text-sm">
              <div class="flex items-center gap-3 min-w-0">
                <span class="font-mono bg-mnema-elevated border border-mnema-border px-2 py-0.5 rounded text-mnema-text font-semibold">{{ inv.code }}</span>
                <span class="text-mnema-muted">Nutzungen: {{ inv.uses_count }} / {{ inv.max_uses ?? '∞' }}</span>
                <span class="text-mnema-tertiary truncate">{{ inviteStatus(inv) }}</span>
              </div>
              <div class="flex items-center gap-3 flex-shrink-0">
                <button class="flex items-center gap-1.5 text-mnema-accent hover:text-mnema-accent-hover transition font-medium" @click="copyInviteLink(inv.code)">
                  <Check v-if="copiedCode === inv.code" class="w-4 h-4" />
                  <Copy v-else class="w-4 h-4" />
                  <span>{{ copiedCode === inv.code ? 'Kopiert!' : 'Link kopieren' }}</span>
                </button>
                <button class="text-mnema-tertiary hover:text-mnema-danger transition" title="Einladung löschen" @click="deleteInvite(inv.id)">
                  <Trash2 class="w-4 h-4" />
                </button>
              </div>
            </div>
          </div>
        </section>

        <!-- Media -->
        <section class="space-y-3">
          <h3 class="text-sm font-semibold text-mnema-text uppercase font-mono tracking-wider">Hochgeladene Medien</h3>
          <div v-if="!mediaItems.length" class="p-6 text-center text-sm text-mnema-tertiary bg-mnema-surface rounded-lg border border-mnema-hairline">
            Keine Medien vorhanden.
          </div>
          <div v-else class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-3">
            <div
              v-for="item in mediaItems"
              :key="item.id"
              class="group relative bg-mnema-surface rounded-md border border-mnema-hairline overflow-hidden aspect-square flex flex-col justify-between"
            >
              <div class="w-full h-full absolute inset-0 bg-mnema-canvas flex items-center justify-center overflow-hidden">
                <img
                  v-if="!item.is_deleted && item.mime_type.startsWith('image/') && item.mime_type !== 'image/svg+xml'"
                  :src="item.url"
                  :alt="item.original_filename"
                  class="w-full h-full object-cover group-hover:scale-105 transition duration-200"
                  loading="lazy"
                />
                <div v-else class="text-sm text-mnema-tertiary p-2 text-center break-all font-mono">
                  {{ item.is_deleted ? '(gelöscht)' : item.original_filename }}
                </div>
              </div>
              <div class="relative z-10 p-2 bg-gradient-to-t from-black/85 via-black/40 to-transparent flex items-end justify-between mt-auto">
                <div class="text-xs text-white truncate max-w-[120px]">
                  <p class="truncate font-medium">{{ item.original_filename }}</p>
                  <p class="text-mnema-muted">{{ formatBytes(item.size_bytes) }} • {{ item.uploader_name }}</p>
                </div>
                <template v-if="!item.is_deleted">
                  <button
                    v-if="pendingDelete !== item.id"
                    class="p-1 rounded bg-mnema-danger/80 hover:bg-mnema-danger text-white transition flex-shrink-0"
                    title="Datei löschen"
                    @click.stop="pendingDelete = item.id"
                  >
                    <Trash2 class="w-4 h-4" />
                  </button>
                  <button
                    v-else
                    class="px-1.5 py-0.5 rounded bg-mnema-danger text-white text-xs font-semibold flex-shrink-0"
                    @click.stop="deleteMedia(item.id)"
                  >
                    Sicher?
                  </button>
                </template>
              </div>
            </div>
          </div>
        </section>
      </div>
    </div>
  </div>
</template>
