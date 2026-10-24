// Automod rules (internal/automod): the kinds, the ready-made choices of the
// editor, and the conversion between a rule and the lines of its form.
import type { AutomodRule, AutomodRuleInput, AutomodSettings } from '../types/rest'
import { durationLabel } from './moderation'
import { t } from '../i18n'

export type AutomodKind = AutomodRule['kind']

/** Every kind; raid protection exists at most once. */
export const AUTOMOD_KINDS: readonly AutomodKind[] = ['words', 'links', 'mentions', 'new_account', 'raid']

/** Ready-made choices, in seconds. */
export const ACCOUNT_AGES = [60, 300, 600, 1800, 3600, 21600, 86400, 259200, 604800, 2592000] as const
export const RAID_WINDOWS = [5, 10, 30, 60, 300, 600] as const
export const RAID_RESTRICTIONS = [60, 300, 600, 1800, 3600, 21600, 86400] as const
export const RULE_TIMEOUTS = [0, 60, 300, 600, 3600, 86400, 604800] as const

export const MAX_RULE_NAME = 60
export const MAX_WORDS = 200
export const MAX_DOMAINS = 100

/** The form of one rule: the lists are the text of a box, one entry per line. */
export interface RuleForm {
  kind: AutomodKind
  name: string
  enabled: boolean
  action: 'block' | 'flag'
  timeoutSeconds: number
  words: string
  mode: 'block_all' | 'allow_list' | 'block_list'
  domains: string
  maxMentions: number
  maxAgeSeconds: number
  restrict: 'links' | 'attachments' | 'links_and_attachments' | 'everything'
  joins: number
  windowSeconds: number
  restrictSeconds: number
  exemptRoles: string[]
  exemptChannels: string[]
}

export function emptyForm(kind: AutomodKind): RuleForm {
  return {
    kind, name: t(`automod.kind.${kind}`), enabled: true, action: kind === 'raid' ? 'flag' : 'block', timeoutSeconds: 0,
    words: '', mode: 'block_list', domains: '', maxMentions: 10, maxAgeSeconds: 600, restrict: 'links',
    joins: 10, windowSeconds: 10, restrictSeconds: 600, exemptRoles: [], exemptChannels: []
  }
}

const lines = (list: readonly string[] | undefined) => (list ?? []).join('\n')
const entries = (text: string) => text.split('\n').map(l => l.trim()).filter(Boolean)

export function formFromRule(rule: AutomodRule): RuleForm {
  const c = rule.config
  const base = emptyForm(rule.kind)
  return {
    ...base, name: rule.name, enabled: rule.enabled, action: rule.action, timeoutSeconds: rule.timeout_seconds,
    words: lines(c.words), mode: c.mode ?? base.mode, domains: lines(c.domains),
    maxMentions: c.max_mentions ?? base.maxMentions, maxAgeSeconds: c.max_age_seconds ?? base.maxAgeSeconds,
    restrict: c.restrict ?? base.restrict, joins: c.joins ?? base.joins, windowSeconds: c.window_seconds ?? base.windowSeconds,
    restrictSeconds: c.restrict_seconds ?? base.restrictSeconds,
    exemptRoles: [...rule.exempt_roles], exemptChannels: [...rule.exempt_channels]
  }
}

function settingsOf(f: RuleForm): AutomodSettings {
  switch (f.kind) {
    case 'words': return { words: entries(f.words) }
    case 'links': return f.mode === 'block_all' ? { mode: f.mode } : { mode: f.mode, domains: entries(f.domains) }
    case 'mentions': return { max_mentions: f.maxMentions }
    case 'new_account': return { max_age_seconds: f.maxAgeSeconds, restrict: f.restrict }
    case 'raid': return { joins: f.joins, window_seconds: f.windowSeconds, restrict_seconds: f.restrictSeconds }
  }
}

export function inputFromForm(f: RuleForm): AutomodRuleInput {
  return {
    kind: f.kind, name: f.name.trim(), enabled: f.enabled,
    action: f.kind === 'raid' ? 'flag' : f.action,
    timeout_seconds: f.kind === 'raid' ? 0 : f.timeoutSeconds,
    config: settingsOf(f), exempt_roles: f.exemptRoles, exempt_channels: f.exemptChannels
  }
}

/** The same rule with another on/off state, as the body that replaces it. */
export function toggled(rule: AutomodRule): AutomodRuleInput {
  return inputFromForm({ ...formFromRule(rule), enabled: !rule.enabled })
}

/** What the form still lacks, as a translated sentence; '' when it can be saved. */
export function formProblem(f: RuleForm): string {
  if (!f.name.trim()) return t('automod.problem.name')
  if (f.kind === 'words' && entries(f.words).length > MAX_WORDS) return t('automod.problem.tooManyWords', { max: MAX_WORDS })
  if (f.kind === 'links' && f.mode !== 'block_all' && entries(f.domains).length > MAX_DOMAINS) return t('automod.problem.tooManyDomains', { max: MAX_DOMAINS })
  if (f.kind === 'mentions' && !(Number.isInteger(f.maxMentions) && f.maxMentions >= 1 && f.maxMentions <= 50)) return t('automod.problem.mentions')
  if (f.kind === 'raid' && !(Number.isInteger(f.joins) && f.joins >= 2 && f.joins <= 200)) return t('automod.problem.joins')
  return ''
}

/** One line that says what a rule does. */
export function ruleSummary(rule: AutomodRule): string {
  const c = rule.config
  switch (rule.kind) {
    case 'words': return t('automod.summary.words', { count: c.words?.length ?? 0 })
    case 'links': return t(`automod.summary.links.${c.mode ?? 'block_list'}`, { count: c.domains?.length ?? 0 })
    case 'mentions': return t('automod.summary.mentions', { n: c.max_mentions ?? 0 })
    case 'new_account': return t(`automod.summary.newAccount.${c.restrict ?? 'links'}`, { age: durationLabel(c.max_age_seconds ?? 0) })
    case 'raid': return t('automod.summary.raid', { joins: c.joins ?? 0, window: durationLabel(c.window_seconds ?? 0), restrict: durationLabel(c.restrict_seconds ?? 0) })
  }
}

/** A choice list that also holds the current value when it is not a ready-made one. */
export function withCurrent(choices: readonly number[], current: number): number[] {
  return choices.includes(current) ? [...choices] : [...choices, current].sort((a, b) => a - b)
}
