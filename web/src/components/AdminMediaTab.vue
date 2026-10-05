<script setup>
// Admin dashboard: storage summary, manual prune and the media list.
import { ref, onMounted } from 'vue'
import { RefreshCw, Trash2 } from '@lucide/vue'
import { api } from '../lib/api'
import { confirm } from '../lib/confirm'
import { useToastStore } from '../stores/toast'
import { t, locale } from '../i18n'

const EMPTY_STATS = { total_files: 0, total_size_bytes: 0, deleted_files: 0 }

const toasts = useToastStore()

const stats = ref({ ...EMPTY_STATS })
const mediaItems = ref([])
const pruneDays = ref('')
const isPruning = ref(false)

function showError(e) {
  toasts.error(e?.message || t('admin.unknownError'))
}

// Each request reports its own failure and keeps what is already shown, so
// one broken endpoint neither hides the other nor blanks the numbers.
async function refresh() {
  const [s, m] = await Promise.allSettled([
    api('/api/admin/media/stats'),
    api('/api/admin/media?limit=50')
  ])
  if (s.status === 'fulfilled') stats.value = s.value || { ...EMPTY_STATS }
  else showError(s.reason)
  if (m.status === 'fulfilled') mediaItems.value = m.value || []
  else showError(m.reason)
}

function formatBytes(bytes) {
  if (!bytes) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return new Intl.NumberFormat(locale.value, { maximumFractionDigits: 2 }).format(bytes / Math.pow(k, i)) + ' ' + sizes[i]
}

async function runPrune() {
  const days = parseInt(pruneDays.value, 10)
  if (!days || days < 1) return
  const ok = await confirm({
    title: t('admin.pruneTitle', { days }),
    body: t('admin.pruneBody'),
    confirmLabel: t('common.delete'),
    danger: true
  })
  if (!ok) return
  isPruning.value = true
  try {
    const data = await api(`/api/admin/media/prune?days=${days}`, { method: 'POST' })
    toasts.success(t('admin.pruned', { count: data.pruned_count, days }))
    pruneDays.value = ''
  } catch (e) {
    showError(e)
    return
  } finally {
    isPruning.value = false
  }
  await refresh()
}

async function deleteMedia(item) {
  const ok = await confirm({
    title: t('admin.deleteFileTitle', { name: item.original_filename }),
    body: t('admin.deleteFileBody'),
    confirmLabel: t('common.delete'),
    danger: true
  })
  if (!ok) return
  try {
    await api(`/api/admin/media/${item.id}`, { method: 'DELETE' })
    toasts.success(t('admin.fileDeleted'))
  } catch (e) {
    showError(e)
    return
  }
  await refresh()
}

onMounted(refresh)
</script>

<template>
  <section class="space-y-6">
    <!-- Storage summary & Prune -->
    <div class="bg-mnema-surface p-5 rounded-lg border border-mnema-hairline space-y-3">
      <div class="flex flex-wrap items-center justify-between gap-4">
        <div class="space-y-1">
          <span class="text-xs uppercase font-semibold font-mono text-mnema-tertiary tracking-wider">{{ $t('admin.storage') }}</span>
          <div class="text-xl font-bold text-mnema-text flex items-baseline gap-2">
            <span>{{ formatBytes(stats.total_size_bytes) }}</span>
            <span class="text-sm font-normal text-mnema-muted">({{ $t('admin.storageCounts', { files: stats.total_files, deleted: stats.deleted_files }) }})</span>
          </div>
          <p class="text-sm text-mnema-muted">{{ $t('admin.retentionOff') }}</p>
        </div>
        <button
          type="button"
          class="p-2 rounded-md text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-elevated transition"
          :title="$t('admin.refresh')"
          :aria-label="$t('admin.refresh')"
          @click="refresh"
        >
          <RefreshCw class="w-4 h-4" />
        </button>
      </div>

      <div class="flex flex-wrap items-center gap-2 pt-2 border-t border-mnema-hairline">
        <span class="text-sm text-mnema-muted">{{ $t('admin.pruneLead') }}</span>
        <input
          v-model="pruneDays"
          type="number"
          min="1"
          :placeholder="$t('admin.days')"
          :aria-label="$t('admin.pruneDaysLabel')"
          class="bg-mnema-canvas border border-mnema-border-field rounded-md px-2 py-1 text-sm text-mnema-text w-20 outline-none focus:border-mnema-accent"
        />
        <span class="text-sm text-mnema-muted">{{ $t('admin.days') }}</span>
        <button
          type="button"
          :disabled="!pruneDays || pruneDays < 1 || isPruning"
          class="border border-mnema-danger/40 bg-mnema-danger/10 text-mnema-danger hover:bg-mnema-danger hover:text-mnema-accent-ink font-medium px-3 py-1 rounded-md text-sm transition disabled:opacity-40"
          @click="runPrune"
        >
          {{ isPruning ? $t('admin.deleting') : $t('admin.pruneButton') }}
        </button>
      </div>
    </div>

    <!-- Media Grid -->
    <div class="space-y-3">
      <h3 class="text-sm font-semibold text-mnema-text uppercase font-mono tracking-wider">{{ $t('admin.media') }}</h3>
      <div v-if="!mediaItems.length" class="p-6 text-center text-sm text-mnema-tertiary bg-mnema-surface rounded-lg border border-mnema-hairline">
        {{ $t('admin.noMedia') }}
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
              {{ item.is_deleted ? $t('admin.deletedMarker') : item.original_filename }}
            </div>
          </div>
          <div class="relative z-10 p-2 bg-gradient-to-t from-black/85 via-black/40 to-transparent flex items-end justify-between mt-auto">
            <div class="text-xs text-white truncate max-w-[120px]">
              <p class="truncate font-medium">{{ item.original_filename }}</p>
              <p class="text-mnema-muted">{{ formatBytes(item.size_bytes) }} • {{ item.uploader_name }}</p>
            </div>
            <template v-if="!item.is_deleted">
              <button
                type="button"
                class="p-1 rounded bg-mnema-danger/80 hover:bg-mnema-danger text-mnema-accent-ink transition flex-shrink-0"
                :title="$t('admin.deleteFile')"
                :aria-label="$t('admin.deleteFile')"
                @click.stop="deleteMedia(item)"
              >
                <Trash2 class="w-4 h-4" />
              </button>
            </template>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>
