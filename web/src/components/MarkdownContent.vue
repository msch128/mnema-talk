<script setup>
import { computed } from 'vue'
import { renderMarkdown } from '../lib/markdown'
import { extractPreviewUrls } from '../lib/chatLogic'
import LinkPreviewCard from './LinkPreviewCard.vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'

const props = defineProps({
  content: {
    type: String,
    default: ''
  }
})

const chatStore = useChatStore()
const authStore = useAuthStore()

// Only real members (and @all/@here) are highlighted, and clickable.
const knownNames = computed(() => new Set(chatStore.members.map(m => (m.username || '').toLowerCase())))
const parsedHtml = computed(() => renderMarkdown(props.content, {
  known: knownNames.value,
  me: authStore.user?.username || ''
}))

function openMention(el) {
  const name = el.dataset.mention
  if (!name || name === 'all' || name === 'here') return
  const member = chatStore.members.find(m => (m.username || '').toLowerCase() === name)
  if (member) chatStore.openUserProfile(member)
}

// Only links in plain text get a card, never ones inside code or spoilers.
const links = computed(() => extractPreviewUrls(props.content, 3))

// Spoilers are revealed via delegation: the CSP forbids inline onclick handlers.
function onClick(event) {
  const mention = event.target.closest?.('.md-mention[data-mention]')
  if (mention) {
    openMention(mention)
    return
  }
  const spoiler = event.target.closest?.('.md-spoiler')
  if (spoiler) spoiler.classList.toggle('revealed')
}

function onKeydown(event) {
  if (event.key === 'Enter' && event.target.dataset?.mention) {
    event.preventDefault()
    openMention(event.target)
    return
  }
  if ((event.key === 'Enter' || event.key === ' ') && event.target.classList?.contains('md-spoiler')) {
    event.preventDefault()
    event.target.classList.toggle('revealed')
  }
}
</script>

<template>
  <div>
    <!-- The only intentional v-html: renderMarkdown escapes all input before adding markup. -->
    <!-- eslint-disable vue/no-v-html -->
    <div
      class="markdown-body break-words whitespace-pre-wrap select-text text-message"
      @click="onClick"
      @keydown="onKeydown"
      v-html="parsedHtml"
    ></div>
    <!-- eslint-enable vue/no-v-html -->

    <div v-if="links.length > 0" class="mt-1 space-y-1">
      <LinkPreviewCard v-for="link in links" :key="link" :url="link" />
    </div>
  </div>
</template>

<style>
.markdown-body strong { font-weight: 600; }
.markdown-body em { font-style: italic; }
.markdown-body del { text-decoration: line-through; opacity: 0.7; }
.markdown-body .md-code {
  padding: 0.125rem 0.375rem;
  border-radius: 0.25rem;
  font-family: 'JetBrains Mono', ui-monospace, monospace;
  font-size: 0.875em;
  background: rgba(255, 255, 255, 0.06);
}
.markdown-body .md-codeblock {
  margin: 0.375rem 0;
  padding: 0.625rem;
  border-radius: 0.5rem;
  font-family: 'JetBrains Mono', ui-monospace, monospace;
  font-size: 0.875rem;
  line-height: 1.25rem;
  overflow-x: auto;
  white-space: pre;
  background: rgba(0, 0, 0, 0.35);
}
.markdown-body .md-quote {
  border-left: 2px solid rgba(45, 167, 113, 0.6);
  padding-left: 0.625rem;
  margin: 0.125rem 0;
  opacity: 0.85;
}
.markdown-body .md-link { color: #2da771; word-break: break-all; }
.markdown-body .md-link:hover { text-decoration: underline; }
.markdown-body .md-mention {
  padding: 0 0.25rem;
  border-radius: 0.25rem;
  font-weight: 600;
  color: #2da771;
  background: rgba(45, 167, 113, 0.15);
}
.markdown-body .md-mention[data-mention] { cursor: pointer; }
.markdown-body .md-mention[data-mention]:hover { background: rgba(45, 167, 113, 0.28); }
.markdown-body .md-mention-group[data-mention] { cursor: default; }
.markdown-body .md-mention-me,
.markdown-body .md-mention-group {
  color: #fce4a8;
  background: rgba(224, 162, 58, 0.18);
}
.markdown-body .md-mention-me[data-mention]:hover { background: rgba(224, 162, 58, 0.3); }

/* Spoiler: blacked out until clicked */
.md-spoiler {
  background-color: #2b2d31;
  color: transparent !important;
  border-radius: 4px;
  padding: 1px 5px;
  cursor: pointer;
  user-select: none;
  transition: all 0.15s ease;
  display: inline-block;
}
.md-spoiler:hover { background-color: #35373c; }
.md-spoiler.revealed {
  background-color: rgba(255, 255, 255, 0.08) !important;
  color: inherit !important;
  user-select: text !important;
  cursor: text;
}
</style>
