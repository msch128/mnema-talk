<script setup lang="ts">
// The signed-out screen. At /invite/{code} it first asks the server whether the
// code still works (GET /api/invites/{code}, public and rate limited, answering
// only valid / community name / expiry) and then shows the sign-up form with
// the code filled in; everywhere else it is just the login dialog.
import { ref, onMounted } from 'vue'
import { api } from '../lib/api'
import { inviteCodeFromPath, type InviteContext } from '../lib/inviteLink'
import { decodeOnboardingInvitePreview } from '../types/rest'
import LoginModal from './LoginModal.vue'

const code = typeof window === 'undefined' ? '' : inviteCodeFromPath(window.location.pathname)
const invite = ref<InviteContext | null>(null)
const loading = ref(code !== '')

onMounted(async () => {
  if (!code) return
  try {
    const preview = await api(`/api/invites/${encodeURIComponent(code)}`, { decode: decodeOnboardingInvitePreview })
    invite.value = { code, valid: preview.valid, communityName: preview.community_name ?? '', expiresAt: preview.expires_at ?? '' }
  } catch {
    // Rate limited or offline: no verdict, the form still works with the code.
    invite.value = { code, valid: null, communityName: '', expiresAt: '' }
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <div v-if="loading" class="flex-1 flex items-center justify-center" role="status" aria-live="polite" data-testid="invite-loading">
    <span class="text-sm text-mnema-tertiary">{{ $t('login.inviteChecking') }}</span>
  </div>
  <LoginModal v-else-if="invite" :invite="invite" />
  <LoginModal v-else />
</template>
