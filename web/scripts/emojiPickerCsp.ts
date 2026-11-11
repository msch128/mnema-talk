// emoji-picker-element and the Content-Security-Policy.
//
// The server sends `style-src 'self'` and `style-src-attr 'none'` (see
// httpx.SPAContentSecurityPolicy). emoji-picker-element 1.29.1 would break
// both: its constructor appends a <style> element to its shadow root, and its
// small template engine writes inline style="" attributes with setAttribute.
// Neither has a nonce or any other switch. The policy also requires Trusted
// Types (`require-trusted-types-for 'script'`), and the picker's template
// engine parses its templates with innerHTML. This plugin applies exact-match
// source patches while Vite bundles the package, so nothing is forked or
// vendored and the package stays the one that is installed:
//
//   1. The base stylesheet goes into a constructable stylesheet
//      (shadowRoot.adoptedStyleSheets), which CSP does not treat as inline
//      markup. Browsers without it (Safari before 16.4) keep the <style>.
//   2. Attribute bindings named "style" are applied through the CSSOM
//      (element.style.cssText), which CSP does not restrict, instead of
//      setAttribute. Custom properties, var() and url() work the same way.
//   3. The template engine's innerHTML takes a TrustedHTML of the policy
//      `mnema-emoji-picker`, which the app creates at start-up
//      (src/shared/lib/trustedTypes.ts); the patched picker imports that module. What
//      it parses is only the static text of the picker's own html`` templates:
//      every expression (emoji, names, search text) is bound afterwards
//      through the DOM, never as markup. Browsers without Trusted Types, or a
//      page whose policy does not allow the name, keep the plain string.
//   4. The fallback for browsers without replaceChildren (Safari 13) empties
//      a node with textContent instead of innerHTML, so the bundle has no
//      other HTML sink.
//
// Each patch must match exactly once. The version is pinned in package.json,
// and a new version that changes one of the sites fails the build (and
// emojiPickerCsp.test.ts) instead of silently shipping a picker that the policy
// blocks. To update: bump the pin, adapt the patches, run the Playwright suite
// (its CSP-violation listener fails every spec on a violation).
import type { Plugin } from 'vite'

export const PINNED_VERSION = '1.29.1'

/** The Trusted Types policy of the patched picker; httpx.SPAContentSecurityPolicy names it. */
export const TEMPLATE_POLICY = 'mnema-emoji-picker'

/**
 * The app module that creates that policy when the app starts and hands the
 * picker its templates through it. The picker's own chunk is loaded on first
 * use, and a policy created only then would leave the name free until then.
 */
export const TEMPLATE_MODULE = '@/shared/lib/trustedTypes'

const STYLE_ELEMENT = `    const style = document.createElement('style');
    style.textContent = baseStyles + EXTRA_STYLES;
    this.shadowRoot.appendChild(style);`

const STYLE_SHEET = `    if (typeof CSSStyleSheet === 'function' && 'adoptedStyleSheets' in Document.prototype && 'replaceSync' in CSSStyleSheet.prototype) {
      // Constructable stylesheet: allowed by style-src 'self' (Mnema CSP patch).
      const sheet = new CSSStyleSheet();
      sheet.replaceSync(baseStyles + EXTRA_STYLES);
      this.shadowRoot.adoptedStyleSheets = [sheet];
    } else {
      const style = document.createElement('style');
      style.textContent = baseStyles + EXTRA_STYLES;
      this.shadowRoot.appendChild(style);
    }`

const SET_ATTRIBUTE = `        targetNode.setAttribute(attributeName, newValue);`

const SET_STYLE = `        if (attributeName === 'style') {
          // CSSOM instead of the attribute: allowed by style-src-attr 'none' (Mnema CSP patch).
          targetNode.style.cssText = newValue;
        } else {
          targetNode.setAttribute(attributeName, newValue);
        }`

const TEMPLATE_PARSE = `function parseTemplate (htmlString) {
  const template = document.createElement('template');
  template.innerHTML = htmlString;
  return template
}`

const TEMPLATE_PARSE_TRUSTED = `// The app's Trusted Types policy for the picker's own static templates (Mnema CSP patch).
import { emojiPickerTemplate as mnemaTemplateHtml } from '${TEMPLATE_MODULE}';

function parseTemplate (htmlString) {
  const template = document.createElement('template');
  template.innerHTML = mnemaTemplateHtml(htmlString);
  return template
}`

const CLEAR_CHILDREN = `    parentNode.innerHTML = '';`

const CLEAR_CHILDREN_TEXT = `    parentNode.textContent = ''; // no HTML sink (Mnema CSP patch)`

function replaceOnce(code: string, from: string, to: string): string {
  const first = code.indexOf(from)
  if (first < 0 || code.indexOf(from, first + 1) >= 0) {
    throw new Error(`emoji-picker-element CSP patch: expected exactly one match for ${JSON.stringify(from.slice(0, 60))}; the package changed (pinned ${PINNED_VERSION})`)
  }
  return code.slice(0, first) + to + code.slice(first + from.length)
}

const PATCHES = [
  [STYLE_ELEMENT, STYLE_SHEET],
  [SET_ATTRIBUTE, SET_STYLE],
  [TEMPLATE_PARSE, TEMPLATE_PARSE_TRUSTED],
  [CLEAR_CHILDREN, CLEAR_CHILDREN_TEXT]
] as const

/** The picker source with the CSP patches applied; throws if the package no longer matches. */
export function patchEmojiPicker(code: string): string {
  return PATCHES.reduce((patched, [from, to]) => replaceOnce(patched, from, to), code)
}

export function emojiPickerCsp(): Plugin {
  return {
    name: 'mnema-emoji-picker-csp',
    enforce: 'pre',
    transform(code, id) {
      return /[\\/]emoji-picker-element[\\/]picker\.js(?:\?.*)?$/.test(id) ? { code: patchEmojiPicker(code), map: null } : null
    }
  }
}
