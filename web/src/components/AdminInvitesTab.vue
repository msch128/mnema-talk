<script setup>
// Admin dashboard: invite codes (registration is invite-only).
import { ref, onMounted } from 'vue'
import { Link, Trash2, Check, Copy } from '@lucide/vue'
import { api } from '../lib/api'
import { confirm } from '../lib/confirm'
import { useToastStore } from '../stores/toast'
import { t, locale } from '../i18n'

const toasts = useToastStore()

const invites = ref([])
const newInviteUses = ref('')
const newInviteHours = ref('')
const copiedCode = ref('')

function showError(e) {
  toasts.error(e?.message || t('admin.unknownError'))
}

// A failed reload keeps the list that is already on screen.
async function loadInvites() {
  try {
    invites.value = (await api('/api/admin/invites')) || []
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
    toasts.success(t('admin.inviteCreated'))
  } catch (e) {
    showError(e)
    return
  }
  await loadInvites()
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

onMounted(loadInvites)
</script>

<template>
  <section class="space-y-4">
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
        :placeholder="$t('admin.maxUses')"
        :aria-label="$t('admin.maxUses')"
        class="bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-1.5 text-sm text-mnema-text placeholder-mnema-tertiary outline-none min-w-0 flex-1 basis-56 focus:border-mnema-accent transition"
      />
      <input
        v-model="newInviteHours"
        type="number"
        min="1"
        :placeholder="$t('admin.validHours')"
        :aria-label="$t('admin.validHours')"
        class="bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-1.5 text-sm text-mnema-text placeholder-mnema-tertiary outline-none min-w-0 flex-1 basis-56 focus:border-mnema-accent transition"
      />
      <button
        type="button"
        class="bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold px-3.5 py-1.5 rounded-md text-sm transition shadow-sm"
        @click="createInvite"
      >
        {{ $t('admin.createInvite') }}
      </button>
    </div>

    <div class="bg-mnema-surface rounded-lg border border-mnema-hairline overflow-hidden divide-y divide-mnema-hairline">
      <div v-if="!invites.length" class="p-4 text-sm text-mnema-tertiary">{{ $t('admin.noInvites') }}</div>
      <div v-for="inv in invites" :key="inv.id" class="px-4 py-2.5 flex items-center justify-between gap-3 text-sm">
        <div class="flex items-center gap-3 min-w-0">
          <span class="font-mono bg-mnema-elevated border border-mnema-border px-2 py-0.5 rounded text-mnema-text font-semibold">{{ inv.code }}</span>
          <span class="text-mnema-muted whitespace-nowrap">{{ $t('admin.uses', { used: inv.uses_count, max: inv.max_uses ?? '∞' }) }}</span>
          <span class="text-mnema-tertiary truncate">{{ inviteStatus(inv) }}</span>
        </div>
        <div class="flex items-center gap-3 flex-shrink-0">
          <button
            type="button"
            class="flex items-center gap-1.5 text-mnema-accent hover:text-mnema-accent-hover transition font-medium"
            @click="copyInviteLink(inv.code)"
          >
            <Check v-if="copiedCode === inv.code" class="w-4 h-4" />
            <Copy v-else class="w-4 h-4" />
            <span>{{ copiedCode === inv.code ? $t('admin.copied') : $t('admin.copyLink') }}</span>
          </button>
          <button
            type="button"
            class="text-mnema-tertiary hover:text-mnema-danger transition"
            :title="$t('admin.deleteInvite')"
            :aria-label="$t('admin.deleteInvite')"
            @click="deleteInvite(inv)"
          >
            <Trash2 class="w-4 h-4" />
          </button>
        </div>
      </div>
    </div>
  </section>
</template>
