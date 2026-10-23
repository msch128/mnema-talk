import { readdirSync, readFileSync } from 'node:fs'
import { join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import ts from 'typescript'
import { parse } from '@vue/compiler-sfc'

export interface Tailwind3Violation {
  file: string
  line: number
  className: string
  replacement: string
}

const VALID_CLASS = /^-?[a-zA-Z][a-zA-Z0-9/_.-]*(?:\[[^\]]+\])?$/

function parseClassTokens(raw: string): { variants: string[]; base: string; hasImportant: boolean } | null {
  let hasImportant = false
  let token = raw
  if (token.startsWith('!')) {
    hasImportant = true
    token = token.slice(1)
  }

  const parts = token.split(':')
  const variants: string[] = []
  for (let i = 0; i < parts.length - 1; i++) {
    let v = parts[i]
    if (v === undefined) return null
    if (v.startsWith('!')) {
      hasImportant = true
      v = v.slice(1)
    }
    if (!/^[a-zA-Z0-9_-]+$/.test(v)) return null
    variants.push(v)
  }

  let base = parts[parts.length - 1]
  if (base === undefined) return null
  if (base.startsWith('!')) {
    hasImportant = true
    base = base.slice(1)
  }

  if (!VALID_CLASS.test(base)) return null
  return { variants, base, hasImportant }
}

function resolveReplacement(base: string): string | null {
  if (base === 'outline-none') return 'outline-hidden'
  if (base === 'flex-shrink-0') return 'shrink-0'
  if (base === 'flex-shrink') return 'shrink'
  if (base === 'flex-grow') return 'grow'
  if (base === 'flex-grow-0') return 'grow-0'
  if (base === 'shadow-sm') return 'shadow-xs'
  if (base === 'overflow-ellipsis') return 'text-ellipsis'
  if (base === 'decoration-slice') return 'box-decoration-slice'
  if (base === 'decoration-clone') return 'box-decoration-clone'

  const opacityMatch = base.match(/^(bg|text|border)-opacity-(.+)$/)
  if (opacityMatch) {
    return `${opacityMatch[1]}-<color>/${opacityMatch[2]}`
  }
  return null
}

export function matchTailwind3Class(className: string): string | null {
  if (!className || typeof className !== 'string') return null
  const trimmed = className.trim()
  if (!trimmed) return null

  const parsed = parseClassTokens(trimmed)
  if (!parsed) return null
  const { variants, base, hasImportant } = parsed

  if (base === 'rounded-sm' && !hasImportant) return null

  const replacementBase = resolveReplacement(base)
  if (replacementBase) {
    const prefix = variants.length ? `${variants.join(':')}:` : ''
    return `${prefix}${replacementBase}${hasImportant ? '!' : ''}`
  }

  if (hasImportant) {
    const prefix = variants.length ? `${variants.join(':')}:` : ''
    return `${prefix}${base}!`
  }

  return null
}

function checkStringLiteral(
  text: string,
  startPos: number,
  sf: ts.SourceFile,
  file: string,
  baseLine: number,
  violations: Tailwind3Violation[]
): void {
  for (const token of text.split(/\s+/)) {
    const replacement = matchTailwind3Class(token)
    if (replacement) {
      const { line } = sf.getLineAndCharacterOfPosition(startPos)
      violations.push({
        file,
        line: baseLine + line,
        className: token,
        replacement
      })
    }
  }
}

function scanTypeScriptSource(
  code: string,
  file: string,
  baseLine: number,
  violations: Tailwind3Violation[]
): void {
  const sf = ts.createSourceFile(file, code, ts.ScriptTarget.Latest, true)
  function visit(node: ts.Node): void {
    if (
      ts.isStringLiteral(node) ||
      ts.isNoSubstitutionTemplateLiteral(node) ||
      ts.isTemplateHead(node) ||
      ts.isTemplateMiddle(node) ||
      ts.isTemplateTail(node)
    ) {
      checkStringLiteral(node.text, node.getStart(), sf, file, baseLine, violations)
    }
    ts.forEachChild(node, visit)
  }
  visit(sf)
}

function scanVueTemplate(
  templateAst: unknown,
  file: string,
  violations: Tailwind3Violation[]
): void {
  function checkElement(node: unknown): void {
    if (!node || typeof node !== 'object') return
    const record = node as { props?: unknown[]; children?: unknown[] }
    if (Array.isArray(record.props)) {
      for (const p of record.props as { type: number; name?: string; value?: { content: string }; exp?: { content: string }; loc: { start: { line: number } }; arg?: { content: string } }[]) {
        if (p.type === 6 && p.name === 'class' && p.value?.content) {
          for (const token of p.value.content.split(/\s+/)) {
            const replacement = matchTailwind3Class(token)
            if (replacement) {
              violations.push({ file, line: p.loc.start.line, className: token, replacement })
            }
          }
        } else if (p.type === 7 && p.name === 'bind' && p.arg?.content === 'class' && p.exp?.content) {
          scanTypeScriptSource(`(${p.exp.content})`, file, p.loc.start.line, violations)
        }
      }
    }
    if (Array.isArray(record.children)) {
      for (const child of record.children) {
        checkElement(child)
      }
    }
  }
  checkElement(templateAst)
}

export function scanFile(filePath: string, content: string): Tailwind3Violation[] {
  const violations: Tailwind3Violation[] = []
  if (filePath.endsWith('.ts')) {
    scanTypeScriptSource(content, filePath, 1, violations)
  } else if (filePath.endsWith('.vue')) {
    const { descriptor } = parse(content, { filename: filePath })
    for (const block of [descriptor.script, descriptor.scriptSetup]) {
      if (block?.content) {
        scanTypeScriptSource(block.content, filePath, block.loc.start.line, violations)
      }
    }
    if (descriptor.template?.ast) {
      scanVueTemplate(descriptor.template.ast, filePath, violations)
    }
  }
  return violations
}

function isProductionSource(name: string): boolean {
  if (!name.endsWith('.vue') && !name.endsWith('.ts')) return false
  if (name.endsWith('.d.ts') || name.endsWith('.test.ts') || name.endsWith('.fixture.ts')) return false
  if (name === 'test-setup.ts') return false
  return true
}

function walk(dir: string, files: string[]): void {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === 'node_modules' || entry.name === '.git' || entry.name === 'dist') continue
    const full = join(dir, entry.name)
    if (entry.isDirectory()) {
      walk(full, files)
    } else if (isProductionSource(entry.name)) {
      files.push(full)
    }
  }
}

export function runCheck(srcDir: string): Tailwind3Violation[] {
  const files: string[] = []
  walk(srcDir, files)
  const allViolations: Tailwind3Violation[] = []
  for (const file of files) {
    const content = readFileSync(file, 'utf8')
    const violations = scanFile(file, content)
    allViolations.push(...violations)
  }
  return allViolations
}

const isMain = process.argv[1] && fileURLToPath(import.meta.url) === resolve(process.argv[1])
if (isMain) {
  const webRoot = fileURLToPath(new URL('..', import.meta.url))
  const srcDir = resolve(webRoot, 'src')
  const violations = runCheck(srcDir)
  if (violations.length > 0) {
    for (const v of violations) {
      const displayPath = relative(process.cwd(), v.file).replaceAll('\\', '/')
      console.error(`${displayPath}:${v.line}: deprecated Tailwind 3 class "${v.className}", use "${v.replacement}"`)
    }
    console.error(`Tailwind 3 guard: ${violations.length} forbidden class name(s) found`)
    process.exitCode = 1
  }
}
