// Enforce the frontend's measured coverage contract without hiding own sources.
import { readFileSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { executableSources, isTestOrFixture } from './check-coverage-sources.ts'

export const metrics = ['statements', 'branches', 'functions', 'lines'] as const
type Metric = typeof metrics[number]
type Counts = { total: number; covered: number }
type Coverage = Record<Metric, Counts>
type Exception = {
  reason: string
  author: string
  reviewers: string[]
  minimum: Record<Metric, number>
  approvedCounts?: Partial<Record<Metric, Counts>>
}
type Policy = { exceptions: Record<string, Exception> }
type Review = { reason: string; author: string; reviewers: string[] }
const minimum = 80
const totalMinimum = 90
const tableHeader = ['| Scope | Statements | Branches | Functions | Lines |', '|---|---:|---:|---:|---:|']

function record(value: unknown, context: string): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new Error(`${context}: expected object`)
  return value as Record<string, unknown>
}

function countsInRange(total: unknown, covered: unknown, minimumTotal: number): Counts | undefined {
  if (typeof total !== 'number' || !Number.isSafeInteger(total)) return undefined
  if (total < minimumTotal) return undefined
  if (typeof covered !== 'number' || !Number.isSafeInteger(covered)) return undefined
  if (covered < 0 || covered > total) return undefined
  return { total, covered }
}

function metricCounts(entry: Record<string, unknown>, context: string, metric: Metric): Counts {
  const count = record(entry[metric], `${context}.${metric}`)
  const counts = countsInRange(count.total, count.covered, 0)
  if (!counts) throw new Error(`${context}.${metric}: invalid coverage counts`)
  return counts
}

function coverage(value: unknown, context: string): Coverage {
  const entry = record(value, context)
  const counts = {} as Coverage
  for (const metric of metrics) counts[metric] = metricCounts(entry, context, metric)
  return counts
}

function validExceptionPath(path: string): boolean {
  if (!/^src\/.+\.(ts|vue)$/.test(path)) return false
  if (path.includes('..')) return false
  return !path.includes('*')
}

function rejectReview(path: string): never {
  throw new Error(`${path}: exception needs a specific rationale, author and two distinct independent reviewers`)
}

function reviewedReason(path: string, reason: unknown): string {
  if (typeof reason !== 'string') rejectReview(path)
  if (reason.trim().length < 40) rejectReview(path)
  return reason
}

function reviewedAuthor(path: string, author: unknown): string {
  if (typeof author !== 'string') rejectReview(path)
  if (!author.trim()) rejectReview(path)
  return author
}

function isIndependentReviewer(author: string, reviewer: unknown): reviewer is string {
  if (typeof reviewer !== 'string') return false
  if (!reviewer.trim()) return false
  return reviewer !== author
}

function reviewedReviewers(path: string, author: string, reviewers: unknown): string[] {
  if (!Array.isArray(reviewers) || reviewers.length < 2) rejectReview(path)
  const names: string[] = []
  for (const reviewer of reviewers) {
    if (!isIndependentReviewer(author, reviewer)) rejectReview(path)
    names.push(reviewer)
  }
  if (new Set(names).size !== names.length) rejectReview(path)
  return names
}

function reviewedException(path: string, entry: Record<string, unknown>): Review {
  const author = reviewedAuthor(path, entry.author)
  return { reason: reviewedReason(path, entry.reason), author, reviewers: reviewedReviewers(path, author, entry.reviewers) }
}

function assertMinimum(path: string, metric: Metric, value: unknown): asserts value is number {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    throw new Error(`${path}.${metric}: exception minimum must be at least 60 and at most 80`)
  }
  if (value < 60 || value > minimum) throw new Error(`${path}.${metric}: exception minimum must be at least 60 and at most 80`)
}

function lowersAMetric(limits: Record<string, unknown>): boolean {
  for (const metric of metrics) {
    if (limits[metric] !== minimum) return true
  }
  return false
}

function minimumFor(path: string, value: unknown): Record<Metric, number> {
  const limits = record(value, `${path}.minimum`)
  for (const metric of metrics) assertMinimum(path, metric, limits[metric])
  if (!lowersAMetric(limits)) throw new Error(`${path}: exception does not lower any metric`)
  return limits as Record<Metric, number>
}

function isMetric(value: string): value is Metric {
  return (metrics as readonly string[]).includes(value)
}

function approvedCount(path: string, metric: string, raw: unknown): { metric: Metric; counts: Counts } {
  if (!isMetric(metric)) throw new Error(`${path}: invalid approved metric`)
  const value = record(raw, `${path}.approvedCounts.${metric}`)
  const counts = countsInRange(value.total, value.covered, 1)
  if (!counts) throw new Error(`${path}.${metric}: invalid approved counts`)
  return { metric, counts }
}

function readApprovedCounts(path: string, value: unknown): Partial<Record<Metric, Counts>> {
  const approved = record(value, `${path}.approvedCounts`)
  const counts: Partial<Record<Metric, Counts>> = {}
  for (const [metric, raw] of Object.entries(approved)) {
    const parsed = approvedCount(path, metric, raw)
    counts[parsed.metric] = parsed.counts
  }
  return counts
}

function readException(path: string, value: unknown): Exception {
  if (!validExceptionPath(path)) throw new Error(`Invalid exception path: ${path}`)
  const entry = record(value, path)
  const exception: Exception = { ...reviewedException(path, entry), minimum: minimumFor(path, entry.minimum) }
  if (entry.approvedCounts !== undefined) exception.approvedCounts = readApprovedCounts(path, entry.approvedCounts)
  return exception
}

function policy(value: unknown): Policy {
  const entries = record(record(value, 'policy').exceptions, 'policy.exceptions')
  const exceptions: Record<string, Exception> = {}
  for (const [path, entry] of Object.entries(entries)) exceptions[path] = readException(path, entry)
  return { exceptions }
}

function percent(counts: Counts): number {
  if (counts.total === 0) return 100
  return counts.covered * 100 / counts.total
}

function limitsFor(value: number): Record<Metric, number> {
  const limits = {} as Record<Metric, number>
  for (const metric of metrics) limits[metric] = value
  return limits
}

function measuredTotal(files: Map<string, Coverage>, metric: Metric): Counts {
  let covered = 0
  let total = 0
  for (const values of files.values()) {
    covered += values[metric].covered
    total += values[metric].total
  }
  return { covered, total }
}

function totalsDiffer(measured: Counts, reported: Counts): boolean {
  if (measured.covered !== reported.covered) return true
  return measured.total !== reported.total
}

function mismatchedTotals(files: Map<string, Coverage>, total: Coverage): string[] {
  const failures: string[] = []
  for (const metric of metrics) {
    if (totalsDiffer(measuredTotal(files, metric), total[metric])) {
      failures.push(`total: ${metric} counts do not match the measured modules`)
    }
  }
  return failures
}

function coveragePath(webRoot: string, name: string): string {
  return relative(webRoot, resolve(webRoot, name)).replaceAll('\\', '/')
}

function isReportedSource(path: string): boolean {
  if (!path.startsWith('src/')) return false
  if (path.startsWith('src/third_party/')) return false
  if (path === 'src/test-setup.ts') return false
  if (isTestOrFixture(path)) return false
  return !path.endsWith('.d.ts')
}

function addModule(files: Map<string, Coverage>, webRoot: string, name: string, value: unknown): void {
  if (name === 'total') return
  const path = coveragePath(webRoot, name)
  if (!isReportedSource(path)) throw new Error(`Unexpected source in coverage report: ${name}`)
  if (files.has(path)) throw new Error(`Duplicate source in coverage report: ${path}`)
  files.set(path, coverage(value, path))
}

function readModules(summary: Record<string, unknown>, webRoot: string): Map<string, Coverage> {
  const files = new Map<string, Coverage>()
  for (const [name, value] of Object.entries(summary)) addModule(files, webRoot, name, value)
  return files
}

function assertExecutable(files: Map<string, Coverage>, total: Coverage): void {
  if (!files.size) throw new Error('Coverage report contains no executable frontend source')
  if (metrics.some(metric => total[metric].total > 0)) return
  throw new Error('Coverage report contains no executable frontend source')
}

function collectFloorFailures(name: string, values: Coverage, limits: Record<Metric, number>, report: { messages: string[]; belowFloor: string[] }): void {
  for (const metric of metrics) {
    const ratio = percent(values[metric])
    if (ratio >= limits[metric]) continue
    report.messages.push(`${name}: ${metric} ${ratio.toFixed(4)}% < ${limits[metric]}%`)
    if (name !== 'total') report.belowFloor.push(name)
  }
}

function sortedFiles(files: Map<string, Coverage>): [string, Coverage][] {
  return [...files].sort(([left], [right]) => left.localeCompare(right))
}

function floorFailures(
  files: Map<string, Coverage>,
  total: Coverage,
  exceptions: Record<string, Exception>,
): { messages: string[]; belowFloor: string[] } {
  const report = { messages: mismatchedTotals(files, total), belowFloor: [] as string[] }
  const fileLimits = limitsFor(minimum)
  collectFloorFailures('total', total, limitsFor(totalMinimum), report)
  for (const [path, values] of sortedFiles(files)) {
    collectFloorFailures(path, values, exceptions[path]?.minimum ?? fileLimits, report)
  }
  return report
}

function cell(values: Coverage, metric: Metric): string {
  const counts = values[metric]
  return `${percent(counts).toFixed(2)} % (${counts.covered}/${counts.total})`
}

function row(name: string, values: Coverage): string {
  return `| ${name} | ${metrics.map(metric => cell(values, metric)).join(' | ')} |`
}

function lowestPercent(values: Coverage): number {
  const scored = metrics.filter(metric => values[metric].total > 0).map(metric => percent(values[metric]))
  if (!scored.length) return Infinity
  return Math.min(...scored)
}

function lowestFiles(files: Map<string, Coverage>): [string, Coverage][] {
  return [...files].sort(([, left], [, right]) => lowestPercent(left) - lowestPercent(right)).slice(0, 10)
}

function gateStatus(failures: string[]): string {
  if (failures.length === 0) return 'pass'
  return 'fail'
}

function floorLine(belowFloor: string[]): string {
  if (!belowFloor.length) return `No file is below the ${minimum} % floor.`
  const listed = [...new Set(belowFloor)].map(path => `\`${path}\``).join(', ')
  return `Files below the ${minimum} % floor: ${listed}`
}

function coverageMarkdown(total: Coverage, files: Map<string, Coverage>, failures: string[], belowFloor: string[]): string {
  return [
    '### Frontend coverage',
    '',
    `Gate: at least ${totalMinimum} % in total and ${minimum} % in every file (statements, branches, functions, lines): **${gateStatus(failures)}**`,
    '',
    ...tableHeader,
    row('total', total),
    '',
    '<details><summary>Lowest covered files</summary>',
    '',
    ...tableHeader,
    ...lowestFiles(files).map(([path, values]) => row(path, values)),
    '',
    '</details>',
    '',
    floorLine(belowFloor),
  ].join('\n')
}

function uninstrumented(path: string, values: Coverage | undefined): string | undefined {
  if (!values) return `${path}: own executable source missing from coverage report`
  if (values.statements.total !== 0) return undefined
  if (values.lines.total !== 0) return undefined
  return `${path}: executable source has zero instrumented statements and lines`
}

function expectedFailures(expectedSources: string[], files: Map<string, Coverage>): string[] {
  const failures: string[] = []
  for (const path of expectedSources) {
    const failure = uninstrumented(path, files.get(path))
    if (failure) failures.push(failure)
  }
  return failures
}

function meetsNormalThreshold(values: Coverage): boolean {
  for (const metric of metrics) {
    if (percent(values[metric]) < minimum) return false
  }
  return true
}

function countDrift(path: string, metric: Metric, expected: Counts): string {
  return `${path}: ${metric} counts differ from the independently reviewed ${expected.covered}/${expected.total}`
}

function approvedDrift(path: string, values: Coverage, approved: Partial<Record<Metric, Counts>> | undefined): string[] {
  if (!approved) return []
  const failures: string[] = []
  for (const metric of metrics) {
    const expected = approved[metric]
    if (!expected) continue
    if (values[metric].covered !== expected.covered) failures.push(countDrift(path, metric, expected))
    else if (values[metric].total !== expected.total) failures.push(countDrift(path, metric, expected))
  }
  return failures
}

function exceptionFailures(path: string, values: Coverage | undefined, approved: Partial<Record<Metric, Counts>> | undefined): string[] {
  const failures: string[] = []
  if (!values) failures.push(`${path}: exception has no measured module`)
  else if (meetsNormalThreshold(values)) failures.push(`${path}: stale exception; module meets the normal threshold`)
  if (values) failures.push(...approvedDrift(path, values, approved))
  return failures
}

function reviewFailures(exceptions: Record<string, Exception>, files: Map<string, Coverage>): string[] {
  const failures: string[] = []
  for (const path of Object.keys(exceptions)) {
    failures.push(...exceptionFailures(path, files.get(path), exceptions[path]?.approvedCounts))
  }
  return failures
}

export function checkCoverage(rawSummary: unknown, rawPolicy: unknown, webRoot: string, expectedSources: string[] = []): { failures: string[]; markdown: string } {
  const summary = record(rawSummary, 'summary')
  const total = coverage(summary.total, 'total')
  const exceptions = policy(rawPolicy).exceptions
  const files = readModules(summary, webRoot)
  assertExecutable(files, total)
  const gate = floorFailures(files, total, exceptions)
  const markdown = coverageMarkdown(total, files, gate.messages, gate.belowFloor)
  gate.messages.push(...expectedFailures(expectedSources, files))
  gate.messages.push(...reviewFailures(exceptions, files))
  return { failures: gate.messages, markdown }
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
