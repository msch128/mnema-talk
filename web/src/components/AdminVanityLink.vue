<script setup lang="ts">
// The vanity invite link (Owner and Administrators): one custom address for the whole community,
// /join/<name>. By default it only leads visitors to the sign-in page; registration stays invite-only.
// The switch turns it into a permanent public invite, which is a decision of its own.
import { ref, computed, onMounted } from 'vue'
import { Check, Copy } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import { decodeOnboardingVanitySettings } from '../types/rest'
import { joinLink } from '../lib/inviteLink'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'

const MIN_SLUG = 3
const MAX_SLUG = 32
const JOIN_PREFIX = '/join/'

const toasts = useToastStore()
const slug = ref('')
const accepts = ref(false)
const saved = ref({ slug: '', accepts: false })
const loaded = ref(false)
const busy = ref(false)
const copied = ref(false)

const fullLink = computed(() => (saved.value.slug ? joinLink(window.location.origin, saved.value.slug) : ''))
const dirty = computed(() => slug.value.trim().toLowerCase() !== saved.value.slug || (slug.value.trim() !== '' && accepts.value !== saved.value.accepts))
const tooShort = computed(() => slug.value.trim() !== '' && slug.value.trim().length < MIN_SLUG)

async function load() {
  try {
    adopt(await api('/api/admin/vanity-link', { decode: decodeOnboardingVanitySettings }))
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
  }
}

function adopt(v: { slug: string; accepts_signups: boolean }) {
  saved.value = { slug: v.slug, accepts: v.accepts_signups }
  slug.value = v.slug
  accepts.value = v.accepts_signups
  loaded.value = true
}

async function save() {
  busy.value = true
  try {
    adopt(await api('/api/admin/vanity-link', {
      method: 'PUT',
      json: { slug: slug.value, accepts_signups: accepts.value },
      decode: decodeOnboardingVanitySettings
    }))
    toasts.success(t(saved.value.slug ? 'admin.vanity.saved' : 'admin.vanity.removed'))
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
  } finally {
    busy.value = false
  }
}

async function copy() {
  try {
    await navigator.clipboard.writeText(fullLink.value)
    copied.value = true
    setTimeout(() => { copied.value = false }, 2000)
  } catch {
    toasts.error(t('admin.vanity.copyFailed'))
  }
}

onMounted(load)
</script>

<template>
  <section v-if="loaded" class="space-y-3 rounded-lg border border-mnema-hairline p-4" data-testid="vanity-link">
    <h4 class="text-sm font-semibold text-mnema-text">{{ $t('admin.vanity.title') }}</h4>
    <p class="text-xs text-mnema-tertiary">{{ $t('admin.vanity.hint') }}</p>
    <form class="space-y-3" @submit.prevent="save">
      <div class="space-y-1">
        <label for="vanity-slug" class="block text-xs font-medium text-mnema-muted font-mono uppercase tracking-wider">{{ $t('admin.vanity.slug') }}</label>
        <div class="flex max-w-md items-center gap-2">
          <span class="shrink-0 text-sm text-mnema-tertiary">{{ JOIN_PREFIX }}</span>
          <input id="vanity-slug" v-model="slug" type="text" :maxlength="MAX_SLUG" autocomplete="off" autocapitalize="none" spellcheck="false" :placeholder="$t('admin.vanity.placeholder')" class="w-full rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent" data-testid="vanity-slug" />
        </div>
        <p class="text-xs" :class="tooShort ? 'text-mnema-danger' : 'text-mnema-tertiary'">{{ $t('admin.vanity.rules', { min: MIN_SLUG, max: MAX_SLUG }) }}</p>
      </div>
      <label class="flex items-start gap-2 text-sm text-mnema-text">
        <input v-model="accepts" type="checkbox" class="mt-0.5" :disabled="slug.trim() === ''" data-testid="vanity-accepts" />
        <span>
          {{ $t('admin.vanity.accepts') }}
          <span class="block text-xs text-mnema-tertiary">{{ $t('admin.vanity.acceptsHint') }}</span>
        </span>
      </label>
      <button type="submit" class="btn btn-primary" :disabled="busy || !dirty || tooShort" data-testid="vanity-save">{{ $t('common.save') }}</button>
    </form>
    <div v-if="fullLink" class="flex items-center gap-2" data-testid="vanity-full">
      <code class="min-w-0 flex-1 truncate rounded-md bg-mnema-canvas px-2 py-1.5 text-xs text-mnema-text" data-testid="vanity-url">{{ fullLink }}</code>
      <button type="button" class="btn btn-secondary" :aria-label="$t('admin.vanity.copy')" data-testid="vanity-copy" @click="copy">
        <Check v-if="copied" class="h-4 w-4" aria-hidden="true" />
        <Copy v-else class="h-4 w-4" aria-hidden="true" />
        <span>{{ copied ? $t('admin.vanity.copied') : $t('admin.vanity.copy') }}</span>
      </button>
    </div>
  </section>
</template>
