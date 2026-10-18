import { test } from 'node:test'
import assert from 'node:assert/strict'
import { checkCoverage, metrics } from './check-coverage.ts'

const root = '/fixture/web'
const values = (covered = 80, total = 100) => Object.fromEntries(metrics.map(metric => [metric, { covered, total }]))
const report = (covered = 80) => ({ total: values(2000 + covered, 2100), '/fixture/web/src/main.ts': values(covered), '/fixture/web/src/lib/support.ts': values(2000, 2000) })
const emptyPolicy = { exceptions: {} }
const exception = {
  reason: 'A concrete limitation reviewed independently in two separate review rounds.',
  author: 'owner', reviewers: ['reviewer-one', 'reviewer-two'],
  minimum: Object.fromEntries(metrics.map(metric => [metric, 60])),
}

test('all four totals and each module must meet the exact threshold', () => {
  assert.deepEqual(checkCoverage(report(), emptyPolicy, root, ['src/main.ts']).failures, [])
  const summary = report(79)
  summary.total = { ...values(95), branches: { covered: 89, total: 100 } }
  assert.equal(checkCoverage(summary, emptyPolicy, root).failures.filter(failure => failure.includes(' < ')).length, 5)
  const belowRounding = report()
  belowRounding.total = values(89999, 100000)
  assert.equal(checkCoverage(belowRounding, emptyPolicy, root).failures.filter(failure => failure.includes(' < ')).length, 4)
})

test('no branch/function opportunities count as complete; uninstrumented executable modules fail', () => {
  const summary = { total: values(), '/fixture/web/src/main.ts': values(0, 0) }
  assert.match(checkCoverage(summary, emptyPolicy, root, ['src/main.ts']).failures.join(), /zero instrumented/)
  assert.match(checkCoverage(report(), emptyPolicy, root, ['src/App.vue']).failures.join(), /missing/)
})

test('individual reviewed exceptions never lower aggregate or hard floor', () => {
  const config = { exceptions: { 'src/main.ts': exception } }
  assert.deepEqual(checkCoverage(report(60), config, root).failures, [])
  assert.equal(checkCoverage(report(59), config, root).failures.length, 4)
  const summary = report(60)
  summary.total = values(60)
  assert.equal(checkCoverage(summary, config, root).failures.filter(failure => failure.includes(' < ')).length, 4)
  assert.throws(() => checkCoverage(report(60), { exceptions: { 'src/main.ts': { ...exception, minimum: { ...exception.minimum, branches: 59 } } } }, root), /at least 60/)
  const branchesOnly = { ...exception, minimum: { statements: 80, branches: 60, functions: 80, lines: 80 } }
  const branchesReport = report()
  branchesReport['/fixture/web/src/main.ts'] = { ...values(), branches: { covered: 60, total: 100 } }
  branchesReport.total = { ...values(2080, 2100), branches: { covered: 2060, total: 2100 } }
  assert.deepEqual(checkCoverage(branchesReport, { exceptions: { 'src/main.ts': branchesOnly } }, root).failures, [])
})

test('exceptions require two distinct independent reviews and precise rationale', () => {
  for (const reviewers of [['owner', 'other'], ['reviewer', 'reviewer'], ['one']]) {
    assert.throws(() => checkCoverage(report(), { exceptions: { 'src/main.ts': { ...exception, reviewers } } }, root), /independent reviewers/)
  }
  assert.throws(() => checkCoverage(report(), { exceptions: { 'src/main.ts': { ...exception, reason: 'hard to test' } } }, root), /rationale/)
  assert.throws(() => checkCoverage(report(), { exceptions: { 'src/*.ts': exception } }, root), /Invalid exception path/)
  assert.match(checkCoverage(report(), { exceptions: { 'src/main.ts': exception } }, root).failures.join(), /stale exception/)
})

test('malformed, empty, duplicate and unexpected coverage inputs fail closed', () => {
  assert.throws(() => checkCoverage({ total: values(0, 0) }, emptyPolicy, root), /no executable/)
  assert.throws(() => checkCoverage({ total: values(), 'src/main.ts': values(101) }, emptyPolicy, root), /invalid coverage/)
  assert.throws(() => checkCoverage({ ...report(), 'src/main.ts': values() }, emptyPolicy, root), /Duplicate/)
  assert.throws(() => checkCoverage({ total: values(), '../outside.ts': values() }, emptyPolicy, root), /Unexpected source/)
})

test('review conditioned on precise counters cannot silently cover a different deficit', () => {
  const config = { exceptions: { 'src/main.ts': { ...exception, approvedCounts: { branches: { covered: 60, total: 100 } } } } }
  assert.deepEqual(checkCoverage(report(60), config, root).failures, [])
  assert.match(checkCoverage(report(61), config, root).failures.join(), /differ from the independently reviewed 60\/100/)
  assert.throws(() => checkCoverage(report(60), { exceptions: { 'src/main.ts': { ...exception, approvedCounts: { invalid: { covered: 60, total: 100 } } } } }, root), /invalid approved metric/)
})
