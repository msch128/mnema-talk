<script setup lang="ts">
// Who may start a direct message with this member: everyone (the default) or
// nobody. It only decides who can start one; conversations that exist go on,
// and the member can still start their own.
import { onMounted, ref } from 'vue'
import { api } from '../lib/api'
import { decodeChatPrivacy, type ChatPrivacy } from '../types/rest'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'

const toasts = useToastStore()
const policy = ref<ChatPrivacy['dm_policy']>('everyone')
const loaded = ref(false)
const busy = ref(false)

async function load() {
  try {
    policy.value = (await api('/api/users/me/privacy', { decode: decodeChatPrivacy })).dm_policy
    loaded.value = true
  } catch (error) {
    console.warn('Failed to read the privacy settings:', error)
  }
}

async function choose(next: ChatPrivacy['dm_policy']) {
  if (busy.value || next === policy.value) return
  const before = policy.value
  policy.value = next
  busy.value = true
  try {
    policy.value = (await api('/api/users/me/privacy', { method: 'PUT', json: { dm_policy: next }, decode: decodeChatPrivacy })).dm_policy
  } catch {
    policy.value = before
    toasts.error(t('dm.policy.failed'))
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <div v-if="loaded" class="space-y-2" data-testid="dm-policy" role="radiogroup" aria-labelledby="dm-policy-title">
    <p id="dm-policy-title" class="text-sm font-medium text-mnema-text">{{ $t('dm.policy.title') }}</p>
    <p class="text-sm text-mnema-muted">{{ $t('dm.policy.intro') }}</p>
    <label
      v-for="option in (['everyone', 'nobody'] as const)"
      :key="option"
      :class="['flex cursor-pointer items-start gap-2 rounded-md border px-3 py-2 transition max-md:min-h-11', policy === option ? 'border-mnema-accent/50 bg-mnema-accent/10' : 'border-mnema-border hover:bg-mnema-hover']"
    >
      <input type="radio" name="dm-policy" :value="option" :checked="policy === option" :disabled="busy" :data-dm-policy="option" class="mt-1 accent-mnema-accent" @change="choose(option)" />
      <span>
        <span class="block text-sm font-medium text-mnema-text">{{ $t(`dm.policy.${option}`) }}</span>
        <span class="block text-xs text-mnema-tertiary">{{ $t(`dm.policy.${option}Hint`) }}</span>
      </span>
    </label>
  </div>
</template>
