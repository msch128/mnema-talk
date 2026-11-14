# Mnema Talk

**A small, self-hosted home for one community: text channels, drop-in Talks
and screen sharing up to 4K at 60 FPS, in one Go binary.**

Website and docs: **[mnema.xyz](https://mnema.xyz)** ·
Source: [GitHub](https://github.com/mnema-xyz/talk) ·
Desktop app: [releases](https://github.com/mnema-xyz/talk-desktop/releases/latest)

This is the official server image: the Go server with the web app built in.
It runs next to PostgreSQL and S3-compatible storage (SeaweedFS by default),
three containers on a NAS, a Raspberry Pi or a small VPS.

## Where the image lives

| Registry | Image |
|---|---|
| GitHub Container Registry (primary) | `ghcr.io/mnema-xyz/mnema-talk` |
| Docker Hub | `docker.io/mnemaxyz/mnema-talk`, `docker.io/msch128/mnema-talk` |

All of them carry the same multi-arch index (linux/amd64 and linux/arm64)
with the same digest, BuildKit provenance and an SBOM, and a keyless cosign
signature of the project's release workflow.

## Tags

| Tag | Follows |
|---|---|
| `X.Y.Z` | exactly that release |
| `X.Y` | the patch releases of `X.Y` (recommended) |
| `X` | every release of major version `X` |
| `latest` | the newest release |

## Quick start

The setup wizard writes `docker-compose.yml` and `.env` for you, with fresh
secrets, and checks your host first:

```sh
mkdir mnema && cd mnema
docker run --rm -it --user "$(id -u):$(id -g)" -v "$PWD:/out" ghcr.io/mnema-xyz/mnema-talk setup
docker compose up -d
```

Open the address you entered and create the owner account. The full guide,
including Synology, Unraid, TLS, TURN for calls behind NAT and backups, is on
[mnema.xyz](https://mnema.xyz/host-your-own/installation/).

## Updating

```sh
docker compose pull && docker compose up -d
```

With the optional updater sidecar (compose profile `autoupdate`), an admin can
also press **Update now** in the System tab. It verifies the new image's
signature and provenance before it pulls or restarts anything, and fails
closed.

## Verifying an image

```sh
cosign verify ghcr.io/mnema-xyz/mnema-talk:latest \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github\.com/(mnema-xyz/talk|msch128/mnema-talk)/\.github/workflows/release\.yml@refs/heads/main$'
```

The same command works for the Docker Hub references.

## Features in short

- Text channels with threads, reactions, mentions, pins, polls and search.
- Talks: one-click voice and video rooms with screen sharing up to 4K at 60 FPS.
  Microphone, camera and screen are end-to-end encrypted (MLS-derived keys).
  Text is not end-to-end encrypted, by design.
- Roles, private channels, moderation, webhooks and a bot platform with
  official SDKs ([`@mnema-xyz/bot-sdk`](https://www.npmjs.com/package/@mnema-xyz/bot-sdk) for TypeScript).
- No telemetry. Every outgoing call of the server can be turned off.
- Members can export and delete their own data.

## License

AGPL-3.0. Security reports: see the
[security policy](https://github.com/mnema-xyz/talk/security/policy).
