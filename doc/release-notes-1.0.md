# Mnema Talk 1.0.0 release notes

The generated list of changes is in [CHANGELOG.md](../CHANGELOG.md) (release-please
writes it from the commits). This page is what the changelog cannot say: what
1.0 promises, what it does not, and what to do before updating. The release
pull request body can link here.

## Stability promises

From 1.0.0 Mnema Talk follows [semantic versioning](https://semver.org) for the
surfaces in [api-stability.md](api-stability.md). Concretely, for every `1.x`:

- **Your clients, bots, webhooks, dashboards and alerts keep working.** The REST
  API (`api/openapi.json`), WebSocket event names and payloads, bot gateway and
  bot API, incoming and outgoing webhook formats, the desktop client contract and
  bridge, documented environment variables (names, meaning, defaults) and
  Prometheus metric names are only extended in a minor release, never changed
  incompatibly.
- **Breaking changes wait for 2.0.** Anything on that list is deprecated in a
  minor release first (announced in the changelog and `doc/api-stability.md`)
  and removed no earlier than the next major version. CI fails an incompatible
  change to the OpenAPI document unless a commit carries a `BREAKING CHANGE:`
  footer.
- **Upgrades are forward-only and tested.** Migrations run at startup and are
  never edited after release. CI installs each supported earlier release,
  seeds data through its own API, upgrades to the build under test and checks
  the data, then checks that a backup of the old release restores into the new
  one and that the old release refuses to start on migrated data. Rollback means
  restoring the backup taken before the update
  ([upgrade.md](upgrade.md#09x--100)).
- **Safe defaults stay safe.** `MEDIA_RETENTION_DAYS=0` (nothing deleted
  automatically), invite-only registration and a single Owner who nobody can
  restrict do not change in a minor release.
- **Security fixes ship as patch releases** for the latest minor and the
  previous one for 90 days ([SECURITY.md](../SECURITY.md#supported-versions)).

Not covered by the promises: the database schema, `internal/...` Go packages,
operator-only admin endpoints, log text and UI wording ([the full list](api-stability.md#what-is-not-public)).
The desktop clients stay Experimental.

## What 1.0 is not

- Not an externally audited release. The
  [internal security review](../SECURITY.md#10-internal-security-review) found
  no Critical or High issue and fixed every Medium one; the items it deferred
  are listed there.
- No end-to-end encryption of text: messages and attachments stay readable by
  the server. Voice, camera and screen payloads are sealed in the browser
  ([product-status.md](product-status.md)).
- No signed or notarised desktop installers, and no capacity claim beyond the
  recorded load tests.

## Before you update from 0.9.x

Read [the 0.9.x to 1.0.0 notes](upgrade.md#09x--100), then follow the
[update procedure](upgrade.md#update-procedure-compose-released-image) with a
backup first. The short version:

1. Migrations 0038 and 0039 run on first start and are quick.
2. Weak custom invite codes stop working; regenerate them.
3. Uploads are limited to 2 GiB per account and 24 hours by default
   (`UPLOAD_QUOTA_MB_PER_DAY`, `0` = off).
4. coturn gets allocation quotas; fetch the compose file of the tag.
5. An `http://` `PUBLIC_URL` in production logs a warning.
6. Regenerate bot tokens you still use so they are bound to their issuer.

New to Mnema Talk? [deployment.md](deployment.md#fresh-install) installs 1.0
from scratch, and [operations.md](operations.md#monitoring-and-alerts) has the
metrics and the ready-made
[alert rules](../deploy/monitoring/mnema-alerts.yml).
