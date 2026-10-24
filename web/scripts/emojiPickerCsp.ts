// emoji-picker-element and the Content-Security-Policy.
//
// The server sends `style-src 'self'` and `style-src-attr 'none'` (see
// httpx.SPAContentSecurityPolicy). emoji-picker-element 1.29.1 would break
// both: its constructor appends a <style> element to its shadow root, and its
// small template engine writes inline style="" attributes with setAttribute.
// Neither has a nonce or any other switch. This plugin applies two exact-match
// source patches while Vite bundles the package, so nothing is forked or
// vendored and the package stays the one that is installed:
//
//   1. The base stylesheet goes into a constructable stylesheet
//      (shadowRoot.adoptedStyleSheets), which CSP does not treat as inline
//      markup. Browsers without it (Safari before 16.4) keep the <style>.
//   2. Attribute bindings named "style" are applied through the CSSOM
//      (element.style.cssText), which CSP does not restrict, instead of
//      setAttribute. Custom properties, var() and url() work the same way.
//
// Each patch must match exactly once. The version is pinned in package.json,
// and a new version that changes either site fails the build (and
// emojiPickerCsp.test.ts) instead of silently shipping a picker that the policy
// blocks. To update: bump the pin, adapt the patches, run the Playwright suite
// (its CSP-violation listener fails every spec on a violation).
import type { Plugin } from 'vite'

export const PINNED_VERSION = '1.29.1'

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

function replaceOnce(code: string, from: string, to: string): string {
  const first = code.indexOf(from)
  if (first < 0 || code.indexOf(from, first + 1) >= 0) {
    throw new Error(`emoji-picker-element CSP patch: expected exactly one match for ${JSON.stringify(from.slice(0, 60))}; the package changed (pinned ${PINNED_VERSION})`)
  }
  return code.slice(0, first) + to + code.slice(first + from.length)
}

/** The picker source with the two CSP patches applied; throws if the package no longer matches. */
export function patchEmojiPicker(code: string): string {
  return replaceOnce(replaceOnce(code, STYLE_ELEMENT, STYLE_SHEET), SET_ATTRIBUTE, SET_STYLE)
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
