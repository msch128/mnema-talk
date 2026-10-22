<script setup lang="ts">
// One emoji as text or picture: a custom emoji token (<:name:id>) becomes its
// image, or :name: once the emoji is gone; anything else is shown as typed.
import { computed } from 'vue'
import { emojiImage, parseEmojiToken } from '../lib/customEmoji'
import { prefersReducedMotion, useEmojiStore } from '../stores/emoji'

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
  <img
    v-if="image"
    :src="image.src"
    :alt="image.alt"
    :title="image.alt"
    class="inline-block h-[1.25em] w-auto align-[-0.25em] object-contain"
    draggable="false"
    loading="lazy"
    decoding="async"
  >
  <span v-else-if="token">:{{ token.name }}:</span>
  <span v-else>{{ value }}</span>
</template>
