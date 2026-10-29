<script setup lang="ts">
import type { LinkPreview } from '../types/domain'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { Play } from '@lucide/vue'
import { fetchLinkPreview } from '../lib/linkPreview'
import { t } from '../i18n'
import { PLAYER_ALLOW, PLAYER_SANDBOX, playerSrc, safeColor, safeHttpUrl } from '../lib/videoEmbed'

const props = defineProps({
  url: {
    type: String,
    required: true
  }
})

const preview = ref<LinkPreview | null>(null)
const imageFailed = ref(false)
// Third-party content loads only after this turns true by a click.
const playing = ref(false)
// The host page refused the player (the desktop apps load no third-party
// content): the card then falls back to a plain link.
const playerBlocked = ref(false)

function imageUrl(raw: string) {
  return `/api/link-preview/image?url=${encodeURIComponent(raw)}`
}

function domainFrom(raw: string) {
  try {
    return new URL(raw).hostname.replace(/^www\./, '')
  } catch {
    return ''
  }
}

const href = computed(() => safeHttpUrl(preview.value?.url) || props.url)
const authorHref = computed(() => safeHttpUrl(preview.value?.author_url))
const accent = computed(() => safeColor(preview.value?.color))
const embedSrc = computed(() => (playerBlocked.value ? null : playerSrc(preview.value?.embed)))
const showImage = computed(() => !!preview.value?.image && !imageFailed.value)
const compact = computed(() => !!preview.value?.compact && !embedSrc.value)
// Fields carry a stable key: known ones are translated, others show the server's name.
const fields = computed(() => (preview.value?.fields ?? []).map(f => ({
  name: f.key && ['price', 'recommendations'].includes(f.key) ? t(`linkPreview.field.${f.key}`) : f.name,
  value: f.value
})))

let stopWatching: (() => void) | null = null

// Reports a refusal of the player by the page's CSP (frame-src) within a few
// seconds of the click, and then goes back to the thumbnail link.
function play() {
  const src = embedSrc.value
  if (!src) return
  const origin = new URL(src).origin
  const onViolation = (e: Event) => {
    const v = e as Partial<SecurityPolicyViolationEvent>
    if (typeof v.blockedURI === 'string' && v.blockedURI.startsWith(origin)) {
      playing.value = false
      playerBlocked.value = true
      stopWatching?.()
    }
  }
  stopWatching?.()
  document.addEventListener('securitypolicyviolation', onViolation)
  const timer = setTimeout(() => stopWatching?.(), 5000)
  stopWatching = () => {
    clearTimeout(timer)
    document.removeEventListener('securitypolicyviolation', onViolation)
    stopWatching = null
  }
  playing.value = true
}

onBeforeUnmount(() => stopWatching?.())

onMounted(async () => {
  // Cached per URL: remounting while scrolling doesn't hit the server again.
  preview.value = await fetchLinkPreview(props.url)
})
</script>

<template>
  <article
    v-if="preview"
    class="mt-2 max-w-lg overflow-hidden rounded-lg border border-mnema-border bg-mnema-elevated/70 text-left select-text"
    :class="accent ? 'border-l-4' : ''"
    :style="accent ? { borderLeftColor: accent } : undefined"
    data-testid="link-preview"
  >
    <div class="flex gap-3 p-3">
      <div class="min-w-0 flex-1">
        <div class="text-xs text-mnema-tertiary" data-testid="link-preview-provider">{{ preview.site_name || domainFrom(href) }}</div>

        <div v-if="preview.author" class="mt-0.5 text-xs text-mnema-muted" data-testid="link-preview-author">
          <a v-if="authorHref" :href="authorHref" target="_blank" rel="noopener noreferrer" class="no-underline hover:text-mnema-text hover:underline">{{ preview.author }}</a>
          <template v-else>{{ preview.author }}</template>
        </div>

        <a
          v-if="preview.title"
          :href="href"
          target="_blank"
          rel="noopener noreferrer"
          class="mt-1 line-clamp-2 block text-sm font-medium text-mnema-accent no-underline hover:underline"
          data-testid="link-preview-title"
        >{{ preview.title }}</a>

        <p v-if="preview.description" class="mt-1 line-clamp-3 text-xs text-mnema-muted">{{ preview.description }}</p>

        <dl v-if="fields.length" class="mt-2 flex flex-wrap gap-x-6 gap-y-1" data-testid="link-preview-fields">
          <div v-for="f in fields" :key="f.name" class="min-w-0">
            <dt class="text-xs text-mnema-tertiary">{{ f.name }}</dt>
            <dd class="text-sm text-mnema-text">{{ f.value }}</dd>
          </div>
        </dl>
      </div>

      <a
        v-if="compact && showImage"
        :href="href"
        target="_blank"
        rel="noopener noreferrer"
        tabindex="-1"
        aria-hidden="true"
        class="h-16 w-16 shrink-0 overflow-hidden rounded-md bg-mnema-surface"
      >
        <img :src="imageUrl(preview.image!)" alt="" class="h-full w-full object-cover" loading="lazy" @error="imageFailed = true" />
      </a>
    </div>

    <div v-if="embedSrc" class="relative aspect-video w-full bg-mnema-surface" data-testid="link-preview-media">
      <iframe
        v-if="playing"
        :src="embedSrc"
        :title="t('linkPreview.playerTitle', { title: preview.title })"
        class="absolute inset-0 h-full w-full border-0"
        :sandbox="PLAYER_SANDBOX"
        :allow="PLAYER_ALLOW"
        allowfullscreen
        referrerpolicy="strict-origin-when-cross-origin"
      />
      <button
        v-else
        type="button"
        class="group absolute inset-0 flex h-full w-full items-center justify-center overflow-hidden focus-visible:outline-2 focus-visible:outline-mnema-accent"
        :aria-label="t('linkPreview.play', { title: preview.title })"
        data-testid="link-preview-play"
        @click="play"
      >
        <img v-if="showImage" :src="imageUrl(preview.image!)" alt="" class="absolute inset-0 h-full w-full object-cover" loading="lazy" @error="imageFailed = true" />
        <span class="relative flex h-12 w-12 items-center justify-center rounded-full bg-black/60 text-white transition group-hover:bg-black/75">
          <Play class="ml-0.5 h-5 w-5" fill="currentColor" aria-hidden="true" />
        </span>
      </button>
    </div>

    <a
      v-else-if="!compact && showImage"
      :href="href"
      target="_blank"
      rel="noopener noreferrer"
      tabindex="-1"
      aria-hidden="true"
      class="block h-44 w-full overflow-hidden bg-mnema-surface"
      data-testid="link-preview-media"
    >
      <img :src="imageUrl(preview.image!)" alt="" class="h-full w-full object-cover" loading="lazy" @error="imageFailed = true" />
    </a>
    <p v-if="playerBlocked" class="px-3 pb-3 text-xs text-mnema-tertiary" data-testid="link-preview-blocked">{{ t('linkPreview.playerBlocked') }}</p>
  </article>
</template>
