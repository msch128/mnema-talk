import type { ObjectDirective } from 'vue'

// v-role-colors on a v-html container: gives every role pill inside it its
// colour. The markup carries the colour as data-role-color="#rrggbb" (a
// style attribute string is blocked by the CSP); this sets --role-color
// through the CSSOM instead, which the CSP allows. Only a strict #rrggbb
// value is applied, anything else leaves the pill in the default colour.

const HEX_COLOR = /^#[0-9a-fA-F]{6}$/

export function isRoleHexColor(value: string | null | undefined): value is string {
  return typeof value === 'string' && HEX_COLOR.test(value)
}

function paint(root: HTMLElement) {
  for (const pill of root.querySelectorAll<HTMLElement>('[data-role-color]')) {
    const color = pill.getAttribute('data-role-color')
    if (isRoleHexColor(color)) pill.style.setProperty('--role-color', color)
    else pill.style.removeProperty('--role-color')
  }
}

export const roleColors: ObjectDirective<HTMLElement> = {
  mounted: paint,
  updated: paint
}
