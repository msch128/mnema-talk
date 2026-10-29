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
[releases page](https://github.com/msch128/mnema-talk/releases)):

```sh
base=https://raw.githubusercontent.com/msch128/mnema-talk/vX.Y.Z
mkdir -p scripts && cd scripts
for f in backup.sh backup-common.sh restore.sh upgrade-postgres.sh health-check.sh; do curl -fsSLO "$base/scripts/$f"; done
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

Run in the deployment folder. Replace `X.Y.Z` with the version to install.

```sh
NEW=X.Y.Z

# Note what runs now, for a rollback
grep '^MNEMA_IMAGE=' .env
curl -fsS http://127.0.0.1:8080/api/health
cp docker-compose.yml docker-compose.yml.bak

# Fetch the compose file (and scripts) of the new release
base=https://raw.githubusercontent.com/msch128/mnema-talk/v$NEW
curl -fsSLo docker-compose.yml        "$base/docker-compose.yml"
curl -fsSLo .env.example.new          "$base/.env.example"
for f in backup.sh backup-common.sh restore.sh upgrade-postgres.sh health-check.sh; do
  curl -fsSLo "scripts/$f" "$base/scripts/$f"
done
chmod +x scripts/*.sh

# New or changed variables since your .env was written
diff <(grep -o '^[A-Z_0-9]*=' .env | sort) <(grep -o '^[A-Z_0-9]*=' .env.example.new | sort)

# Point the app at the new image
if grep -q '^MNEMA_IMAGE=' .env; then
  sed -i "s|^MNEMA_IMAGE=.*|MNEMA_IMAGE=ghcr.io/msch128/mnema-talk:$NEW|" .env
else
  echo "MNEMA_IMAGE=ghcr.io/msch128/mnema-talk:$NEW" >> .env
fi

# Check the configuration, pull and start
docker compose config -q
docker compose pull app
docker compose up -d       # add --profile turn / --profile autoupdate if you use them
```

(`sed -i` as written is for Linux; on macOS use `sed -i ''`.) The same image is
also published on Docker Hub as `docker.io/msch128/mnema-talk` (same digest and
signature); GHCR stays the primary reference.

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
