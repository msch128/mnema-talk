<script setup lang="ts">
// The Owner's calm "Get started" checklist under the channel header. The server
// ticks the steps from real state (stores/firstRun); closing it keeps it one
// click away in the server menu.
import { computed, onMounted, watch } from 'vue'
import { Check, Circle, X } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import { decodeInvite } from '../types/domain'
import { openTalkSettings } from '../lib/talks'
import { requestAdminTab } from '../lib/adminRequest'
import { useChatStore } from '../stores/chat'
import { useFirstRunStore } from '../stores/firstRun'
import { useToastStore } from '../stores/toast'
import { useCommunityName } from '../composables/useCommunityName'
import { useFirstRunShown } from '../composables/useFirstRunShown'
import { t } from '../i18n'

// The shortcut's invite: a week, a handful of people.
const INVITE_HOURS = 24 * 7
const INVITE_USES = 5

const chat = useChatStore()
const firstRun = useFirstRunStore()
const toasts = useToastStore()
const communityName = useCommunityName()

const shown = useFirstRunShown()
const steps = computed(() => firstRun.state?.steps ?? [])
const complete = computed(() => firstRun.state?.complete === true)
// A change of lobbies elsewhere (the Talk settings) is a change of this list.
const lobbies = computed(() => chat.allChannels.filter(c => c.is_lobby).length)

onMounted(() => { void firstRun.ensureLoaded() })
watch(lobbies, () => { void firstRun.reload() })

async function createInvite() {
  try {
    const invite = await api('/api/admin/invites', { method: 'POST', json: { max_uses: INVITE_USES, expires_in_hours: INVITE_HOURS }, decode: decodeInvite })
    const link = `${window.location.origin}/?invite=${encodeURIComponent(invite.code)}`
    try {
      await navigator.clipboard.writeText(link)
      toasts.success(t('firstRun.inviteCopied', { count: INVITE_USES }))
    } catch {
      toasts.success(t('firstRun.inviteCreated'), { detail: link })
    }
    await firstRun.reload()
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
  }
}

function setUpVoice() {
  const talk = chat.allChannels.find(c => c.type === 'voice' && !c.is_temp)
  if (talk) openTalkSettings(talk.id)
  else requestAdminTab('channels')
}

const actions: Record<string, () => void> = {
  name: () => requestAdminTab('onboarding'),
  invite: () => { void createInvite() },
  channels: () => requestAdminTab('channels'),
  voice: setUpVoice,
  status: () => requestAdminTab('status'),
  roles_rules: () => requestAdminTab('onboarding')
}
</script>

<template>
  <section v-if="shown" class="shrink-0 border-b border-mnema-hairline bg-mnema-raised px-4 py-3" :aria-label="$t('firstRun.label')" data-testid="first-run">
    <div class="flex items-start gap-3">
      <div class="min-w-0 flex-1">
        <h2 class="text-sm font-semibold text-mnema-text">{{ complete ? $t('firstRun.allSet') : $t('firstRun.title', { name: communityName }) }}</h2>
        <p class="text-xs text-mnema-tertiary">{{ complete ? $t('firstRun.allSetHint') : $t('firstRun.progress', { done: firstRun.done, total: steps.length }) }}</p>
      </div>
      <button v-if="complete" type="button" class="btn btn-secondary btn-sm shrink-0" data-testid="first-run-done" @click="firstRun.setHidden(true)">{{ $t('firstRun.finish') }}</button>
      <button type="button" class="btn btn-icon btn-sm hover:text-mnema-text shrink-0" :aria-label="$t('firstRun.hide')" v-tooltip="$t('firstRun.hide')" data-testid="first-run-hide" @click="firstRun.setHidden(true)">
        <X class="w-4 h-4" />
      </button>
    </div>
    <ul class="mt-2 grid gap-x-6 gap-y-1 sm:grid-cols-2">
      <li v-for="step in steps" :key="step.id" class="flex min-w-0 items-center gap-2 text-sm" :data-step="step.id" :data-done="step.done ? 'true' : 'false'">
        <Check v-if="step.done" class="h-4 w-4 shrink-0 text-mnema-accent" aria-hidden="true" />
        <Circle v-else class="h-4 w-4 shrink-0 text-mnema-tertiary" aria-hidden="true" />
        <span class="sr-only">{{ step.done ? $t('firstRun.done') : $t('firstRun.todo') }}</span>
        <span :class="['min-w-0 flex-1 truncate', step.done ? 'text-mnema-tertiary line-through' : 'text-mnema-text']" v-tooltip="$t(`firstRun.step.${step.id}.hint`)">
          {{ $t(`firstRun.step.${step.id}.label`) }}<span v-if="step.optional" class="text-mnema-tertiary"> ({{ $t('firstRun.optional') }})</span>
        </span>
        <button v-if="!step.done" type="button" class="btn btn-ghost btn-sm shrink-0" :data-testid="`first-run-${step.id}`" @click="actions[step.id]?.()">{{ $t(`firstRun.step.${step.id}.action`) }}</button>
      </li>
    </ul>
  </section>
</template>
