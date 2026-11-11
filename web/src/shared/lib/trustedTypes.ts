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

// Where the build puts the worker scripts (vite.config.ts, build.assetsDir).
// The dev server has no build and serves them from their source path.
const OWN_SCRIPT_PREFIXES = import.meta.env.DEV ? [__ASSETS_PREFIX__, '/src/'] : [__ASSETS_PREFIX__]
const SERVICE_WORKER = '/sw.js'

// Scheme and host, not `origin`: under the desktop client's custom scheme every
// origin reads "null", which a data: address would match. The path is checked
// after the URL parser resolved it, so `/assets/../api/x` counts as `/api/x`.
function ownScript(input: string): string {
  const url = new URL(input, window.location.href)
  if (url.protocol !== window.location.protocol || url.host !== window.location.host) {
    throw new TypeError(`Refused to load a script from another origin: ${url.protocol}//${url.host}`)
  }
  if (url.pathname !== SERVICE_WORKER && !OWN_SCRIPT_PREFIXES.some(prefix => url.pathname.startsWith(prefix))) {
    throw new TypeError(`Refused to load a script that is not one of the app's own: ${url.pathname}`)
  }
  return url.href
}

const scriptUrls = createTrustedTypePolicy('mnema-script-url', { createScriptURL: ownScript })

// Created here, when the app starts, and not in the picker's lazily loaded
// chunk: `trusted-types` refuses a second policy of a name, so a name that is
// still free could be taken by any script on the page, with rules of its own.
const pickerTemplates = createTrustedTypePolicy('mnema-emoji-picker', { createHTML: html => html })

/**
 * The address of one of the app's own worker scripts, for `new Worker` and
 * `serviceWorker.register`: `/sw.js` or a file the build wrote under its
 * asset prefix. Another origin and every other path (the API, uploads) are
 * refused. The DOM types do not know Trusted Types yet, so the value is typed
 * as the string it stands for.
 */
export function ownScriptUrl(url: string): string {
  return scriptUrls ? scriptUrls.createScriptURL(url) as string : url
}

/**
 * One of emoji-picker-element's own static templates, for the innerHTML of
 * its template engine. Called only by the patched picker
 * (web/scripts/emojiPickerCsp.ts); it passes text through, so nothing else may
 * use it.
 */
export function emojiPickerTemplate(html: string): string {
  return pickerTemplates ? pickerTemplates.createHTML(html) as string : html
}
