// The community icon the Owner picked (GET /api/community/icon). The server
// already puts it into the tab icon, the home-screen icon and the manifest of
// every page it serves; this keeps the open tab in step when the Owner changes
// it live, and falls back to the Mnema icon when it is removed.

export type CommunityIconSize = 128 | 512

const DEFAULT_ICON = { href: '/favicon.svg', type: 'image/svg+xml', sizes: '' }
const DEFAULT_TOUCH_ICON = '/apple-touch-icon.png'

/** Where the icon of the given version is served; the version makes the URL immutable. Empty while there is none. */
export function communityIconUrl(version: string, size: CommunityIconSize): string {
  return version ? `/api/community/icon?size=${size}&v=${encodeURIComponent(version)}` : ''
}

function linkFor(rel: string): HTMLLinkElement | null {
  return document.head.querySelector<HTMLLinkElement>(`link[rel="${rel}"]`)
}

/** Points the tab icon and the home-screen icon at the community icon, or back at the Mnema icon. */
export function syncDocumentIcons(version: string): void {
  if (typeof document === 'undefined') return
  const icon = linkFor('icon')
  if (icon) {
    const custom = communityIconUrl(version, 128)
    icon.href = custom || DEFAULT_ICON.href
    icon.type = custom ? 'image/png' : DEFAULT_ICON.type
    if (custom) icon.setAttribute('sizes', '128x128')
    else icon.removeAttribute('sizes')
  }
  const touch = linkFor('apple-touch-icon')
  if (touch) touch.href = communityIconUrl(version, 512) || DEFAULT_TOUCH_ICON
}
