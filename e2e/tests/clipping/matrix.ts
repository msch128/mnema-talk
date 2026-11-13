// The sizes, languages and zoom levels the clipping sweep looks at, and the
// smoke subset of them that runs in every e2e job.

export interface Viewport { name: string; width: number; height: number }
export type Locale = 'en' | 'de'
export interface Combo { viewport: Viewport; locale: Locale; zoom: 1 | 1.25 }

// The desktop client opens at 1200x820 with a minimum of 640x480 and keeps 68px
// on the left for its server rail (mnema-talk-desktop: web_shell/window.rs, RAIL_WIDTH).
export const VIEWPORTS: Viewport[] = [
  { name: 'full-hd', width: 1920, height: 1080 },
  { name: 'laptop', width: 1366, height: 768 },
  { name: 'small-laptop', width: 1280, height: 720 },
  { name: 'desktop-default', width: 1132, height: 820 },
  { name: 'desktop-minimum', width: 572, height: 480 },
  { name: 'tablet', width: 820, height: 1180 },
  { name: 'phone', width: 390, height: 844 },
]

const byName = (name: string): Viewport => {
  const found = VIEWPORTS.find(v => v.name === name)
  if (!found) throw new Error(`unknown viewport ${name}`)
  return found
}

/**
 * "125 percent" is the browser zoomed to 125 %: the page gets a CSS viewport
 * of 80 % and draws every CSS pixel with 1.25 device pixels. The app pins the
 * root font size to 16px (style.css), so the browser's font-size preference
 * alone changes nothing; zoom is what a reader who needs larger text uses.
 */
export function cssViewport(combo: Combo): { width: number; height: number } {
  return { width: Math.round(combo.viewport.width / combo.zoom), height: Math.round(combo.viewport.height / combo.zoom) }
}

export function comboLabel(combo: Combo): string {
  return `${combo.viewport.name} ${combo.viewport.width}x${combo.viewport.height} ${combo.locale}${combo.zoom === 1 ? '' : ' zoom125'}`
}

/** Every size in both languages at 100 %, and every size at 125 % in German (the longer strings). */
export function fullMatrix(): Combo[] {
  const out: Combo[] = []
  for (const viewport of VIEWPORTS) {
    for (const locale of ['en', 'de'] as const) out.push({ viewport, locale, zoom: 1 })
    out.push({ viewport, locale: 'de', zoom: 1.25 })
  }
  return out
}

/** The reported sizes: the desktop client's default window and a phone, German. */
export function smokeMatrix(): Combo[] {
  return [
    { viewport: byName('desktop-default'), locale: 'de', zoom: 1 },
    { viewport: byName('phone'), locale: 'en', zoom: 1 },
  ]
}
