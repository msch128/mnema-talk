<script setup lang="ts">
// The vanity invite link (Owner and Administrators): one custom address for the whole community,
// /join/<name>. When active, anyone with the link can sign up without an individual code.
// When inactive, the link is closed and the name stays reserved.
import { ref, computed, onMounted, watch } from 'vue'
import { Check, Copy } from '@lucide/vue'
import { api } from '../../shared/lib/api'
import { decodeOnboardingVanitySettings } from '../../types/rest'
import { normalise, useSettingsDraft } from '../../shared/composables/useSettingsDraft'
import UnsavedChangesBar from '../../shared/components/UnsavedChangesBar.vue'
import SwitchRow from '../../shared/components/SwitchRow.vue'
import { joinLink } from '../../shared/lib/inviteLink'
import { useToastStore } from '../../stores/toast'
import { usePermissionsStore } from '../../stores/permissions'
import { t } from '../../i18n'
import StatePanel from '../../shared/components/StatePanel.vue'

const MIN_SLUG = 3
const MAX_SLUG = 32
const JOIN_PREFIX = '/join/'

const toasts = useToastStore()
// Only the Owner can activate the link; Administrators set the name and may switch it off.
const isOwner = computed(() => usePermissionsStore().isOwner)
const stored = ref({ slug: '', accepts: false })
const loaded = ref(false)
const failed = ref(false)
const copied = ref(false)

const fullLink = computed(() => (stored.value.slug ? joinLink(window.location.origin, stored.value.slug) : ''))
const nameOf = (slug: string) => normalise.text(slug).toLowerCase()
// What is sent: an Administrator cannot keep an active link for another name, so a rename switches it off.
const acceptsFor = (draft: { slug: string; accepts: boolean }) => draft.accepts && (isOwner.value || nameOf(draft.slug) === stored.value.slug)

// One draft with one Save (doc/settings-ux.md): the name and the switch are saved together.
const form = useSettingsDraft<{ slug: string; accepts: boolean }>({
  source: () => ({ ...stored.value }),
  normalisers: { slug: nameOf, accepts: (_, state) => nameOf(state.slug) !== '' && acceptsFor(state) },
  validate: draft => (normalise.text(draft.slug) !== '' && normalise.text(draft.slug).length < MIN_SLUG ? { slug: t('admin.vanity.rules', { min: MIN_SLUG, max: MAX_SLUG }) } : {}),
  blocked: () => !loaded.value,
  save: async ({ draft, commit }) => {
    const v = await api('/api/admin/vanity-link', {
      method: 'PUT',
      json: { slug: draft.slug, accepts_signups: acceptsFor(draft) },
      decode: decodeOnboardingVanitySettings
    })
    adopt(v)
    commit(['slug', 'accepts'], { slug: v.slug, accepts: v.accepts_signups })
    draft.slug = v.slug
    draft.accepts = v.accepts_signups
  },
  saved: () => { toasts.success(t(stored.value.slug ? 'admin.vanity.saved' : 'admin.vanity.removed')) }
})
const { draft, errors } = form

// When no link exists yet and the viewer is the Owner, start with the switch on once a name is typed.
watch(
  () => draft.slug,
  (next, prev) => {
    if (!stored.value.slug && isOwner.value) {
      if (next.trim() !== '' && (!prev || prev.trim() === '')) {
        draft.accepts = true
      } else if (next.trim() === '') {
        draft.accepts = false
      }
    }
  }
)

async function load() {
  failed.value = false
  try {
    adopt(await api('/api/admin/vanity-link', { decode: decodeOnboardingVanitySettings }))
  } catch {
    failed.value = true
  }
}

function adopt(v: { slug: string; accepts_signups: boolean }) {
  stored.value = { slug: v.slug, accepts: v.accepts_signups }
  loaded.value = true
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
  <section v-if="loaded || failed" class="space-y-3 rounded-lg border border-mnema-hairline p-4" data-testid="vanity-link">
    <h4 class="text-sm font-semibold text-mnema-text">{{ $t('admin.vanity.title') }}</h4>
    <p class="text-xs text-mnema-tertiary">{{ $t('admin.vanity.hint') }}</p>
    <StatePanel v-if="!loaded" state="error" retryable data-testid="vanity-error" @retry="load">{{ $t('admin.vanity.loadFailed') }}</StatePanel>
    <form v-else class="space-y-3" @submit.prevent="form.save" @keydown="form.onKeydown">
      <div class="space-y-1">
        <label for="vanity-slug" class="block text-xs font-medium text-mnema-muted font-mono uppercase tracking-wider">{{ $t('admin.vanity.slug') }}</label>
        <div class="flex max-w-md items-center gap-2">
          <span class="shrink-0 text-sm text-mnema-tertiary">{{ JOIN_PREFIX }}</span>
          <input id="vanity-slug" v-model="draft.slug" type="text" :maxlength="MAX_SLUG" autocomplete="off" autocapitalize="none" spellcheck="false" :placeholder="$t('admin.vanity.placeholder')" class="w-full rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent" data-testid="vanity-slug" />
        </div>
        <p class="text-xs" :class="errors.slug ? 'text-mnema-danger' : 'text-mnema-tertiary'">{{ $t('admin.vanity.rules', { min: MIN_SLUG, max: MAX_SLUG }) }}</p>
      </div>
      <SwitchRow
        v-if="isOwner || stored.accepts"
        v-model="draft.accepts"
        :title="$t('admin.vanity.active')"
        :hint="$t('admin.vanity.activeHint')"
        :disabled="draft.slug.trim() === ''"
        data-testid="vanity-active"
      />
      <p v-else class="text-xs text-mnema-tertiary" data-testid="vanity-owner-note">{{ $t('admin.vanity.ownerOnly') }}</p>
      <UnsavedChangesBar inline save-testid="vanity-save" :dirty="form.dirty.value" :can-save="form.canSave.value" :saving="form.saving.value" :error="form.error.value" @save="form.save" @reset="form.reset" />
    </form>
    <div v-if="loaded && fullLink" class="flex items-center gap-2" data-testid="vanity-full">
      <code class="min-w-0 flex-1 truncate rounded-md bg-mnema-canvas px-2 py-1.5 text-xs text-mnema-text" data-testid="vanity-url">{{ fullLink }}</code>
      <button type="button" class="btn btn-secondary" :aria-label="$t('admin.vanity.copy')" data-testid="vanity-copy" @click="copy">
        <Check v-if="copied" class="h-4 w-4" aria-hidden="true" />
        <Copy v-else class="h-4 w-4" aria-hidden="true" />
        <span>{{ copied ? $t('admin.vanity.copied') : $t('admin.vanity.copy') }}</span>
      </button>
    </div>
  </section>
</template>
