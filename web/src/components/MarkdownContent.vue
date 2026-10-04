<script setup>
import { computed } from 'vue'

const props = defineProps({
  content: {
    type: String,
    default: ''
  }
})

// Discord-like safe Markdown parser
const parsedHtml = computed(() => {
  if (!props.content) return ''

  // 1. Escape raw HTML for security
  let text = props.content
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')

  // 2. Multiline Code Blocks: ```lang\ncode\n```
  text = text.replace(/```(?:([a-zA-Z0-9_-]+)\n)?([\s\S]*?)```/g, (match, lang, code) => {
    return `<div class="my-1.5 rounded-lg border border-mnema-border bg-mnema-canvas/90 p-2.5 font-mono text-[11px] overflow-x-auto select-text text-mnema-mint shadow-inner">${code.trim()}</div>`
  })

  // 3. Inline Code: `code`
  text = text.replace(/`([^`\n]+)`/g, '<code class="px-1.5 py-0.5 rounded border border-mnema-hairline bg-mnema-surface font-mono text-[11px] text-mnema-mint select-text">$1</code>')

  // 4. Spoilers: ||spoiler|| (Click to reveal like Discord)
  text = text.replace(/\|\|([\s\S]+?)\|\|/g, '<span class="discord-spoiler" onclick="this.classList.toggle(\'revealed\')">$1</span>')

  // 5. Bold & Italic: ***text***
  text = text.replace(/\*\*\*([^*]+)\*\*\*/g, '<strong class="font-bold"><em class="italic">$1</em></strong>')

  // 6. Bold: **text**
  text = text.replace(/\*\*([^*]+)\*\*/g, '<strong class="font-semibold text-mnema-text">$1</strong>')

  // 7. Italic: *text* or _text_
  text = text.replace(/(^|[^*])\*([^*]+)\*([^*]|$)/g, '$1<em class="italic">$2</em>$3')
  text = text.replace(/(^|[^_])_([^_]+)_([^_]|$)/g, '$1<em class="italic">$2</em>$3')

  // 8. Strikethrough: ~~text~~
  text = text.replace(/~~([^~]+)~~/g, '<del class="line-through opacity-70">$1</del>')

  // 9. Blockquotes: > quote (handles single line or multiline quote)
  text = text.replace(/^(&gt;|\>)\s?(.*)$/gm, '<blockquote class="border-l-2 border-mnema-accent/60 pl-2.5 my-0.5 text-mnema-muted italic">$2</blockquote>')

  // 10. Auto-link safe URLs
  text = text.replace(/(https?:\/\/[^\s<]+)/g, '<a href="$1" target="_blank" rel="noopener noreferrer" class="text-mnema-accent hover:underline break-all">$1</a>')

  // 11. User Mentions: @Username
  text = text.replace(/@([a-zA-Z0-9_\-]+)/g, '<span class="px-1 py-0.2 rounded bg-mnema-accent/15 text-mnema-accent font-semibold text-[11px] cursor-pointer hover:bg-mnema-accent hover:text-mnema-accent-ink transition">@$1</span>')

  return text
})
</script>

<template>
  <div class="markdown-body leading-relaxed break-words whitespace-pre-wrap select-text text-xs" v-html="parsedHtml"></div>
</template>

<style>
/* Discord Spoiler: Blacked-out until clicked */
.discord-spoiler {
  background-color: #2b2d31;
  color: transparent !important;
  border-radius: 4px;
  padding: 1px 5px;
  cursor: pointer;
  user-select: none;
  transition: all 0.15s ease;
  display: inline-block;
}

.discord-spoiler:hover {
  background-color: #35373c;
}

.discord-spoiler.revealed {
  background-color: rgba(255, 255, 255, 0.08) !important;
  color: inherit !important;
  user-select: text !important;
  cursor: text;
}
</style>
