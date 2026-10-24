# Contributing to Mnema Talk

Thanks for helping. Mnema Talk is a small, self-hosted project for one
community, so changes that keep it small, quiet and easy to run are the ones
that land fastest. This repository is public: never commit secrets, databases
or uploads. [AGENTS.md](AGENTS.md) holds the full rules and applies to people
and AI agents alike; this page is the short version.

## Before you start

- Bugs and ideas: open an issue first for anything larger than a small fix, so
  we can agree on scope. Mnema Talk is deliberately not a Discord clone: one
  community, private conversations only between members, no ringing.
- Security problems go through the private route in [SECURITY.md](SECURITY.md),
  never into a public issue or pull request.
- Use the words in [doc/glossary.md](doc/glossary.md) (community, member, Talk,
  Owner) in the UI, docs and error messages. The German interface says "du".

## Development setup

You need Go (see `go.mod`), Node and npm, Docker and GNU make.

```sh
make install-hooks      # pre-commit: gitleaks + gofmt
make dev                # postgres + seaweedfs in Docker, server on :8080 (reads .env)
cd web && npm run dev   # Vite on :3000, proxies /api to :8080
```

Copy `.env.example` to `.env` first. `make help` lists every target;
[doc/development.md](doc/development.md) covers building, tests, CI and releases.

## Quality gates

Run `make check` before you push: it is what CI runs.

- **Tests describe behaviour.** New behaviour comes with tests named after what
  the caller observes. Do not add tests, branches or exports only to raise a
  coverage number; if code is hard to cover, simplify it.
- **Coverage:** at least 90 % in total and 80 % in every file, for Go and for
  the web app. Do not lower the gates.
- **Limits:** production files stay under 400 lines, functions under 80 lines,
  cognitive complexity 15 (Go and TypeScript). Refactor instead of adding an
  exemption.
- **Migrations** are new numbered files in `internal/db/migrations/`; never edit
  one that has shipped.
- **Public API** (REST, WebSocket events, bot and webhook payloads, environment
  variables, metric names) changes incompatibly only in a major release; see
  [doc/api-stability.md](doc/api-stability.md).
- New UI strings need English and German. Everything else is English only:
  code, comments, log and error messages, tests, fixtures, docs and scripts.
  German belongs in the locale resources and in tests that assert German
  output. `make english-check` (part of `make check`) fails on German anywhere
  else; a legitimate exception goes in `scripts/english-only-allowlist.txt`
  with a reason.

## Commits and pull requests

- Use conventional commits: `feat:`, `fix:`, `docs:`, `chore:`, `refactor:`,
  `test:`. release-please derives versions and the changelog from them, so the
  subject should say what changed for the user.
- Changes land through a pull request with green checks and a squash merge.
  Work goes into `main`; while a release is being prepared a `release/x.y.z`
  branch exists and pull requests for that release target it. Ask on the issue
  which branch to use. Never force-push or rewrite `main` or a release tag.
- Keep a pull request to one topic and fill in the template.
- **No AI attribution.** Do not add `Co-Authored-By` lines for AI tools, or
  "generated with" footers, to commits or pull request descriptions. You are
  responsible for what you submit, whatever helped you write it.

## License

Contributions are accepted under the AGPL-3.0, and their authors agree that
Marius Schröder may also distribute them under a commercial license (see
[README](README.md#license)).
