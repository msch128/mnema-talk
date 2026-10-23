// Syntax highlighting for fenced code blocks, with highlight.js (core build,
// a fixed list of languages, no auto-detection). highlight.js returns HTML;
// it is never trusted: the output is re-tokenised below and only
// <span class="hljs-…"> pairs and text survive. Everything else, and anything
// unbalanced, falls back to the plain escaped code.
import hljs from 'highlight.js/lib/core'
import bash from 'highlight.js/lib/languages/bash'
import c from 'highlight.js/lib/languages/c'
import cpp from 'highlight.js/lib/languages/cpp'
import csharp from 'highlight.js/lib/languages/csharp'
import css from 'highlight.js/lib/languages/css'
import diff from 'highlight.js/lib/languages/diff'
import dockerfile from 'highlight.js/lib/languages/dockerfile'
import go from 'highlight.js/lib/languages/go'
import ini from 'highlight.js/lib/languages/ini'
import java from 'highlight.js/lib/languages/java'
import javascript from 'highlight.js/lib/languages/javascript'
import json from 'highlight.js/lib/languages/json'
import kotlin from 'highlight.js/lib/languages/kotlin'
import lua from 'highlight.js/lib/languages/lua'
import makefile from 'highlight.js/lib/languages/makefile'
import markdown from 'highlight.js/lib/languages/markdown'
import php from 'highlight.js/lib/languages/php'
import powershell from 'highlight.js/lib/languages/powershell'
import python from 'highlight.js/lib/languages/python'
import ruby from 'highlight.js/lib/languages/ruby'
import rust from 'highlight.js/lib/languages/rust'
import shell from 'highlight.js/lib/languages/shell'
import sql from 'highlight.js/lib/languages/sql'
import swift from 'highlight.js/lib/languages/swift'
import typescript from 'highlight.js/lib/languages/typescript'
import xml from 'highlight.js/lib/languages/xml'
import yaml from 'highlight.js/lib/languages/yaml'
import { escapeHtml } from './escape'

const LANGUAGES = { bash, c, cpp, csharp, css, diff, dockerfile, go, ini, java, javascript, json, kotlin, lua, makefile, markdown, php, powershell, python, ruby, rust, shell, sql, swift, typescript, xml, yaml }
for (const [name, definition] of Object.entries(LANGUAGES)) hljs.registerLanguage(name, definition)

/** Larger blocks are shown unhighlighted: tokenising them would stall the UI. */
export const HIGHLIGHT_MAX_CHARS = 8000
export const HIGHLIGHT_MAX_LINES = 300

// Names and aliases of the registered languages: an own list, because
// hljs.getLanguage would also answer for inherited keys like "constructor".
const KNOWN = new Set(hljs.listLanguages().flatMap(name => [name, ...(hljs.getLanguage(name)?.aliases ?? [])]))

/** The languages a code block can be highlighted in (names and aliases). */
export function isHighlightable(lang: string): boolean {
  return KNOWN.has(lang)
}

const TOKEN = /<span class="hljs-[A-Za-z0-9_ -]{1,60}">|<\/span>|[^<]+|</g
const OPEN = '<span class="hljs-'
const KNOWN_ENTITY = /&(?!(?:amp|lt|gt|quot|#x27|#39);)/g

/**
 * Keeps only highlight.js's own span markup. Returns null when the markup is
 * not balanced, so the caller falls back to plain text.
 */
export function sanitizeHighlighted(html: string): string | null {
  let depth = 0
  let out = ''
  for (const [token] of html.matchAll(TOKEN)) {
    if (token.startsWith(OPEN)) {
      depth++
      out += token
    } else if (token === '</span>') {
      if (--depth < 0) return null
      out += token
    } else if (token === '<') {
      out += '&lt;'
    } else {
      out += token.replace(KNOWN_ENTITY, '&amp;').replace(/>/g, '&gt;')
    }
  }
  return depth === 0 ? out : null
}

/** The highlighted, sanitised HTML for the code, or null to show it plain. */
export function highlightCode(code: string, lang: string): string | null {
  if (!isHighlightable(lang) || code.length > HIGHLIGHT_MAX_CHARS) return null
  if (code.split('\n', HIGHLIGHT_MAX_LINES + 1).length > HIGHLIGHT_MAX_LINES) return null
  try {
    return sanitizeHighlighted(hljs.highlight(code, { language: lang, ignoreIllegals: true }).value)
  } catch {
    return null
  }
}

/** The inner HTML of a <code> element: highlighted when possible, else escaped. */
export function codeHtml(code: string, lang: string): string {
  return highlightCode(code, lang) ?? escapeHtml(code)
}
