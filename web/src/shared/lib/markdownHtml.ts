import { renderMarkdown } from './markdown'
import { createTrustedTypePolicy } from './trustedTypes'

type MarkdownOptions = NonNullable<Parameters<typeof renderMarkdown>[1]>

// The policy is the renderer itself: the only way to a value of this policy is
// through renderMarkdown, which escapes all input before it adds markup.
const policy = createTrustedTypePolicy<[MarkdownOptions]>('mnema-markdown', {
  createHTML: (source, options) => renderMarkdown(source, options)
})

/**
 * Markdown as HTML for the one v-html of the app (MarkdownContent.vue). With
 * Trusted Types the value is a TrustedHTML of the policy `mnema-markdown`;
 * the DOM types do not know those yet, so it is typed as the string it stands for.
 */
export function markdownHtml(source: string, options: MarkdownOptions = {}): string {
  return policy ? policy.createHTML(source, options) as string : renderMarkdown(source, options)
}
