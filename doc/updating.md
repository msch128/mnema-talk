# Updating

The one page for updating a Mnema Talk server. Per-version changes you may have
to act on are in the [upgrade notes](upgrade.md); going back is
[Rollback](upgrade.md#rollback). Database migrations run by themselves at
startup and are **forward-only**: an older version cannot use a schema a newer
one has migrated. So take a backup first, every time.

## 1. Back up

Back up the database, the media and `.env` before every update
([details](operations.md#backups-and-restore)). In the folder with
`docker-compose.yml` and `.env`:

```sh
BACKUP_DIR=/path/to/backups ./scripts/backup.sh
```

A server made with the setup assistant has no `scripts/` folder yet. Fetch the
scripts of the release you run once (replace `X.Y.Z` with the version in
Admin > System, or the newest on the
[releases page](https://github.com/mnema-xyz/talk/releases)):

```sh
base=https://raw.githubusercontent.com/mnema-xyz/talk/vX.Y.Z
mkdir -p scripts && cd scripts
for f in backup.sh backup-common.sh backup-online.sh restore.sh upgrade-postgres.sh health-check.sh; do curl -fsSLO "$base/scripts/$f"; done
chmod +x *.sh && cd ..
```

## 2. Choose how to update

Which way is available depends on the tag in `MNEMA_IMAGE` in `.env`:

| `MNEMA_IMAGE` tag | Follows | One-click button | Command line |
|---|---|---|---|
| `:X.Y` (for example `:2.1`) | all patch releases of that line | yes, until the next minor line | `docker compose pull && docker compose up -d`; a new minor line is a deliberate tag change |
| `:latest` | every release, breaking ones included | always | `docker compose pull && docker compose up -d` |
| `:X.Y.Z` or `@sha256:…` | nothing (pinned) | no; the System tab shows the steps | [pinned procedure](#pinned-or-new-minor-line) |
| `mnema-talk:local` (built on the server) | your checkout | no | `git fetch --tags && git checkout vX.Y.Z && docker compose up -d --build` |

The setup assistant writes a tag for the version it came with. Pinned tags are
the most predictable: you choose each version and can go back by setting the
old one. `:X.Y` is the practical middle ground: a `fix:` release bumps the patch
and a `feat:` release the minor, so `:X.Y` receives only fixes. There is no
`:X` tag. Releases publish `X.Y.Z`, `X.Y` and `latest`.

### The "Update now" button

The button belongs to the `autoupdate` [Compose profile](deployment.md#optional-parts-compose-profiles):
optional services that start only when enabled.


Admin > **System** shows the running version, the newest release with its notes
and, when a newer one exists, a dot on the server menu and a badge on "Admin
console" (the server asks GitHub every 30 minutes; `UPDATE_CHECK_ENABLED=false`
turns it off). Nothing updates automatically. Open tabs show "A new version is
available – click here to reload" after an update, and a call in progress
reconnects after the reload.

The optional **updater sidecar** adds the button. It verifies the new image's
signature and provenance, pulls that exact digest and restarts only the app;
everyone is disconnected for about 30 to 60 seconds, and it asks for the Owner's
password again. It is off by default. Enable it when the setup assistant asks, or
add two lines to `.env` and start the profile:

```sh
COMPOSE_PROFILES=autoupdate
UPDATER_TOKEN=<output of: openssl rand -hex 32>    # at least 32 characters
```

```sh
docker compose up -d
```

It needs a released image (not `mnema-talk:local`), and outbound HTTPS to `ghcr.io`
and to Sigstore ([Network ports](ports.md#outbound-connections)). The sidecar
holds the Docker socket: read the [threat model](../SECURITY.md#self-update-sidecar)
first. How the check works: [Verifying a release image](operations.md#verifying-a-release-image).
Synology: [platform notes](platform-notes.md#synology).

### Unattended updates (cron)

The "Update now" button never runs by itself. For a server that should follow
patch releases on its own, pin the minor line (`MNEMA_IMAGE=...:X.Y`, a `fix:`
release only bumps the patch) and let cron do what you would do by hand: back
up, pull, recreate the app, check health and, when the new version fails, go
back to the old one. Save this as `/usr/local/sbin/mnema-auto-update`,
`chmod 700` it, and adjust the paths at the top:

```sh
#!/usr/bin/env bash
set -uo pipefail
exec 9>/run/mnema-auto-update.lock; flock -n 9 || exit 0   # never two runs at once
cd /opt/mnema-talk || exit 1                               # your deployment folder
backups=/var/backups/mnema-talk      # where this script's backups go
bad=/var/lib/mnema-bad-image         # id of a release that failed its health check
halted=/var/lib/mnema-update-halted  # written when a rollback fails: no updates until you remove it
HEALTH_WAIT=${HEALTH_WAIT:-180}         # seconds, at least 5
MIGRATION_WAIT=${MIGRATION_WAIT:-3600}  # longest wait while the new version still migrates
[[ $HEALTH_WAIT =~ ^[0-9]+$ ]] && [ "$HEALTH_WAIT" -ge 5 ] || { echo "HEALTH_WAIT must be a number >= 5" >&2; exit 1; }
[[ $MIGRATION_WAIT =~ ^[0-9]+$ ]] || { echo "MIGRATION_WAIT must be a number" >&2; exit 1; }
[ ! -e "$halted" ] || { echo "updates halted after a failed rollback: $(cat "$halted")" >&2; exit 1; }

img=$(sed -n 's/^MNEMA_IMAGE=//p' .env)
[[ ${img##*:} =~ ^[0-9]+\.[0-9]+$ ]] || { echo "MNEMA_IMAGE must be a pinned X.Y tag, got '$img'" >&2; exit 1; }
old=$(docker inspect -f '{{.Image}}' mnema-talk 2>/dev/null || true)   # what runs now
docker pull -q "$img" >/dev/null || { echo "pull of $img failed" >&2; exit 1; }
new=$(docker image inspect -f '{{.Id}}' "$img")
[ "$new" != "$old" ] || exit 0                                         # nothing new

rollback() { [ -n "$old" ] && docker tag "$old" "$img"; }              # the tag points at the old image again
if [ "$(cat "$bad" 2>/dev/null)" = "$new" ]; then rollback; exit 0; fi # failed before: do not retry

health() { for _ in $(seq 1 $((HEALTH_WAIT / 5))); do
  [ "$(docker inspect -f '{{.State.Health.Status}}' mnema-talk 2>/dev/null)" = healthy ] && return 0; sleep 5; done; return 1; }
# The server holds this advisory lock while it applies migrations.
migrating() { [ "$(docker compose exec -T postgres sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -c "SELECT count(*) FROM pg_locks WHERE locktype='"'advisory'"' AND granted AND objsubid=1 AND ((classid::bigint << 32) | objid::bigint) = 470003576161"' </dev/null)" = 1 ]; }
halt() { docker compose stop app; echo "$(date -u +%FT%TZ) $1" > "$halted"
  echo "ROLLBACK FAILED: $1. The app is stopped; restore $backup by hand (doc/upgrade.md, Rollback)" >&2; exit 1; }

out=$(BACKUP_MODE=online BACKUP_DIR=$backups KEEP_DAYS=14 ./scripts/backup.sh 2>&1); status=$?
echo "$out"
backup=$(echo "$out" | sed -n 's/^backup complete: \(.*\) (restore test: [a-z]*)$/\1/p')   # this run's backup
if [ "$status" != 0 ] || [ ! -d "$backup" ]; then
  echo "backup failed, not updating" >&2; rollback; exit 1           # retried at the next run
fi
docker compose up -d        # recreates the services whose image changed (app and updater)
waited=0
while ! health && migrating; do   # never decide while a migration still runs
  [ "$waited" -lt "$MIGRATION_WAIT" ] || { echo "still migrating after ${MIGRATION_WAIT}s, not rolling back: check by hand" >&2; exit 1; }
  echo "a migration is still running, waiting"; waited=$((waited + HEALTH_WAIT))
done
health && exit 0                  # after a migration, the health check gets HEALTH_WAIT once more
echo "new version not healthy, going back" >&2
echo "$new" > "$bad"; rollback; docker compose up -d
health && exit 0
# The old version refuses a database the new one migrated: restore this run's backup.
[ "$(docker compose logs app | grep -c 'migrated by a newer version')" != 0 ] || halt "the old version is unhealthy too"
(cd "$backup" && sha256sum --quiet -c SHA256SUMS) || halt "backup $backup fails its checksums"
echo "the old version refuses the migrated database, restoring $backup" >&2
docker compose stop app
./scripts/restore.sh "$backup" --yes || halt "restore failed"
docker compose up -d
health || halt "the old version is unhealthy after the restore"
echo "rolled back and restored $backup: writes made since that backup are lost" >&2
exit 1
```

Run it hourly from `/etc/cron.d/mnema-auto-update`
(`10 * * * * root /usr/local/sbin/mnema-auto-update >> /var/log/mnema-auto-update.log 2>&1`)
and keep it clear of the nightly backup slot. Nothing alerts you: the log file
is the only report, so read it now and then (or add a mail or webhook call at the
`echo` lines). A release that failed its health check is remembered in
`/var/lib/mnema-bad-image` and not retried until a newer image appears; a failed
backup is retried at the next run. If `old` is empty (the app container was
missing) the script cannot roll back and only reports it.

When the new version does not become healthy, the script goes back in steps:

1. It waits while the new version still applies migrations (up to
   `MIGRATION_WAIT`), so it never rolls back in the middle of one.
2. It points the tag at the old image and starts it. Without schema changes
   this is enough, and no data is lost.
3. Only if the old version refuses the database (the new one already migrated
   it: `the database was migrated by a newer version` in the log) does it check
   the checksums of the backup it took minutes earlier, restore exactly that
   backup with `restore.sh` and start the old version again. Messages and
   uploads made between that backup and the rollback are lost.
4. If the restore or the old version fails as well, it stops the app, writes
   `/var/lib/mnema-update-halted` and makes no further attempts. Repair by hand
   ([Rollback](upgrade.md#rollback)) and remove that file to resume updates.

Things to know:

- Members are disconnected for 30 to 60 seconds during an update, so pick the
  hours that suit your community (change the cron hour field).
- This path pulls by tag and does not verify the signature the way the
  "Update now" button does ([Verifying a release image](operations.md#verifying-a-release-image)).
  Use a pinned digest and the button if that matters more than automation.
- A new minor line (a `feat:` release) is never picked up; it can change the
  compose file, so move to it by hand as described above.
- The backup is taken online, so the app keeps serving while it runs; writes
  made during the backup itself are not in it either.
- The script reads no secret and prints none: `restore.sh` and `backup.sh`
  read `.env` themselves, and the database query runs inside the postgres
  container.

### Command line within a line

For `:X.Y` and `:latest`:

```sh
cd /path/to/mnema-talk
docker compose pull && docker compose up -d
```

Compose recreates only the services whose image changed; the downtime is a few
seconds. Then [verify](#4-verify).

On Synology without SSH: Container Manager > Project > `mnema-talk` > Action >
Stop, then Clean (removes the project's containers and images, not your data)
and Build, which pulls the current image of the tag again. Menu wording differs
a little between DSM versions.

### Pinned or new minor line

Run in the deployment folder. The first lines look up the newest release; set
`NEW` yourself to install another.

```sh
NEW=$(curl -fsSL https://api.github.com/repos/mnema-xyz/talk/releases/latest | sed -n 's/.*"tag_name": *"v\([^"]*\)".*/\1/p')  # the newest release, or set a version
[ -n "$NEW" ] || { echo "could not read the latest version; set NEW=x.y.z yourself" >&2; false; }

# Note what runs now, for a rollback
grep '^MNEMA_IMAGE=' .env
curl -fsS http://127.0.0.1:8080/api/health
cp docker-compose.yml docker-compose.yml.bak

# Fetch the compose file (and scripts) of the new release
base=https://raw.githubusercontent.com/mnema-xyz/talk/v$NEW
curl -fsSLo docker-compose.yml        "$base/docker-compose.yml"
curl -fsSLo .env.example.new          "$base/.env.example"
for f in backup.sh backup-common.sh backup-online.sh restore.sh upgrade-postgres.sh health-check.sh; do
  curl -fsSLo "scripts/$f" "$base/scripts/$f"
done
chmod +x scripts/*.sh

# New or changed variables since your .env was written
diff <(grep -o '^[A-Z_0-9]*=' .env | sort) <(grep -o '^[A-Z_0-9]*=' .env.example.new | sort)

# Point the app at the new image
if grep -q '^MNEMA_IMAGE=' .env; then
  sed -i "s|^MNEMA_IMAGE=.*|MNEMA_IMAGE=ghcr.io/mnema-xyz/mnema-talk:$NEW|" .env
else
  echo "MNEMA_IMAGE=ghcr.io/mnema-xyz/mnema-talk:$NEW" >> .env
fi

# Check the configuration, pull and start
docker compose config -q
docker compose pull app
docker compose up -d       # add --profile turn / --profile autoupdate if you use them
```

(`sed -i` as written is for Linux; on macOS use `sed -i ''`.) The same image is
also published on Docker Hub as `docker.io/mnemaxyz/mnema-talk` (same digest and
signature); GHCR stays the primary reference. With
`MNEMA_IMAGE=mnemaxyz/mnema-talk:<tag>` "Update now" works too and pulls from
Docker Hub.

## 3. Read the notes

For every release you skip over, read its entry in the [upgrade notes](upgrade.md#version-notes):
some need a setting changed before you update, for example a narrower
`TRUSTED_PROXY_CIDRS` ([Broad trusted proxies](upgrade.md#broad-trusted-proxies)).
Check the changelog before updating across a release that changed the schema.

## 4. Verify

```sh
docker compose ps                 # app "healthy" after about 15-30 seconds
curl -fsS http://127.0.0.1:8080/api/health
docker compose logs --since 10m app | grep -E 'migration applied|"level":"(WARN|ERROR)"'
```

Open the app: other open tabs show the reload notice, and Admin > System shows
the new version. Then look at Admin > Status for new amber or red items.

## If it goes wrong

Stop the app and follow [Rollback](upgrade.md#rollback). Because migrations are
forward-only, going back across a release that changed the database means
restoring the backup from step 1. Keep that backup and the saved
`docker-compose.yml.bak` until the new version has passed your own checks.
