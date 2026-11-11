/**
 * Trusted Types: the browser refuses a plain string at an HTML or script-URL
 * sink (innerHTML, new Worker, serviceWorker.register, ...) and takes only a
 * value made by a policy the Content-Security-Policy names
 * (httpx.SPAContentSecurityPolicy: `require-trusted-types-for 'script'` and
 * `trusted-types`). Every policy of the app is created through this module.
 *
 * A browser without the API gets `null` and the callers pass their plain
 * strings on, exactly as before. The same happens when the page's policy does
 * not allow the name (a host with another policy), so creating a policy can
 * never stop the app from loading.
 */

/** What a policy may produce; a rule left out makes that kind of sink unreachable through the policy. */
export interface TrustedTypeRules<Args extends unknown[]> {
  createHTML?: (input: string, ...args: Args) => string
  createScriptURL?: (input: string, ...args: Args) => string
}

/** A created policy. Its values are opaque objects that stringify to the checked text. */
export interface TrustedTypePolicy<Args extends unknown[]> {
  createHTML(input: string, ...args: Args): unknown
  createScriptURL(input: string, ...args: Args): unknown
}

interface TrustedTypePolicyFactory {
  createPolicy<Args extends unknown[]>(name: string, rules: TrustedTypeRules<Args>): TrustedTypePolicy<Args>
}

export function createTrustedTypePolicy<Args extends unknown[] = []>(name: string, rules: TrustedTypeRules<Args>): TrustedTypePolicy<Args> | null {
  const factory = (globalThis as { trustedTypes?: TrustedTypePolicyFactory }).trustedTypes
  if (!factory) return null
  try {
    return factory.createPolicy(name, rules)
  } catch (error) {
    console.warn(`Trusted Types policy ${name} not created:`, error)
    return null
  }
}

// Scheme and host, not `origin`: under the desktop client's custom scheme every
// origin reads "null", which a data: address would match.
function sameOriginScript(input: string): string {
  const url = new URL(input, window.location.href)
  if (url.protocol !== window.location.protocol || url.host !== window.location.host) {
    throw new TypeError(`Refused to load a script from another origin: ${url.protocol}//${url.host}`)
  }
  return url.href
}

const scriptUrls = createTrustedTypePolicy('mnema-script-url', { createScriptURL: sameOriginScript })

/**
 * The address of one of the app's own worker scripts, for `new Worker` and
 * `serviceWorker.register`. Another origin is refused. The DOM types do not
 * know Trusted Types yet, so the value is typed as the string it stands for.
 */
export function ownScriptUrl(url: string): string {
  return scriptUrls ? scriptUrls.createScriptURL(url) as string : url
}
