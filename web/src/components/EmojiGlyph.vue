<script setup lang="ts">
// One emoji as text or picture: a custom emoji token (<:name:id>) becomes its
// image, or :name: once the emoji is gone; anything else is shown as typed.
import { computed } from 'vue'
import { emojiImage, parseEmojiToken } from '../lib/customEmoji'
import { prefersReducedMotion, useEmojiStore } from '../stores/emoji'
import MediaImage from './MediaImage.vue'

const props = defineProps({
  value: { type: String, required: true }
})
const store = useEmojiStore()
const token = computed(() => parseEmojiToken(props.value))
const image = computed(() => {
  const emoji = token.value ? store.byId.get(token.value.id) : undefined
  return emoji ? emojiImage(emoji, prefersReducedMotion()) : null
})
</script>

<template>
  <MediaImage
    v-if="image"
    :src="image.src"
    :alt="image.alt"
    :title="image.alt"
    class="inline-block h-[1.25em] w-auto align-[-0.25em] object-contain"
    draggable="false"
    loading="lazy"
    decoding="async"
  >
    <!-- The emoji's picture is gone from storage: its name, as for a deleted emoji. -->
    <template #fallback="{ state, retry }">
      <span v-if="state === 'gone'" :title="$t('media.imageDeleted')">:{{ token?.name }}:</span>
      <button v-else type="button" class="cursor-pointer" :title="$t('media.retry')" @click.stop.prevent="retry">:{{ token?.name }}:</button>
    </template>
  </MediaImage>
  <span v-else-if="token">:{{ token.name }}:</span>
  <span v-else>{{ value }}</span>
</template>
