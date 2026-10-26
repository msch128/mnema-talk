// The emoji picker is a web component with a shadow root, out of reach of our
// stylesheet. A constructed stylesheet is adopted into it instead: it is not
// an inline <style>, so the page's CSP (style-src 'self') allows it.

const PICKER_CSS = `
.search-row { padding: 10px 12px 6px; }
input.search {
  height: 38px;
  padding: 0 16px;
  border-radius: 9999px;
  border: 1px solid #2B2F2D;
  background: #171918;
  outline: none;
  transition: border-color 150ms, box-shadow 150ms;
}
input.search:hover { border-color: #3B413E; }
input.search:focus { border-color: #2DA771; box-shadow: 0 0 0 3px rgb(45 167 113 / 0.18); outline: none; }
.nav { padding: 0 8px; }
.nav-button { border-radius: 10px; margin: 2px 1px; transition: background-color 150ms; }
.nav-button:hover { background: #202423; }
.indicator { height: 3px; border-radius: 3px; }
.category {
  padding: 12px 8px 4px;
  font-family: Inter, system-ui, sans-serif;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}
.favorites { border-top: 1px solid #2B2F2D; padding: 2px 8px; }
.emoji { border-radius: 10px; transition: background-color 120ms, transform 120ms; }
.emoji:hover { transform: scale(1.14); }
.emoji:focus-visible { outline: 2px solid #2DA771; outline-offset: -2px; }
button.skintone-button { border-radius: 10px; }
`

/** Adopts the Mnema look into the picker's shadow root (no-op where constructed sheets do not exist). */
export function styleEmojiPicker(picker: { shadowRoot: ShadowRoot | null }): boolean {
  const root = picker.shadowRoot
  if (!root || typeof CSSStyleSheet === 'undefined' || !('adoptedStyleSheets' in root)) return false
  const sheet = new CSSStyleSheet()
  sheet.replaceSync(PICKER_CSS)
  root.adoptedStyleSheets = [...root.adoptedStyleSheets, sheet]
  return true
}
