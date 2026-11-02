<script setup lang="ts">
// The signed-out screen. At /invite/{code} it first asks the server whether the
// code still works (GET /api/invites/{code}, public and rate limited, answering
// only valid / community name / expiry) and then shows the sign-up form with
// the code filled in; everywhere else it is just the login dialog. At /join/{name}
// (the Owner's vanity link) it asks GET /api/join/{name}: a link that takes sign-ups opens the
// sign-up form, one that does not greets the visitor on the sign-in page; an unknown one is
// just the login dialog.
import { ref, onMounted } from 'vue'
import { api } from '../lib/api'
import { inviteCodeFromPath, joinSlugFromPath, vanityInviteCode, type InviteContext } from '../lib/inviteLink'
import { decodeOnboardingInvitePreview, decodeOnboardingJoinPreview } from '../types/rest'
import LoginModal from './LoginModal.vue'

const path = typeof window === 'undefined' ? '' : window.location.pathname
const code = inviteCodeFromPath(path)
const slug = joinSlugFromPath(path)
const invite = ref<InviteContext | null>(null)
const loading = ref(code !== '' || slug !== '')

async function checkVanity() {
  try {
    const preview = await api(`/api/join/${encodeURIComponent(slug)}`, { decode: decodeOnboardingJoinPreview })
    // An unknown link is no invite: the plain login dialog, nothing about the slug.
    if (!preview.valid) return
    invite.value = {
      code: preview.accepts_signups ? vanityInviteCode(slug) : '', valid: true,
      communityName: preview.community_name ?? '', expiresAt: '', vanity: true
    }
  } catch {
    // Rate limited or offline: the login dialog works without it.
  }
}

onMounted(async () => {
  if (slug) {
    await checkVanity()
    loading.value = false
    return
  }
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
