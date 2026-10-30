// Automod rules (internal/automod): the kinds, the ready-made choices of the
// editor, and the conversion between a rule and the lines of its form.
import type { AutomodRule, AutomodRuleInput, AutomodSettings } from '../types/rest'
import { locale, t } from '../i18n'
import presets from './presets.json'

export type AutomodKind = AutomodRule['kind']

/** Every kind; raid protection exists at most once. */
export const AUTOMOD_KINDS: readonly AutomodKind[] = ['words', 'links', 'mentions', 'new_account', 'raid']

/** Ready-made choices, in seconds. */
export const ACCOUNT_AGES = presets.automodAccountAges
export const RAID_WINDOWS = presets.automodRaidWindows
export const RAID_RESTRICTIONS = presets.automodRaidRestrictions
export const RULE_TIMEOUTS = presets.automodRuleTimeouts

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

// The names migration 0066 gave the built-in rules. They are stored in English;
// a rule still carrying one is shown with the title of its kind in the UI language.
const SEEDED_NAMES: Record<AutomodKind, string> = {
  words: 'Blocked words', links: 'Links', mentions: 'Mention spam', new_account: 'New accounts', raid: 'Raid protection'
}

/** The name to show: built-in rules read in the UI language, custom names stay as written. */
export function ruleLabel(rule: Pick<AutomodRule, 'kind' | 'name'>): string {
  return rule.name === SEEDED_NAMES[rule.kind] ? t(`automod.kind.${rule.kind}`) : rule.name
}

/** The same for a rule known only by its stored name (a report keeps the name the rule had). */
export function storedRuleLabel(name: string): string {
  const kind = (Object.keys(SEEDED_NAMES) as AutomodKind[]).find(k => SEEDED_NAMES[k] === name)
  return kind ? t(`automod.kind.${kind}`) : name
}

/** "1 day", "10 minutes": the long form of a duration in the UI language. */
export function longDuration(seconds: number): string {
  const units: Array<[string, number]> = [['week', 604800], ['day', 86400], ['hour', 3600], ['minute', 60]]
  const [unit, size] = units.find(([, size]) => seconds >= size && seconds % size === 0) ?? ['second', 1]
  return new Intl.NumberFormat(locale.value, { style: 'unit', unit, unitDisplay: 'long' }).format(seconds / size)
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
    // The stored name, not its translation: saving must not rename a built-in rule.
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
    case 'links': {
      const mode = c.mode ?? 'block_list'
      const count = c.domains?.length ?? 0
      return t(`automod.summary.links.${mode}${mode !== 'block_all' && count === 0 ? 'Empty' : ''}`, { count })
    }
    case 'mentions': return t('automod.summary.mentions', { n: c.max_mentions ?? 0 })
    case 'new_account': return t(`automod.summary.newAccount.${c.restrict ?? 'links'}`, { age: longDuration(c.max_age_seconds ?? 0) })
    case 'raid': return t('automod.summary.raid', { joins: c.joins ?? 0, window: longDuration(c.window_seconds ?? 0), restrict: longDuration(c.restrict_seconds ?? 0) })
  }
}

/** A choice list that also holds the current value when it is not a ready-made one. */
export function withCurrent(choices: readonly number[], current: number): number[] {
  return choices.includes(current) ? [...choices] : [...choices, current].sort((a, b) => a - b)
}
