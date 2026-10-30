#!/usr/bin/env bash
# Fresh install check: follows doc/deployment.md ("Fresh install") in an empty
# directory. The only inputs are the compose file and .env.example of this
# checkout (what an operator downloads for a tag) and the image under test;
# the required .env values are filled in with throwaway secrets, then it
# starts the stack, checks /api/health, signs in as the Owner and removes the
# project again.
# The compose file fixes its container names, so stop any Mnema stack running on
# this machine first (CI runs on an empty runner).
# Usage: scripts/fresh-install-check.sh [image]   (default mnema-talk:local)
#   FRESH_PORT=<port>  host port for the app
#   FRESH_PUBLIC_HOST=localhost  the "try it on this computer" path: PUBLIC_URL is
#     http://localhost:<port>, WEBRTC_NAT_1TO1_IP stays empty (the server announces
#     127.0.0.1 itself) and the log must say so
set -euo pipefail

SRC="$(cd "$(dirname "$0")/.." && pwd)"
IMAGE="${1:-mnema-talk:local}"
source "$(cd "$(dirname "$0")" && pwd)/free-port.sh"
UDP_MIN="${FRESH_UDP_PORT:-$(free_port_range 11)}"
UDP_MAX=$((UDP_MIN + 10))
PORT="${FRESH_PORT:-$(free_port "$UDP_MIN-$UDP_MAX")}"
PROJECT="mnema-fresh-$(openssl rand -hex 6)"
DIR="$(mktemp -d)"
BASE="http://127.0.0.1:$PORT"
PUBLIC_HOST="${FRESH_PUBLIC_HOST:-127.0.0.1}"
PUBLIC="http://$PUBLIC_HOST:$PORT"
ADMIN_PASSWORD="fresh-$(openssl rand -hex 10)"

cleanup() {
  status=$?
  if [ "$status" -ne 0 ]; then
    docker compose -p "$PROJECT" --project-directory "$DIR" logs --no-color --tail 60 app >&2 || true
  fi
  docker compose -p "$PROJECT" --project-directory "$DIR" down -v --remove-orphans >/dev/null 2>&1 || true
  rm -rf "$DIR"
  exit "$status"
}
trap cleanup EXIT

# The guide's first step: an empty directory with the two downloaded files.
cp "$SRC/docker-compose.yml" "$DIR/docker-compose.yml"
cp "$SRC/.env.example" "$DIR/.env"
chmod 600 "$DIR/.env"

# The values the guide's table names. The ports also move so the run cannot
# collide with another stack on the machine.
set_env() { # name value
  if grep -q "^$1=" "$DIR/.env"; then
    sed "s|^$1=.*|$1=$2|" "$DIR/.env" >"$DIR/.env.tmp" && mv "$DIR/.env.tmp" "$DIR/.env"
  else
    echo "$1=$2" >>"$DIR/.env"
  fi
}
set_env MNEMA_IMAGE "$IMAGE"
set_env PORT "$PORT"
set_env PUBLIC_URL "$PUBLIC"
set_env JWT_SECRET "$(openssl rand -hex 32)"
set_env POSTGRES_PASSWORD "$(openssl rand -hex 16)"
set_env S3_ACCESS_KEY "fresh$(openssl rand -hex 4)"
set_env S3_SECRET_KEY "$(openssl rand -hex 16)"
set_env ADMIN_INITIAL_PASSWORD "$ADMIN_PASSWORD"
set_env UPDATE_CHECK_ENABLED false
set_env LINK_PREVIEWS_ENABLED false
set_env WEBRTC_UDP_PORT_MIN "$UDP_MIN"
set_env WEBRTC_UDP_PORT_MAX "$UDP_MAX"
if [ "$PUBLIC_HOST" = localhost ]; then
  # Nothing pinned: a trial on one computer must work with the defaults.
  sed '/^WEBRTC_NAT_1TO1_IP=/d' "$DIR/.env" >"$DIR/.env.tmp" && mv "$DIR/.env.tmp" "$DIR/.env"
else
  set_env WEBRTC_NAT_1TO1_IP 127.0.0.1
fi

# The guide's start commands (--pull never: the image under test is local).
cd "$DIR"
docker compose -p "$PROJECT" config -q
echo "==> docker compose up -d --wait"
docker compose -p "$PROJECT" up -d --wait --wait-timeout 240 --pull never

echo "==> /api/health"
curl -fsS "$BASE/api/health"
echo

echo "==> Owner sign-in"
COOKIES="$DIR/cookies"
curl -fsS -c "$COOKIES" -H "Origin: $PUBLIC" -H 'Content-Type: application/json' \
  --data "{\"username\":\"admin\",\"password\":\"$ADMIN_PASSWORD\"}" \
  "$BASE/api/auth/login" >/dev/null
case "$(curl -fsS -b "$COOKIES" "$BASE/api/auth/me")" in
  *'"role":"admin"'*) ;;
  *) echo "the Owner is not signed in" >&2; exit 1 ;;
esac

if [ "$PUBLIC_HOST" = localhost ]; then
  echo "==> the server announces 127.0.0.1 for a trial on this computer"
  docker compose -p "$PROJECT" logs --no-color app | grep '"webrtc announce"' | grep -q '127.0.0.1' \
    || { echo "the log does not show 127.0.0.1 as the announced voice address" >&2; exit 1; }
fi

# mnema-talk doctor: the Status tab's checks for SSH-only operators; it exits
# non-zero only on a red check, and a healthy fresh install has none.
echo "==> mnema-talk doctor"
docker compose -p "$PROJECT" exec -T app mnema-talk doctor

echo "==> fresh install check passed"
