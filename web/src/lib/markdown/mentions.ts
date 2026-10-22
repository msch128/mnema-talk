// @user and @Role mentions as text rules.
import type { Inline } from './types'
import type { TextRule } from './rules'

/** A role the message mentions; me marks a role the reader holds. */
export interface RoleMention {
  name: string
  me?: boolean
  /** The role colour as "#rrggbb"; anything else is ignored. */
  color?: string
}

const text = (v: string): Inline[] => (v ? [{ t: 'text', v }] : [])

/**
 * @name for a member (3 to 32 letters, digits, "_", "." or "-"). With `known`
 * only those members and @all/@here are highlighted, and they get the data
 * the click handler needs. `me` marks the reader's own mentions.
 */
export function userMentionRule(known: ReadonlySet<string> | null, me: string): TextRule {
  const self = me.toLowerCase()
  return {
    re: /(^|[\s(])@([A-Za-z0-9_.-]{3,32})/g,
    build(m) {
      const pre = m[1] ?? ''
      const raw = m[2] ?? ''
      const name = raw.replace(/[.-]+$/, '')
      const lower = name.toLowerCase()
      const group = lower === 'all' || lower === 'here'
      if (name.length < 3 || (known && !group && !known.has(lower))) return null
      const cls = ['md-mention']
      if (group) cls.push('md-mention-group')
      if (lower === self) cls.push('md-mention-me')
      const attrs: Record<string, string> = { class: cls.join(' ') }
      if (known) Object.assign(attrs, { 'data-mention': lower, role: 'button', tabindex: '0' })
      return [...text(pre), { t: 'el', tag: 'span', attrs, c: text(`@${name}`) }, ...text(raw.slice(name.length))]
    }
  }
}

const HEX_COLOR = /^#[0-9a-fA-F]{6}$/

/**
 * "@Role Name" pills for the roles the server recorded on the message. Role
 * names may contain spaces, so the known names are matched (longest first)
 * instead of a username-like token. The colour travels as data-role-color (a
 * validated #rrggbb); the CSP forbids inline styles, so the v-role-colors
 * directive applies it.
 */
export function roleMentionRule(roles: readonly RoleMention[]): TextRule | null {
  if (roles.length === 0) return null
  const byName = new Map(roles.map(r => [r.name.toLowerCase(), r]))
  const names = [...byName.keys()].sort((a, b) => b.length - a.length).map(n => n.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'))
  return {
    re: new RegExp(`(^|[\\s(])@(${names.join('|')})(?![A-Za-z0-9_-]|\\.[A-Za-z0-9_-])`, 'gi'),
    build(m) {
      const role = byName.get((m[2] ?? '').toLowerCase())
      const attrs: Record<string, string> = { class: role?.me ? 'md-mention md-mention-role md-mention-me' : 'md-mention md-mention-role' }
      if (role?.color && HEX_COLOR.test(role.color)) attrs['data-role-color'] = role.color
      return [...text(m[1] ?? ''), { t: 'el', tag: 'span', attrs, c: text(`@${m[2] ?? ''}`) }]
    }
  }
}
