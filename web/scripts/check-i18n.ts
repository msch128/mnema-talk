// i18n guard: user-visible text must go through t() / $t() and the locale
// files. Scans every .vue template and every non-test source file and reports
//   text      static text nodes in templates
//   attr      static or string-literal title/aria-label/placeholder/alt/... attributes
//   expr      string literals inside template interpolations and bound user attributes
//   literal   English or German prose in script code (dialogs, toasts, state shown to the user)
//   error     `new Error('prose')` messages: a note only. Callers that show error.message must translate; these are diagnostics
//   format    date/number formatting without an explicit locale
//   plural    plural by string concatenation (`n === 1 ? 'x' : 'xs'`, `+ 's'`)
//   parity    a key present in only one of de.json / en.json
//   unused    a key no source mentions (a note only; keys built at run time count by their prefix)
//   missing   a literal t('key') that neither locale defines
// Run `node scripts/check-i18n.ts`. Parity and missing keys always fail. The text kinds
// (text, attr, expr, literal, format, plural) only warn until `--strict` is passed,
// which fails on any of them; `error` and `unused` stay notes either way and are
// listed only with `--notes`.
// A line can opt out with a trailing `// i18n-ignore: reason` (or `<!-- i18n-ignore: reason -->`).
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'
import ts from 'typescript'
import { parse } from '@vue/compiler-sfc'

export type Kind = 'text' | 'attr' | 'expr' | 'literal' | 'error' | 'format' | 'plural' | 'parity' | 'missing' | 'unused'
export interface Finding { file: string; line: number; kind: Kind; text: string }

// Attributes whose value is read by people (screen readers included).
const USER_ATTRS = new Set(['title', 'aria-label', 'aria-description', 'aria-placeholder', 'aria-roledescription', 'aria-valuetext', 'placeholder', 'alt', 'label', 'content', 'summary', 'description'])
// Text that needs no translation: brand names, units, key names, protocol names.
const ALLOWED_TEXT = new Set([
  'Mnema Talk', 'Mnema', 'Talk', 'M', 'ms', 'MB', 'kB', 'KB', 'GB', 'kbit/s', 'kbps', 'Mbit/s', 'Ø', 'Esc', 'Enter', 'Tab', 'Shift', 'Ctrl', 'Alt', 'Cmd',
  'Opus', 'WebRTC', 'DTLS-SRTP', 'min', 'max', 'px', 'fps', 'Hz', 'dB', 'GIF', 'Tenor', 'GIPHY', 'Markdown', 'Unicode', 'JSON', 'CSV', 'HTTPS', 'URL', 'OK'
])
const LETTERS = /\p{L}{2,}/u
// SPDX-style license names are names, not copy.
const LICENSE = /^(?:[\w.+-]+(?: (?:OR|AND|WITH) [\w.+-]+)+|[\w.+-]+ License)$/
const TECHNICAL = /^(?:[a-z][\w-]*(?:\.[\w-]+)+$|https?:\/\/|#[a-z0-9]{3,8}$|[\w.+-]+@[\w.-]+)/i
const IGNORE = /i18n-ignore/

export function hasWords(text: string): boolean {
  const stripped = text.replace(/&[#\w]+;/g, ' ').trim()
  return LETTERS.test(stripped) && !ALLOWED_TEXT.has(stripped) && !TECHNICAL.test(stripped)
}

// English or German prose: two or more words, a capital start or closing punctuation.
export function looksLikeProse(text: string): boolean {
  const s = text.trim()
  if (ALLOWED_TEXT.has(s) || LICENSE.test(s)) return false
  if (!/\p{L}{2,}[ ]+\p{L}{2,}/u.test(s)) return false
  if (/[<>{}=;\\/|@#$%^*_~`[\]]/.test(s) || /^\w+:\/\//.test(s)) return false
  if (/^[a-z]+(?:[ -][a-z]+)*$/.test(s) && !/[.!?]$/.test(s)) return false
  return /^\p{Lu}/u.test(s) || /[.!?:…]$/.test(s)
}

function lineOf(source: string, offset: number): number {
  let n = 1
  for (let i = 0; i < offset && i < source.length; i++) if (source.charCodeAt(i) === 10) n++
  return n
}

function ignored(source: string, offset: number): boolean {
  const start = source.lastIndexOf('\n', offset - 1) + 1
  let end = source.indexOf('\n', offset)
  if (end < 0) end = source.length
  return IGNORE.test(source.slice(start, end))
}

// ---- script ---------------------------------------------------------------

const QUIET_CALLEES = /^(console\.\w+|t|\$t|i18n\.t|querySelector(All)?|matches|closest|getElementById|addEventListener|removeEventListener|dispatchEvent|setAttribute|getAttribute|removeAttribute|require|import|describe|it|test|expect|assert\.\w+|emit|debug|warn|log|info|fetch|api|apiFetch|encodeURIComponent|JSON\.\w+|Symbol|RegExp|URL|Error|TypeError|RangeError)$/
const NON_UI_KEYS = /^(class|className|style|id|name|type|key|href|src|url|path|method|selector|event|kind|role|mode|rel|target|accept|pattern|regex|code|reason|font|filter|query|header|cache|origin|sdp|codec|mimeType|content-type|Content-Type|Authorization|channel|topic)$/

function calleeName(node: ts.Node): string {
  if (ts.isCallExpression(node) || ts.isNewExpression(node)) {
    const e = node.expression
    if (ts.isIdentifier(e)) return e.text
    if (ts.isPropertyAccessExpression(e)) return `${calleeName2(e.expression)}.${e.name.text}`
  }
  return ''
}
function calleeName2(e: ts.Expression): string {
  if (ts.isIdentifier(e)) return e.text
  if (ts.isPropertyAccessExpression(e)) return `${calleeName2(e.expression)}.${e.name.text}`
  return '?'
}

function insideQuiet(node: ts.Node): boolean {
  for (let p: ts.Node | undefined = node.parent; p; p = p.parent) {
    if (ts.isImportDeclaration(p) || ts.isExportDeclaration(p) || ts.isLiteralTypeNode(p) || ts.isTypeNode(p) || ts.isCaseClause(p) || ts.isTaggedTemplateExpression(p)) return true
    if ((ts.isCallExpression(p) || ts.isNewExpression(p)) && QUIET_CALLEES.test(calleeName(p)) && calleeName(p) !== 'Error') return true
    if (ts.isBinaryExpression(p) && [ts.SyntaxKind.EqualsEqualsEqualsToken, ts.SyntaxKind.ExclamationEqualsEqualsToken, ts.SyntaxKind.EqualsEqualsToken, ts.SyntaxKind.ExclamationEqualsToken].includes(p.operatorToken.kind)) return true
    if (ts.isPropertyAssignment(p) && NON_UI_KEYS.test(p.name.getText())) return true
    if (ts.isFunctionLike(p) || ts.isClassLike(p)) return false
  }
  return false
}

// Single capitalised words are only suspicious where the app shows them: a
// toast call, a confirm() dialog field or a property that is plainly UI copy.
const UI_KEYS = /^(title|body|label|placeholder|tooltip|ariaLabel|description|message|text|hint|caption|heading|confirmLabel|cancelLabel|emptyText|detail)$/
const TOAST_CALL = /^(toasts?|useToasts\(\))\.\w+$|^(confirm|notify|toast|alert)$/
function inUiSink(node: ts.Node): boolean {
  const p = node.parent
  if (p && ts.isPropertyAssignment(p) && p.initializer === node && UI_KEYS.test(p.name.getText())) {
    // only inside a call (confirm({...}), toasts.error(..., {...})), not a plain data table
    for (let a: ts.Node | undefined = p.parent; a; a = a.parent) if (ts.isCallExpression(a)) return TOAST_CALL.test(calleeName(a))
    return false
  }
  if (p && ts.isCallExpression(p) && p.arguments[0] === node) return TOAST_CALL.test(calleeName(p))
  if (p && ts.isBinaryExpression(p) && p.right === node && p.operatorToken.kind === ts.SyntaxKind.EqualsToken) return /(^|\.)(error|message|status|notice)(Message|Text)?(\.value)?$/.test(p.left.getText())
  return false
}

function literalText(node: ts.Node): string | null {
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) return node.text
  if (ts.isTemplateExpression(node)) return [node.head.text, ...node.templateSpans.map(s => s.literal.text)].join(' ')
  return null
}

function isPlainString(node: ts.Node): boolean {
  return ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)
}

function enclosingNewError(node: ts.Node): boolean {
  const p = node.parent
  return !!p && ts.isNewExpression(p) && ts.isIdentifier(p.expression) && /^(Error|TypeError|RangeError)$/.test(p.expression.text) && p.arguments?.[0] === node
}

// Locale-sensitive formatters called without a locale argument.
const FORMATTERS = new Set(['toLocaleString', 'toLocaleDateString', 'toLocaleTimeString'])

export function scanScript(source: string, file: string, baseOffset = 0, full = source, loose = false): Finding[] {
  const sf = ts.createSourceFile(file.endsWith('.vue') ? `${file}.ts` : file, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
  const out: Finding[] = []
  const add = (node: ts.Node, kind: Kind, text: string) => {
    const offset = baseOffset + node.getStart(sf)
    if (ignored(full, offset)) return
    out.push({ file, line: lineOf(full, offset), kind, text: text.replace(/\s+/g, ' ').slice(0, 100) })
  }
  const visit = (node: ts.Node): void => {
    const text = literalText(node)
    if (text !== null && !ts.isTemplateHead(node)) {
      const prose = looksLikeProse(text)
      if (loose && hasWords(text) && !insideQuiet(node)) add(node, 'expr', text)
      else if (prose && enclosingNewError(node)) add(node, 'error', text)
      else if (prose && !insideQuiet(node)) add(node, 'literal', text)
      else if (!prose && hasWords(text) && /^\p{Lu}/u.test(text.trim()) && inUiSink(node)) add(node, 'literal', text)
    }
    if (ts.isCallExpression(node) && ts.isPropertyAccessExpression(node.expression)) {
      const name = node.expression.name.text
      if (FORMATTERS.has(name) && node.arguments.length === 0) add(node, 'format', `${name}() without a locale`)
      if (name === 'toLocaleUpperCase' || name === 'toLocaleLowerCase') { /* locale is optional and harmless */ }
    }
    if (ts.isNewExpression(node) && ts.isPropertyAccessExpression(node.expression) && ts.isIdentifier(node.expression.expression) && node.expression.expression.text === 'Intl'
      && /^(DateTimeFormat|NumberFormat|RelativeTimeFormat|ListFormat|PluralRules)$/.test(node.expression.name.text) && (node.arguments?.length ?? 0) === 0) {
      add(node, 'format', `Intl.${node.expression.name.text}() without a locale`)
    }
    // n === 1 ? 'item' : 'items'   |   count !== 1 ? 's' : ''   |   + 's'
    if (ts.isConditionalExpression(node) && /(===|!==|==|!=|>|<=)\s*1\b|\b1\s*(===|!==)/.test(node.condition.getText(sf))
      && isPlainString(node.whenTrue) && isPlainString(node.whenFalse)
      && (hasWords(literalText(node.whenTrue)!) || hasWords(literalText(node.whenFalse)!))) {
      add(node, 'plural', node.getText(sf))
    }
    if (ts.isBinaryExpression(node) && node.operatorToken.kind === ts.SyntaxKind.PlusToken) {
      const r = literalText(node.right)
      if (r !== null && /^(s|n|en|es)$/.test(r) && /count|length|\bn\b|total|num/i.test(node.left.getText(sf))) add(node, 'plural', node.getText(sf))
    }
    ts.forEachChild(node, visit)
  }
  visit(sf)
  return out
}

// ---- template -------------------------------------------------------------

interface TplNode { type: number; tag?: string; content?: unknown; props?: TplProp[]; children?: TplNode[]; loc: { start: { offset: number }; source: string } }
interface TplProp { type: number; name: string; value?: { content: string }; arg?: { content?: string }; exp?: { content: string; loc: { start: { offset: number } } }; rawName?: string; loc: { start: { offset: number } } }

export function scanVue(source: string, file: string): Finding[] {
  const { descriptor } = parse(source, { filename: file })
  const out: Finding[] = []
  const add = (offset: number, kind: Kind, text: string) => {
    if (ignored(source, offset)) return
    out.push({ file, line: lineOf(source, offset), kind, text: text.replace(/\s+/g, ' ').slice(0, 100) })
  }
  const walk = (node: TplNode): void => {
    if (node.type === 2) { // text
      const text = String(node.content ?? '').trim()
      if (hasWords(text)) add(node.loc.start.offset, 'text', text)
    } else if (node.type === 5) { // interpolation: {{ expr }}
      const inner = node.content as { content?: string; loc?: { start: { offset: number } } }
      if (inner.content != null && inner.loc) for (const f of scanScript(`(${inner.content})`, file, inner.loc.start.offset - 1, source, true)) out.push(f)
    }
    for (const prop of node.props ?? []) {
      if (prop.type === 6) { // static attribute
        const v = prop.value?.content ?? ''
        if (USER_ATTRS.has(prop.name) && hasWords(v) && !(node.tag === 'meta')) add(prop.loc.start.offset, 'attr', `${prop.name}="${v}"`)
      } else if (prop.type === 7 && prop.exp) { // directive
        const arg = prop.arg?.content
        const bound = prop.name === 'bind' && arg && USER_ATTRS.has(arg)
        if (bound) {
          out.push(...scanScript(`(${prop.exp.content})`, file, prop.exp.loc.start.offset - 1, source, true))
        } else if (prop.name !== 'bind' && prop.name !== 'for' && prop.name !== 'slot') {
          for (const f of scanScript(`(${prop.exp.content})`, file, prop.exp.loc.start.offset - 1, source)) out.push(f)
        }
      }
    }
    for (const child of node.children ?? []) walk(child)
  }
  if (descriptor.template?.ast) walk(descriptor.template.ast as unknown as TplNode)
  for (const block of [descriptor.script, descriptor.scriptSetup]) {
    if (block) out.push(...scanScript(block.content, file, block.loc.start.offset, source))
  }
  return out
}

// Interpolations and bound attrs also go through scanScript above, which
// reports 'literal'; fold both into the template kinds callers expect.
export function scanSource(source: string, file: string): Finding[] {
  return file.endsWith('.vue') ? scanVue(source, file) : scanScript(source, file)
}

// ---- keys -------------------------------------------------------------------

type Tree = { [k: string]: string | Tree }

export function leafKeys(tree: Tree, prefix = ''): string[] {
  const out: string[] = []
  for (const [k, v] of Object.entries(tree)) {
    const path = prefix ? `${prefix}.${k}` : k
    if (typeof v === 'string') out.push(path)
    else if ('other' in v && Object.values(v).every(x => typeof x === 'string')) out.push(path)
    else out.push(...leafKeys(v, path))
  }
  return out
}

export function parityFindings(de: Tree, en: Tree): Finding[] {
  const d = new Set(leafKeys(de)); const e = new Set(leafKeys(en))
  const f = (name: string, key: string): Finding => ({ file: name, line: 0, kind: 'parity', text: key })
  return [...[...d].filter(k => !e.has(k)).map(k => f('en.json missing', k)), ...[...e].filter(k => !d.has(k)).map(k => f('de.json missing', k))]
}

const CALL = /(?<![\w$.])\$?t\(\s*(['"`])([A-Za-z][\w-]*(?:\.[\w-]+)+)\1/g

export function usedKeys(sources: Record<string, string>): Map<string, string> {
  const used = new Map<string, string>()
  for (const [file, src] of Object.entries(sources)) for (const m of src.matchAll(CALL)) if (!used.has(m[2]!)) used.set(m[2]!, file)
  return used
}

export function missingFindings(used: Map<string, string>, de: Tree, en: Tree): Finding[] {
  const defined = [new Set(leafKeys(de)), new Set(leafKeys(en))]
  return [...used].filter(([k]) => defined.some(s => !s.has(k))).map(([key, file]) => ({ file, line: 0, kind: 'missing' as const, text: key }))
}

// Keys whose full dotted name, or the static prefix of a built name
// (t(`nav.${reason}`)), appears nowhere in the sources. Only a note: a key can
// be reached through a table of names this scan cannot see.
export function unusedFindings(sources: Record<string, string>, de: Tree): Finding[] {
  const all = Object.values(sources).join('\n')
  const prefixes = new Set([...all.matchAll(/`([A-Za-z][\w.-]*[._])\$\{/g)].map(m => m[1]!))
  const quoted = new Set([...all.matchAll(/['"`]([A-Za-z][\w-]*(?:\.[\w-]+)+)['"`]/g)].map(m => m[1]!))
  return leafKeys(de)
    .filter(k => !quoted.has(k) && ![...prefixes].some(p => k.startsWith(p)) && !all.includes(k))
    .map(k => ({ file: 'de.json', line: 0, kind: 'unused' as const, text: k }))
}

// ---- driver -----------------------------------------------------------------

function walkDir(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) { if (name !== 'third_party' && name !== 'node_modules') walkDir(p, out) } else out.push(p)
  }
  return out
}

const SKIP = /\.(test|fixture|spec)\.ts$|errorSentences[A-C]?\.ts$|\.d\.ts$|\/i18n\/|\/sw\/|\.generated\./

export function scanTree(srcDir: string): Finding[] {
  const out: Finding[] = []
  const sources: Record<string, string> = {}
  for (const path of walkDir(srcDir).sort()) {
    const rel = relative(join(srcDir, '..'), path)
    if (!/\.(vue|ts)$/.test(path) || /\.(test|fixture)\.ts$|\.d\.ts$/.test(path)) continue
    const src = readFileSync(path, 'utf8')
    sources[rel] = src
    if (SKIP.test(path)) continue
    out.push(...scanSource(src, rel))
  }
  const de = JSON.parse(readFileSync(join(srcDir, 'i18n/de.json'), 'utf8')) as Tree
  const en = JSON.parse(readFileSync(join(srcDir, 'i18n/en.json'), 'utf8')) as Tree
  out.push(...parityFindings(de, en), ...missingFindings(usedKeys(sources), de, en), ...unusedFindings(sources, de))
  return out
}

const NOTES = new Set<Kind>(['error', 'unused'])
const HARD = new Set<Kind>(['parity', 'missing'])

function main(): void {
  const strict = process.argv.includes('--strict')
  const findings = scanTree(fileURLToPath(new URL('../src', import.meta.url)))
  const failing = findings.filter(f => !NOTES.has(f.kind))
  const failed = failing.filter(f => strict || HARD.has(f.kind))
  const notes = process.argv.includes('--notes')
  for (const f of findings.filter(x => notes || !NOTES.has(x.kind))) process.stdout.write(`${f.file}${f.line ? `:${f.line}` : ''}  [${f.kind}]  ${f.text}\n`)
  const counts = new Map<string, number>()
  for (const f of failing) counts.set(f.kind, (counts.get(f.kind) ?? 0) + 1)
  const summary = [...counts].map(([k, n]) => `${k} ${n}`).join(', ') || 'none'
  process.stdout.write(`i18n check: ${failing.length} finding(s) + ${findings.length - failing.length} note(s) (${summary}) [${strict ? 'strict' : 'warn'}]${failed.length > 0 ? ' FAILED' : ''}\n`)
  if (failed.length > 0) process.exitCode = 1
}

if (process.argv[1] === fileURLToPath(import.meta.url)) main()
