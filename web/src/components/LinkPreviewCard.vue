<script setup>
import { ref, onMounted } from 'vue'
import { ExternalLink } from '@lucide/vue'
import { api } from '../lib/api'

const props = defineProps({
  url: {
    type: String,
    required: true
  }
})

const preview = ref(null)
const imageFailed = ref(false)

function imageUrl(raw) {
  if (!raw) return ''
  return `/api/link-preview/image?url=${encodeURIComponent(raw)}`
}

function domainFrom(raw) {
  try {
    return new URL(raw).hostname.replace(/^www\./, '')
  } catch {
    return ''
  }
}

onMounted(async () => {
  try {
    const data = await api(`/api/link-preview?url=${encodeURIComponent(props.url)}`)
    if (data && (data.title || data.description)) {
      preview.value = data
    }
  } catch {
    // 204 or error - don't show preview card
  }
})
</script>

<template>
  <a
    v-if="preview"
    :href="preview.url || url"
    target="_blank"
    rel="noopener noreferrer"
    class="block max-w-lg mt-2 rounded-lg border border-mnema-border bg-mnema-elevated/70 hover:bg-mnema-hover/50 hover:border-mnema-accent/40 transition overflow-hidden text-left no-underline group select-text"
  >
    <div v-if="preview.image && !imageFailed" class="w-full h-40 overflow-hidden bg-mnema-surface flex items-center justify-center">
      <img
        :src="imageUrl(preview.image)"
        :alt="preview.title || ''"
        class="w-full h-full object-cover group-hover:scale-102 transition duration-200"
        loading="lazy"
        @error="imageFailed = true"
      />
    </div>

    <div class="p-3">
      <div class="flex items-center gap-1.5 text-xs text-mnema-tertiary mb-1">
        <span>{{ preview.site_name || domainFrom(preview.url || url) }}</span>
        <ExternalLink class="w-3 h-3 opacity-60" />
      </div>

      <div v-if="preview.title" class="font-medium text-sm text-mnema-text group-hover:text-mnema-accent transition line-clamp-2">
        {{ preview.title }}
      </div>

      <p v-if="preview.description" class="text-xs text-mnema-muted line-clamp-2 mt-1">
        {{ preview.description }}
      </p>
    </div>
  </a>
</template>
