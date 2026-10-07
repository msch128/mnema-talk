# Frontend TypeScript contract

All first-party frontend source, Vue script blocks, browser tests, fixtures,
workers and build tools use TypeScript. The production Go binary still embeds
Vite's compiled JavaScript and static assets. TypeScript introduces no runtime
service and does not provide encryption by itself.

## Checks

From the repository root, `make typecheck-web` checks source completeness and
then the app, Vitest tests, Node build tools, dedicated worker and audio worklet.
`make contracts-check` verifies generated REST declarations against the
committed OpenAPI document. Both are part of `make check` and CI. From `web`,
use `npm run typecheck` and `npm run contracts:check`. Browser test configuration
also has its own strict Node/DOM compiler project in `e2e`.

`strict`, `noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`,
`useUnknownInCatchVariables`, `noFallthroughCasesInSwitch` and
`verbatimModuleSyntax` are enabled. Runtime app source does not use `allowJs`.
The source check rejects first-party `.js`, `.mjs`, `.cjs`, `.jsx` and Vue script
blocks without `lang="ts"`; this prevents a superficially green `vue-tsc` run
that silently skips the remaining JavaScript. Explicit `any`, `ts-ignore`,
`ts-nocheck` and `ts-expect-error` escape directives fail lint.

Each runtime has a separate compiler scope. DOM and WebWorker global libraries
are not combined. AudioWorklet's processor globals have explicit declarations
because TypeScript's DOM library describes the main thread but omits the
processor execution environment. Application, worker and worklet projects also check dependency declarations
(`skipLibCheck: false`). Node tooling and unit tests use `skipLibCheck: true`
because the pinned development libraries publish incompatible declarations
(for example Vue ESLint parser's missing `SyntaxKind` and happy-dom's
`UnderlyingDefaultSource` reference). Their consuming application code remains
strictly checked. This exception does not apply to production browser types.

## Boundaries and ownership

REST and WebSocket payloads are untrusted at runtime. Types alone do not validate
JSON: decoders check payloads before exposing domain objects. A generic type
assertion on `JSON.parse` or `response.json` does not satisfy that boundary.
DOM targets, nullable tracks, callback generations, pending capture ownership,
optional WebRTC statistics and storage/provider failures remain explicit.
Workers exchange typed messages with runtime validation; TypeScript does not
make messages from another execution context trustworthy.

Build tools run on Node 26 using its native type stripping; config loaders use
the pinned tooling where needed. Vite transpiles TypeScript and does not replace
`vue-tsc` or `tsc`. An AudioWorklet module is imported through Vite's bundled
`?worker&url` path; a raw `.ts?url` asset could contain syntax browsers cannot
execute. Real browser loading remains part of media verification.

The existing generated/licensed DeepFilterNet JavaScript under
`src/third_party/deepfilternet3` is the only source-language exception:
`worker-glue.js` and `worklet.js` retain their provenance and license text.
The active worker import has explicit `worker-glue.d.ts` declarations reviewed
against those exports. The unused original worklet remains an upstream
comparison artifact. This exception does not allow new first-party JavaScript.

Compiler and lint dependencies are exact-pinned in `package.json` and locked.
TypeScript 6.0.3 is compatible with the selected vue-tsc 3.3.12 and
TypeScript-ESLint 8.71.1 peer ranges. The REST generator needs no new runtime
library. Upgrading tooling requires repeating type checks, lint, tests and the
production build; remaining dependency advisories are reported separately from
production dependency results.

## Migration acceptance

Frontend coverage is enforced by `npm run test:coverage`, `make coverage-web`,
`make check` and CI. Statements, branches, functions and lines must each reach
95% globally and per module; the gate compares exact counts without rounding.
All first-party app entries, Vue components, workers and AudioWorklets stay in
the measured source inventory. An executable module with missing or zero
instrumentation fails the gate, even if the coverage tool displays 100%.
Type-only modules and re-export barrels have no statement body to instrument.

`web/coverage-policy.json` is an explicit per-file exception registry. Each
exception must name a concrete limitation, its author and two
distinct independent reviewers and set every metric's floor to at least 80%.
An exception cannot lower global thresholds, exclude code, or cover a wildcard.
Exceptions that no longer apply fail the gate and must be removed. JSON and
LCOV reports remain available when a coverage gate fails. The policy's review
names record actual completed review; filling the fields is not a substitute
for the required two review rounds.
If a review is conditional on precise uncovered counters, `approvedCounts`
records those counters and the gate rejects a different measured deficit.

A partially converted branch is not a completed migration. During isolated
work on a slice, a temporary ignored compiler configuration may permit legacy
JavaScript dependencies for diagnostics. Such configurations are not shipped.
The final integrated branch must pass source completeness, generated-contract
drift, all strict compiler projects, lint, existing behavior tests, native media
checks and `make check`. Changing a filename or weakening a compiler flag is
not sufficient evidence of preserved behavior.

Primary tooling references:
[Vue TypeScript overview](https://vuejs.org/guide/typescript/overview.html),
[Vite TypeScript and workers](https://vite.dev/guide/features.html),
[TypeScript strict compiler options](https://www.typescriptlang.org/tsconfig/#strict),
[TypeScript-ESLint supported dependencies](https://typescript-eslint.io/users/dependency-versions/),
[ESLint TypeScript config loading](https://eslint.org/docs/latest/use/configure/configuration-files#typescript-configuration-files).
