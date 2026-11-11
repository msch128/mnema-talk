import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderMarkdown } from './markdown'
import type { TrustedTypeRules } from './trustedTypes'

async function load() {
  vi.resetModules()
  return import('./markdownHtml')
}

afterEach(() => { vi.unstubAllGlobals() })

describe('markdownHtml', () => {
  it('is the renderer output as a plain string in a browser without Trusted Types', async () => {
    const { markdownHtml } = await load()
    expect(markdownHtml('**bold** <img src=x onerror=alert(1)>')).toBe(renderMarkdown('**bold** <img src=x onerror=alert(1)>'))
    expect(markdownHtml('@ada', { known: new Set(['ada']), me: 'ada' })).toContain('md-mention-me')
  })

  it('is a value of the policy mnema-markdown, whose only rule is the renderer', async () => {
    const policies: Record<string, TrustedTypeRules<unknown[]>> = {}
    vi.stubGlobal('trustedTypes', {
      createPolicy(name: string, rules: TrustedTypeRules<unknown[]>) {
        policies[name] = rules
        return { createHTML: (input: string, ...args: unknown[]) => ({ trusted: rules.createHTML?.(input, ...args) }) }
      }
    })
    const { markdownHtml } = await load()
    expect(Object.keys(policies)).toContain('mnema-markdown')
    expect(policies['mnema-markdown']?.createScriptURL).toBeUndefined()
    // Markup handed to the policy is escaped like any other message text.
    expect(markdownHtml('<script>alert(1)</script> *hi*')).toEqual({ trusted: renderMarkdown('<script>alert(1)</script> *hi*') })
    expect(policies['mnema-markdown']?.createHTML?.('<b>raw</b>', {})).not.toContain('<b>')
    expect(markdownHtml('@ada', { known: new Set(['ada']), me: 'ada' })).toEqual({ trusted: renderMarkdown('@ada', { known: new Set(['ada']), me: 'ada' }) })
  })
})
