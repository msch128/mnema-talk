<script setup>
import { ref, onMounted } from 'vue'
import { Trash2, Link, Copy, Check, RefreshCw } from '@lucide/vue'
import { api } from '../lib/api'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'
import { confirm } from '../lib/confirm'
import { t, locale } from '../i18n'
import BaseDialog from './BaseDialog.vue'

const emit = defineEmits(['close'])
const authStore = useAuthStore()
const toasts = useToastStore()

const stats = ref({ total_files: 0, total_size_bytes: 0, deleted_files: 0 })
const mediaItems = ref([])
const invites = ref([])

const newInviteUses = ref('')
const newInviteHours = ref('')
const copiedCode = ref('')

const pruneDays = ref('')
const isPruning = ref(false)

function showError(e) {
  toasts.error(e?.message || t('admin.unknownError'))
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
    await refresh()
  } catch (e) {
    showError(e)
  } finally {
    isPruning.value = false
  }
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
    toasts.success(t('admin.inviteCreated'))
  } catch (e) {
    showError(e)
  }
}

async function deleteInvite(inv) {
  const ok = await confirm({
    title: t('admin.deleteInviteTitle', { code: inv.code }),
    body: t('admin.deleteInviteBody'),
    confirmLabel: t('common.delete'),
    danger: true
  })
  if (!ok) return
  try {
    await api(`/api/admin/invites/${inv.id}`, { method: 'DELETE' })
    invites.value = invites.value.filter(i => i.id !== inv.id)
    toasts.success(t('admin.inviteDeleted'))
  } catch (e) {
    showError(e)
  }
}

async function copyInviteLink(code) {
  const url = `${window.location.origin}/?invite=${encodeURIComponent(code)}`
  try {
    await navigator.clipboard.writeText(url)
    copiedCode.value = code
    toasts.success(t('admin.linkCopied'))
    setTimeout(() => { copiedCode.value = '' }, 2000)
  } catch {
    toasts.error(t('admin.copyFailed'), { detail: url })
  }
}

function inviteStatus(inv) {
  if (inv.expires_at && new Date(inv.expires_at) < new Date()) return t('admin.expired')
  if (inv.max_uses != null && inv.uses_count >= inv.max_uses) return t('admin.usedUp')
  return inv.expires_at
    ? t('admin.validUntil', { date: new Date(inv.expires_at).toLocaleString(locale.value) })
    : t('admin.unlimited')
}

function formatBytes(bytes) {
  if (!bytes) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return new Intl.NumberFormat(locale.value, { maximumFractionDigits: 2 }).format(bytes / Math.pow(k, i)) + ' ' + sizes[i]
}

onMounted(refresh)
</script>

<template>
  <BaseDialog
    :title="$t('admin.title')"
    :subtitle="$t('admin.signedInAs', { name: authStore.user?.display_name || authStore.user?.username || '' })"
    panel-class="max-w-4xl max-h-[90vh]"
    @close="emit('close')"
  >
      <div class="flex-1 overflow-y-auto p-6 space-y-6">

        <!-- Storage -->
        <section class="bg-mnema-surface p-5 rounded-lg border border-mnema-hairline space-y-3">
          <div class="flex flex-wrap items-center justify-between gap-4">
            <div class="space-y-1">
              <span class="text-xs uppercase font-semibold font-mono text-mnema-tertiary tracking-wider">{{ $t('admin.storage') }}</span>
              <div class="text-xl font-bold text-mnema-text flex items-baseline gap-2">
                <span>{{ formatBytes(stats.total_size_bytes) }}</span>
                <span class="text-sm font-normal text-mnema-muted">({{ $t('admin.storageCounts', { files: stats.total_files, deleted: stats.deleted_files }) }})</span>
              </div>
              <p class="text-sm text-mnema-muted">{{ $t('admin.retentionOff') }}</p>
            </div>
            <button class="p-2 rounded-md text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-elevated transition" v-tooltip="$t('admin.refresh')" @click="refresh">
              <RefreshCw class="w-4 h-4" />
            </button>
          </div>

          <div class="flex flex-wrap items-center gap-2 pt-2 border-t border-mnema-hairline">
            <span class="text-sm text-mnema-muted">{{ $t('admin.pruneLead') }}</span>
            <input
              v-model="pruneDays"
              type="number"
              min="1"
              :placeholder="$t('admin.days')" :aria-label="$t('admin.pruneDaysLabel')"
              class="bg-mnema-canvas border border-mnema-border-field rounded-md px-2 py-1 text-sm text-mnema-text w-20 outline-none focus:border-mnema-accent"
            />
            <span class="text-sm text-mnema-muted">{{ $t('admin.days') }}</span>
            <button
              :disabled="!pruneDays || pruneDays < 1 || isPruning"
              class="border border-mnema-danger/40 bg-mnema-danger/10 text-mnema-danger hover:bg-mnema-danger hover:text-mnema-accent-ink font-medium px-3 py-1 rounded-md text-sm transition disabled:opacity-40"
              @click="runPrune"
            >
              {{ isPruning ? $t('admin.deleting') : $t('admin.pruneButton') }}
            </button>
          </div>
        </section>

        <!-- Invites -->
        <section class="space-y-3">
          <div class="flex items-center justify-between">
            <h3 class="text-sm font-semibold text-mnema-text uppercase font-mono tracking-wider flex items-center gap-2">
              <Link class="w-4 h-4 text-mnema-accent" />
              <span>{{ $t('admin.invites') }}</span>
            </h3>
            <span class="text-xs text-mnema-tertiary">{{ $t('admin.inviteOnly') }}</span>
          </div>

          <div class="flex flex-wrap gap-2">
            <input
              v-model="newInviteUses"
              type="number"
              min="1"
              :placeholder="$t('admin.maxUses')" :aria-label="$t('admin.maxUses')"
              class="bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-1.5 text-sm text-mnema-text placeholder-mnema-tertiary outline-none min-w-0 flex-1 basis-56 focus:border-mnema-accent transition"
            />
            <input
              v-model="newInviteHours"
              type="number"
              min="1"
              :placeholder="$t('admin.validHours')" :aria-label="$t('admin.validHours')"
              class="bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-1.5 text-sm text-mnema-text placeholder-mnema-tertiary outline-none min-w-0 flex-1 basis-56 focus:border-mnema-accent transition"
            />
            <button class="bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold px-3.5 py-1.5 rounded-md text-sm transition shadow-sm" @click="createInvite">
              {{ $t('admin.createInvite') }}
            </button>
          </div>

          <div class="bg-mnema-surface rounded-lg border border-mnema-hairline overflow-hidden divide-y divide-mnema-hairline">
            <div v-if="!invites.length" class="p-3 text-sm text-mnema-tertiary">{{ $t('admin.noInvites') }}</div>
            <div v-for="inv in invites" :key="inv.id" class="px-4 py-2.5 flex items-center justify-between gap-3 text-sm">
              <div class="flex items-center gap-3 min-w-0">
                <span class="font-mono bg-mnema-elevated border border-mnema-border px-2 py-0.5 rounded text-mnema-text font-semibold">{{ inv.code }}</span>
                <span class="text-mnema-muted whitespace-nowrap">{{ $t('admin.uses', { used: inv.uses_count, max: inv.max_uses ?? '∞' }) }}</span>
                <span v-tooltip.visual="inviteStatus(inv)" class="text-mnema-tertiary truncate">{{ inviteStatus(inv) }}</span>
              </div>
              <div class="flex items-center gap-3 flex-shrink-0">
                <button class="flex items-center gap-1.5 text-mnema-accent hover:text-mnema-accent-hover transition font-medium" @click="copyInviteLink(inv.code)">
                  <Check v-if="copiedCode === inv.code" class="w-4 h-4" />
                  <Copy v-else class="w-4 h-4" />
                  <span>{{ copiedCode === inv.code ? $t('admin.copied') : $t('admin.copyLink') }}</span>
                </button>
                <button class="text-mnema-tertiary hover:text-mnema-danger transition" v-tooltip="$t('admin.deleteInvite')" @click="deleteInvite(inv)">
                  <Trash2 class="w-4 h-4" />
                </button>
              </div>
            </div>
          </div>
        </section>

        <!-- Media -->
        <section class="space-y-3">
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
                    class="p-1 rounded bg-mnema-danger/80 hover:bg-mnema-danger text-mnema-accent-ink transition flex-shrink-0"
                    v-tooltip="$t('admin.deleteFile')"
                    @click.stop="deleteMedia(item)"
                  >
                    <Trash2 class="w-4 h-4" />
                  </button>
                </template>
              </div>
            </div>
          </div>
        </section>
      </div>
  </BaseDialog>
</template>
