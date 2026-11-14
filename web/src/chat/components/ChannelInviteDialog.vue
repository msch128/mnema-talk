<script setup lang="ts">
import type { PropType } from 'vue'
// "Invite people" from a channel's menu: a ready link that lands the joiner in
// this channel, a choice of how long it works, and the member's own invites to
// take back. The server decides what is allowed; this only asks.
import { computed, onMounted, ref } from 'vue'
import { Check, Copy } from '@lucide/vue'
import type { Channel } from '../../types/domain'
import type { AuthInvite } from '../../types/rest'
import { useChannelInvite } from '../composables/useChannelInvite'
import { copyToClipboard } from '../../shared/lib/clipboard'
import { durationLabel } from '../../shared/lib/moderation'
import { useToastStore } from '../../stores/toast'
import { locale, t } from '../../i18n'
import BaseDialog from '../../shared/components/BaseDialog.vue'

const props = defineProps({
  channel: { type: Object as PropType<Channel>, required: true }
})
const emit = defineEmits<{ close: [] }>()

const toasts = useToastStore()
const state = useChannelInvite(() => props.channel.id)
const { own, invite, hours, busy, error, link, choices } = state
const copied = ref(false)

async function copy() {
  if (!link.value) return
  if (await copyToClipboard(link.value, t('memberInvites.copied'))) {
    copied.value = true
    setTimeout(() => { copied.value = false }, 2000)
  }
}

async function revoke(target: AuthInvite) {
  if (await state.revoke(target)) toasts.success(t('memberInvites.revoked'))
  else toasts.error(t('memberInvites.revokeFailed'))
}

function status(inv: AuthInvite): string {
  if (inv.expires_at && new Date(inv.expires_at) < new Date()) return t('memberInvites.expired')
  if (inv.max_uses != null && inv.uses_count >= inv.max_uses) return t('memberInvites.usedUp')
  return inv.expires_at ? t('memberInvites.expires', { date: new Date(inv.expires_at).toLocaleString(locale.value) }) : ''
}

const usesNote = computed(() => (invite.value?.max_uses ? t('memberInvites.uses', { count: invite.value.max_uses }) : ''))

onMounted(() => { void state.start() })
</script>

<template>
  <BaseDialog :title="$t('memberInvites.title', { name: channel.name })" panel-class="max-w-lg" @close="emit('close')">
    <div class="space-y-4" data-testid="channel-invite">
      <p class="text-sm text-mnema-muted">{{ $t('memberInvites.intro', { name: channel.name }) }}</p>

      <div class="space-y-1">
        <label for="channel-invite-link" class="block text-xs font-medium text-mnema-muted font-mono uppercase tracking-wider">{{ $t('memberInvites.link') }}</label>
        <div class="flex items-center gap-2">
          <input
            id="channel-invite-link"
            :value="busy && !link ? $t('memberInvites.making') : link"
            type="text"
            readonly
            class="min-w-0 flex-1 rounded-md border border-mnema-border-field bg-mnema-canvas px-3 py-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent"
            data-testid="channel-invite-link"
            @focus="($event.target as HTMLInputElement).select()"
          />
          <button type="button" class="btn btn-primary shrink-0" :disabled="!link" data-testid="channel-invite-copy" @click="copy">
            <Check v-if="copied" class="h-4 w-4" aria-hidden="true" />
            <Copy v-else class="h-4 w-4" aria-hidden="true" />
            <span>{{ copied ? $t('memberInvites.copied') : $t('memberInvites.copy') }}</span>
          </button>
        </div>
        <p v-if="usesNote" class="text-xs text-mnema-tertiary">{{ usesNote }}</p>
      </div>

      <p v-if="error" class="rounded-md border border-mnema-danger/30 bg-mnema-danger/10 p-2.5 text-sm text-mnema-text" role="alert" data-testid="channel-invite-error">{{ error }}</p>

      <div class="space-y-1.5" role="group" :aria-label="$t('memberInvites.expiry')">
        <span class="block text-xs font-medium text-mnema-muted font-mono uppercase tracking-wider">{{ $t('memberInvites.expiry') }}</span>
        <div class="flex flex-wrap gap-1.5">
          <button
            v-for="h in choices"
            :key="h"
            type="button"
            class="chip"
            :aria-pressed="h === hours ? 'true' : 'false'"
            :disabled="busy"
            :data-hours="h"
            @click="state.make(h)"
          >{{ durationLabel(h * 3600) }}</button>
        </div>
      </div>

      <section class="space-y-1.5" data-testid="own-invites">
        <h4 class="text-sm font-semibold text-mnema-text">{{ $t('memberInvites.yours') }}</h4>
        <p class="text-xs text-mnema-tertiary">{{ $t('memberInvites.yoursHint') }}</p>
        <ul class="max-h-48 divide-y divide-mnema-hairline overflow-y-auto rounded-lg border border-mnema-hairline">
          <li v-for="inv in own" :key="inv.id" class="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 px-3 py-2 text-sm">
            <div class="min-w-0 flex-1 basis-40">
              <span class="block truncate font-mono text-mnema-text">{{ inv.code }}</span>
              <span class="block text-xs text-mnema-tertiary">{{ [inv.max_uses == null ? '' : $t('memberInvites.used', { used: inv.uses_count, max: inv.max_uses }), status(inv)].filter(Boolean).join(' · ') }}</span>
            </div>
            <button type="button" class="text-sm text-mnema-tertiary hover:text-mnema-danger transition" :aria-label="$t('memberInvites.revokeNamed', { code: inv.code })" data-testid="own-invite-revoke" @click="revoke(inv)">{{ $t('memberInvites.revoke') }}</button>
          </li>
          <li v-if="!own.length" class="px-3 py-2 text-sm text-mnema-tertiary">{{ $t('memberInvites.none') }}</li>
        </ul>
      </section>

      <div class="flex justify-end">
        <button type="button" class="btn btn-secondary" @click="emit('close')">{{ $t('memberInvites.done') }}</button>
      </div>
    </div>
  </BaseDialog>
</template>
