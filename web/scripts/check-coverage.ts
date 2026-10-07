// Enforce the frontend's measured coverage contract without hiding own sources.
import { readFileSync, readdirSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import ts from 'typescript'

export const metrics = ['statements', 'branches', 'functions', 'lines'] as const
type Metric = typeof metrics[number]
type Counts = { total: number; covered: number }
type Coverage = Record<Metric, Counts>
type Exception = { reason: string; author: string; reviewers: string[]; minimum: Record<Metric, number>; approvedCounts?: Partial<Record<Metric, Counts>> }
type Policy = { exceptions: Record<string, Exception> }
const minimum = 95

function record(value: unknown, context: string): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error(`${context}: expected object`)
  return value as Record<string, unknown>
}

function coverage(value: unknown, context: string): Coverage {
  const entry = record(value, context)
  return Object.fromEntries(metrics.map(metric => {
    const count = record(entry[metric], `${context}.${metric}`)
    const { total, covered } = count
    if (typeof total !== 'number' || !Number.isSafeInteger(total) || total < 0 ||
        typeof covered !== 'number' || !Number.isSafeInteger(covered) || covered < 0 || covered > total) {
      throw new Error(`${context}.${metric}: invalid coverage counts`)
    }
    return [metric, { total, covered }]
  })) as Coverage
}

function policy(value: unknown): Policy {
  const entries = record(record(value, 'policy').exceptions, 'policy.exceptions')
  const exceptions: Record<string, Exception> = {}
  for (const [path, value] of Object.entries(entries)) {
    if (!/^src\/.+\.(ts|vue)$/.test(path) || path.includes('..') || path.includes('*')) throw new Error(`Invalid exception path: ${path}`)
    const entry = record(value, path)
    const { reason, author, reviewers } = entry
    if (typeof reason !== 'string' || reason.trim().length < 40 || typeof author !== 'string' || !author.trim() ||
        !Array.isArray(reviewers) || reviewers.length < 2 || reviewers.some(r => typeof r !== 'string' || !r.trim() || r === author) ||
        new Set(reviewers).size !== reviewers.length) {
      throw new Error(`${path}: exception needs a specific rationale, author and two distinct independent reviewers`)
    }
    const limits = record(entry.minimum, `${path}.minimum`)
    for (const metric of metrics) {
      if (typeof limits[metric] !== 'number' || !Number.isFinite(limits[metric]) || limits[metric] < 80 || limits[metric] > minimum) {
        throw new Error(`${path}.${metric}: exception minimum must be at least 80 and at most 95`)
      }
    }
    if (metrics.every(metric => limits[metric] === minimum)) throw new Error(`${path}: exception does not lower any metric`)
    const exception: Exception = { reason, author, reviewers: reviewers as string[], minimum: limits as Record<Metric, number> }
    if (entry.approvedCounts !== undefined) {
      const approved = record(entry.approvedCounts, `${path}.approvedCounts`)
      const counts: Partial<Record<Metric, Counts>> = {}
      for (const [metric, raw] of Object.entries(approved)) {
        if (!metrics.includes(metric as Metric)) throw new Error(`${path}: invalid approved metric`)
        const value = record(raw, `${path}.approvedCounts.${metric}`)
        const { total, covered } = value
        if (typeof total !== 'number' || !Number.isSafeInteger(total) || total < 1 || typeof covered !== 'number' || !Number.isSafeInteger(covered) || covered < 0 || covered > total) {
          throw new Error(`${path}.${metric}: invalid approved counts`)
        }
        counts[metric as Metric] = { total, covered }
      }
      exception.approvedCounts = counts
    }
    exceptions[path] = exception
  }
  return { exceptions }
}

const percent = ({ covered, total }: Counts): number => total ? covered * 100 / total : 100

export function checkCoverage(rawSummary: unknown, rawPolicy: unknown, webRoot: string, expectedSources: string[] = []): { failures: string[]; markdown: string } {
  const summary = record(rawSummary, 'summary')
  const total = coverage(summary.total, 'total')
  const { exceptions } = policy(rawPolicy)
  const files = new Map<string, Coverage>()
  for (const [name, value] of Object.entries(summary)) {
    if (name === 'total') continue
    const path = relative(webRoot, resolve(webRoot, name)).replaceAll('\\', '/')
    if (!path.startsWith('src/') || path.startsWith('src/third_party/') || /\.(test|fixture)\.ts$|\.d\.ts$/.test(path) || path === 'src/test-setup.ts') {
      throw new Error(`Unexpected source in coverage report: ${name}`)
    }
    if (files.has(path)) throw new Error(`Duplicate source in coverage report: ${path}`)
    files.set(path, coverage(value, path))
  }
  if (!files.size || !metrics.some(metric => total[metric].total > 0)) throw new Error('Coverage report contains no executable frontend source')
  const failures: string[] = []
  for (const metric of metrics) {
    const measured = [...files.values()].reduce((counts, values) => ({
      covered: counts.covered + values[metric].covered,
      total: counts.total + values[metric].total,
    }), { covered: 0, total: 0 })
    if (measured.covered !== total[metric].covered || measured.total !== total[metric].total) {
      failures.push(`total: ${metric} counts do not match the measured modules`)
    }
  }
  const markdown = ['### Frontend coverage', '', '| Scope | Statements | Branches | Functions | Lines |', '|---|---:|---:|---:|---:|']
  function inspect(name: string, values: Coverage, limits: Record<Metric, number>): void {
    markdown.push(`| ${name} | ${metrics.map(metric => `${percent(values[metric]).toFixed(2)}%`).join(' | ')} |`)
    for (const metric of metrics) {
      if (percent(values[metric]) < limits[metric]) failures.push(`${name}: ${metric} ${percent(values[metric]).toFixed(4)}% < ${limits[metric]}%`)
    }
  }
  const defaultLimits = Object.fromEntries(metrics.map(metric => [metric, minimum])) as Record<Metric, number>
  inspect('total', total, defaultLimits)
  for (const [path, values] of [...files].sort(([a], [b]) => a.localeCompare(b))) inspect(path, values, exceptions[path]?.minimum ?? defaultLimits)
  for (const path of expectedSources) {
    const values = files.get(path)
    if (!values) failures.push(`${path}: own executable source missing from coverage report`)
    else if (values.statements.total === 0 && values.lines.total === 0) failures.push(`${path}: executable source has zero instrumented statements and lines`)
  }
  for (const path of Object.keys(exceptions)) {
    const values = files.get(path)
    if (!values) failures.push(`${path}: exception has no measured module`)
    else if (metrics.every(metric => percent(values[metric]) >= minimum)) failures.push(`${path}: stale exception; module meets the normal threshold`)
    if (values) {
      const approved = exceptions[path]?.approvedCounts
      for (const metric of metrics) {
        const expected = approved?.[metric]
        if (expected && (values[metric].covered !== expected.covered || values[metric].total !== expected.total)) failures.push(`${path}: ${metric} counts differ from the independently reviewed ${expected.covered}/${expected.total}`)
      }
    }
  }
  return { failures, markdown: markdown.join('\n') }
}

function executableSources(root: string, directory = join(root, 'src')): string[] {
  const sources: string[] = []
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) {
      if (entry.name !== 'third_party') sources.push(...executableSources(root, path))
      continue
    }
    if (!entry.isFile() || !/\.(ts|vue)$/.test(entry.name) || /\.(test|fixture)\.ts$|\.d\.ts$/.test(entry.name) || entry.name === 'test-setup.ts') continue
    // Type-only modules do not contain executable code to instrument.
    if (entry.name.endsWith('.ts')) {
      const emitted = ts.transpileModule(readFileSync(path, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } }).outputText
      const ast = ts.createSourceFile(path, emitted, ts.ScriptTarget.ES2022, true)
      // Re-export barrels and imports have no instrumentable statement body.
      // They remain in the report, but cannot provide a nonzero hit count.
      if (ast.statements.every(statement => ts.isImportDeclaration(statement) || ts.isExportDeclaration(statement) || ts.isEmptyStatement(statement))) continue
    }
    sources.push(relative(root, path).replaceAll('\\', '/'))
  }
  return sources
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const root = dirname(dirname(fileURLToPath(import.meta.url)))
    const report = JSON.parse(readFileSync(join(root, 'coverage/coverage-summary.json'), 'utf8')) as unknown
    const config = JSON.parse(readFileSync(join(root, 'coverage-policy.json'), 'utf8')) as unknown
    const result = checkCoverage(report, config, root, executableSources(root))
    console.log(result.markdown)
    for (const failure of result.failures) console.error(failure)
    if (result.failures.length) process.exitCode = 1
  } catch (error) {
    console.error(error instanceof Error ? error.message : error)
    process.exitCode = 1
  }
}
