<script setup lang="ts">
// Admin dashboard: invite codes (registration is invite-only).
import { ref, computed, onMounted } from 'vue'
import { Link, Pencil, Trash2, Check, Copy, ChevronRight } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import type { Invite, CreateInviteRequest } from '../types/domain'
import { isAuthInvite } from '../types/rest'
import { arrayDecoder } from '../types/validation'
import { confirm } from '../lib/confirm'
import { useToastStore } from '../stores/toast'
import { t, locale } from '../i18n'
import EmptyState from './EmptyState.vue'
import EditInviteDialog from './EditInviteDialog.vue'

const toasts = useToastStore()

const invites = ref<Invite[]>([])
const newInviteUses = ref<string | number>('')
const newInviteHours = ref<string | number>('')
const copiedCode = ref('')
const creating = ref(false)
const editing = ref<Invite | null>(null)

function showError(e: unknown) {
  toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
}

// A failed reload keeps the list that is already on screen.
async function loadInvites() {
  try {
    invites.value = await api('/api/admin/invites', { decode: arrayDecoder('invites', isAuthInvite) })
  } catch (e) {
    showError(e)
  }
}

async function createInvite() {
  if (creating.value) return
  creating.value = true
  try {
    const body: CreateInviteRequest = {}
    if (newInviteUses.value) body.max_uses = parseInt(String(newInviteUses.value), 10)
    if (newInviteHours.value) body.expires_in_hours = parseInt(String(newInviteHours.value), 10)
    await api('/api/admin/invites', { method: 'POST', json: body })
    newInviteUses.value = ''
    newInviteHours.value = ''
    toasts.success(t('admin.inviteCreated'))
  } catch (e) {
    showError(e)
    return
  } finally {
    creating.value = false
  }
  await loadInvites()
}

async function deleteInvite(inv: Invite) {
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

function onEdited(updated: Invite) {
  invites.value = invites.value.map(i => (i.id === updated.id ? updated : i))
}

async function copyInviteLink(code: string) {
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

function inviteStatus(inv: Invite) {
  if (inv.expires_at && new Date(inv.expires_at) < new Date()) return t('admin.expired')
  if (inv.max_uses != null && inv.uses_count >= inv.max_uses) return t('admin.usedUp')
  return inv.expires_at
    ? t('admin.validUntil', { date: new Date(inv.expires_at).toLocaleString(locale.value) })
    : t('admin.unlimited')
}

// Used up and expired invites stay out of the way until asked for.
const spent = (inv: Invite) => (!!inv.expires_at && new Date(inv.expires_at) < new Date()) || (inv.max_uses != null && inv.uses_count >= inv.max_uses)
const showSpent = ref(false)
const live = computed(() => invites.value.filter(i => !spent(i)))
const spentCount = computed(() => invites.value.length - live.value.length)
const visible = computed(() => (showSpent.value ? [...live.value, ...invites.value.filter(spent)] : live.value))

// Ready-made validity, in hours; '' is no expiry.
const PRESETS: readonly { key: string; hours: number | '' }[] = [{ key: 'day', hours: 24 }, { key: 'week', hours: 168 }, { key: 'never', hours: '' }]

onMounted(loadInvites)
</script>

<template>
  <section class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
      <h3 class="text-sm font-semibold text-mnema-text uppercase font-mono tracking-wider flex items-center gap-2">
        <Link class="w-4 h-4 text-mnema-accent" />
        <span>{{ $t('admin.invites') }}</span>
      </h3>
      <span class="text-xs text-mnema-tertiary">{{ $t('admin.inviteOnly') }}</span>
    </div>

    <form class="flex flex-wrap items-start gap-3" data-testid="invite-form" @submit.prevent="createInvite">
      <div class="block min-w-0 flex-1 basis-48 space-y-1">
        <label for="invite-uses" class="text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('admin.maxUsesLabel') }}</label>
        <input
          id="invite-uses"
          v-model="newInviteUses"
          type="number"
          min="1"
          :placeholder="$t('admin.maxUsesPlaceholder')"
          class="w-full bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-1.5 text-sm text-mnema-text placeholder-mnema-tertiary outline-hidden focus:border-mnema-accent transition"
        />
      </div>
      <div class="block min-w-0 flex-1 basis-48 space-y-1">
        <label for="invite-hours" class="text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('admin.validHoursLabel') }}</label>
        <input
          id="invite-hours"
          v-model="newInviteHours"
          type="number"
          min="1"
          :placeholder="$t('admin.validHoursPlaceholder')"
          class="w-full bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-1.5 text-sm text-mnema-text placeholder-mnema-tertiary outline-hidden focus:border-mnema-accent transition"
        />
        <div class="flex flex-wrap gap-1.5 pt-1" role="group" :aria-label="$t('admin.presets')">
          <button v-for="preset in PRESETS" :key="preset.key" type="button" class="chip" :aria-pressed="String(newInviteHours) === String(preset.hours) ? 'true' : 'false'" :data-preset="preset.key" @click="newInviteHours = preset.hours">{{ $t(`admin.preset.${preset.key}`) }}</button>
        </div>
      </div>
      <button type="submit" :disabled="creating" class="btn btn-primary sm:mt-6">
        {{ $t('admin.createInvite') }}
      </button>
    </form>

    <div class="bg-mnema-surface rounded-lg border border-mnema-hairline overflow-hidden divide-y divide-mnema-hairline">
      <EmptyState v-if="!visible.length" :icon="Link" :text="$t('admin.noInvites')" :hint="$t('admin.noInvitesHint')" />
      <div v-for="inv in visible" :key="inv.id" :class="['px-4 py-2.5 flex flex-wrap items-center justify-between gap-x-3 gap-y-2 text-sm', spent(inv) && 'opacity-70']">
        <div class="flex flex-wrap items-center gap-x-3 gap-y-1 min-w-0 flex-1 basis-64">
          <span class="font-mono bg-mnema-elevated border border-mnema-border px-2 py-0.5 rounded-sm text-mnema-text font-semibold break-all">{{ inv.code }}</span>
          <span v-if="inv.weak" class="text-mnema-danger cursor-help" data-testid="invite-weak" :title="$t('admin.weakInviteHint')">{{ $t('admin.weakInvite') }}</span>
          <span class="text-mnema-muted whitespace-nowrap">{{ $t('admin.uses', { used: inv.uses_count, max: inv.max_uses ?? '∞' }) }}</span>
          <span class="text-mnema-tertiary">{{ inviteStatus(inv) }}</span>
        </div>
        <div class="flex items-center gap-3 shrink-0 ml-auto">
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
            class="text-mnema-tertiary hover:text-mnema-text transition"
            data-testid="invite-edit"
            :title="$t('admin.editInvite.edit')"
            :aria-label="$t('admin.editInvite.edit')"
            @click="editing = inv"
          >
            <Pencil class="w-4 h-4" />
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
    <button v-if="spentCount" type="button" class="flex items-center gap-1.5 text-sm text-mnema-muted hover:text-mnema-text" :aria-expanded="showSpent ? 'true' : 'false'" data-testid="invites-spent-toggle" @click="showSpent = !showSpent">
      <ChevronRight :class="['h-4 w-4 transition', showSpent && 'rotate-90']" aria-hidden="true" />{{ $t(showSpent ? 'admin.hideSpent' : 'admin.showSpent', { count: spentCount }) }}
    </button>
    <EditInviteDialog v-if="editing" :invite="editing" @close="editing = null" @saved="onEdited" />
  </section>
</template>
