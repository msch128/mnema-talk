#!/usr/bin/env bash
# Proxy matrix: the app behind Caddy or nginx, configured with the exact
# snippets from the docs (the blocks marked <!-- test:caddy --> and
# <!-- test:nginx -->, read by scripts/doc-blocks.mjs). Only what a CI run
# cannot share with a real host is rewritten: the host name of the app, a
# self-signed certificate, and the nginx certificate paths.
# It asserts that
#   - HTTPS loads the web app through the proxy,
#   - /api/ws upgrades to a WebSocket (101) through the proxy,
#   - without TRUSTED_PROXY_CIDRS the Status tab warns about the untrusted
#     proxy (so the check can see the problem at all), and
#   - with the documented value (the proxy container's address) the warning
#     is gone and the public address check is green.
# Usage: scripts/onboarding-proxy-check.sh <caddy|nginx> [image]
#   (default image mnema-talk:local). Needs Docker, Node, Python 3, curl, openssl
#   and a free 127.0.0.1:443.
set -euo pipefail

PROXY="${1:?usage: $0 <caddy|nginx> [image]}"
IMAGE="${2:-mnema-talk:local}"
SRC="$(cd "$(dirname "$0")/.." && pwd)"
# Pinned by digest like the rest of the CI images.
CADDY_IMAGE="caddy:2.10-alpine@sha256:4c6e91c6ed0e2fa03efd5b44747b625fec79bc9cd06ac5235a779726618e530d"
NGINX_IMAGE="nginx:1.29-alpine@sha256:5616878291a2eed594aee8db4dade5878cf7edcb475e59193904b198d9b830de"
HOST=chat.example.com
BASE="https://$HOST"

source "$SRC/scripts/free-port.sh"
UDP_MIN="${PROXY_UDP_PORT:-$(free_port_range 11)}"
UDP_MAX=$((UDP_MIN + 10))
PORT="${PROXY_APP_PORT:-$(free_port "$UDP_MIN-$UDP_MAX")}"
PROJECT="mnema-proxy-$(openssl rand -hex 6)"
DIR="$(mktemp -d)"
PROXY_NAME="$PROJECT-$PROXY"
ADMIN_PASSWORD="proxy-$(openssl rand -hex 10)"
COOKIES="$DIR/cookies"
NET="$PROJECT-net"
COMPOSE=(docker compose -p "$PROJECT" --project-directory "$DIR")

cleanup() {
  status=$?
  if [ "$status" -ne 0 ]; then
    echo "--- $PROXY log ---" >&2
    docker logs --tail 40 "$PROXY_NAME" >&2 || true
    "${COMPOSE[@]}" logs --no-color --tail 40 app >&2 || true
  fi
  docker rm -f "$PROXY_NAME" >/dev/null 2>&1 || true
  "${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
  rm -rf "$DIR"
  exit "$status"
}
trap cleanup EXIT

fail() { echo "FAIL ($PROXY): $*" >&2; exit 1; }

# ---- the app: compose file and .env of this checkout, behind the proxy -----
cp "$SRC/docker-compose.yml" "$DIR/docker-compose.yml"
cp "$SRC/.env.example" "$DIR/.env"
chmod 600 "$DIR/.env"
# The compose file fixes container and network names, which would collide with
# a running deployment or a concurrent run on this Docker host. Compose merges
# this override automatically: no fixed container names, per-project networks.
cat >"$DIR/docker-compose.override.yml" <<YAML
services:
  app: {container_name: !reset null}
  postgres: {container_name: !reset null}
  seaweedfs: {container_name: !reset null}
  coturn: {container_name: !reset null}
  updater: {container_name: !reset null}
  recreator: {container_name: !reset null}
networks:
  mnema-network: {name: $NET}
  mnema-updater: {name: $PROJECT-updater}
  mnema-recreator: {name: $PROJECT-recreator}
  mnema-updater-egress: {name: $PROJECT-updater-egress}
YAML
set_env() { # name value
  if grep -q "^$1=" "$DIR/.env"; then
    sed "s|^$1=.*|$1=$2|" "$DIR/.env" >"$DIR/.env.tmp" && mv "$DIR/.env.tmp" "$DIR/.env"
  else
    echo "$1=$2" >>"$DIR/.env"
  fi
}
set_env MNEMA_IMAGE "$IMAGE"
set_env PORT "$PORT"
set_env PUBLIC_URL "$BASE"
set_env JWT_SECRET "$(openssl rand -hex 32)"
set_env POSTGRES_PASSWORD "$(openssl rand -hex 16)"
set_env S3_ACCESS_KEY "proxy$(openssl rand -hex 4)"
set_env S3_SECRET_KEY "$(openssl rand -hex 16)"
set_env ADMIN_INITIAL_PASSWORD "$ADMIN_PASSWORD"
set_env UPDATE_CHECK_ENABLED false
set_env LINK_PREVIEWS_ENABLED false
set_env WEBRTC_UDP_PORT_MIN "$UDP_MIN"
set_env WEBRTC_UDP_PORT_MAX "$UDP_MAX"
set_env WEBRTC_NAT_1TO1_IP 127.0.0.1
# The advertised case: TRUSTED_PROXY_CIDRS is not set until the check below
# has seen the warning.
echo "==> app up"
"${COMPOSE[@]}" up -d --wait --wait-timeout 240

# ---- the proxy: the documented snippet, run as a container next to the app --
snippet="$(node "$SRC/scripts/doc-blocks.mjs" "$PROXY")"
grep -q 'app-host:8080' <<<"$snippet" || fail "the $PROXY snippet no longer forwards to app-host:8080; update scripts/onboarding-proxy-check.sh"
snippet="${snippet//app-host:8080/app:8080}"   # the app's service name on the compose network

mkdir -p "$DIR/proxy"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj "/CN=$HOST" \
  -addext "subjectAltName=DNS:$HOST" -keyout "$DIR/proxy/tls.key" -out "$DIR/proxy/tls.crt" >/dev/null 2>&1
chmod 644 "$DIR/proxy/tls.key"

case "$PROXY" in
  caddy)
    # Self-signed certificate from Caddy's own CA instead of an ACME request.
    { printf '{\n    local_certs\n    skip_install_trust\n}\n'; printf '%s\n' "$snippet"; } >"$DIR/proxy/Caddyfile"
    docker run -d --name "$PROXY_NAME" --network "$NET" -p 127.0.0.1:443:443 \
      -v "$DIR/proxy/Caddyfile:/etc/caddy/Caddyfile:ro" "$CADDY_IMAGE" >/dev/null
    ;;
  nginx)
    grep -q '# ssl_certificate \.\.\.;' <<<"$snippet" || fail "the nginx snippet lost its '# ssl_certificate ...;' line"
    snippet="${snippet//'# ssl_certificate ...;'/ssl_certificate /certs/tls.crt; ssl_certificate_key /certs/tls.key;}"
    printf '%s\n' "$snippet" >"$DIR/proxy/default.conf"
    docker run -d --name "$PROXY_NAME" --network "$NET" -p 127.0.0.1:443:443 \
      -v "$DIR/proxy/default.conf:/etc/nginx/conf.d/default.conf:ro" \
      -v "$DIR/proxy:/certs:ro" "$NGINX_IMAGE" >/dev/null
    ;;
  *) fail "unknown proxy $PROXY" ;;
esac

CURL=(curl -sk --resolve "$HOST:443:127.0.0.1" --max-time 10)

echo "==> HTTPS through $PROXY"
for _ in $(seq 1 30); do
  if [ "$("${CURL[@]}" -o /dev/null -w '%{http_code}' "$BASE/api/health" || true)" = 200 ]; then ok=1; break; fi
  sleep 1
done
[ "${ok:-}" = 1 ] || fail "$BASE/api/health did not answer 200 through the proxy"
case "$("${CURL[@]}" "$BASE/")" in
  *'<div id="app"'*|*'<!doctype html'*|*'<!DOCTYPE html'*) ;;
  *) fail "$BASE/ did not return the web app" ;;
esac

echo "==> Owner sign-in through $PROXY"
"${CURL[@]}" -f -c "$COOKIES" -H "Origin: $BASE" -H 'Content-Type: application/json' \
  --data "{\"username\":\"admin\",\"password\":\"$ADMIN_PASSWORD\"}" "$BASE/api/auth/login" >/dev/null \
  || fail "sign-in through the proxy failed"

ws_status() { # first status code of an /api/ws upgrade request
  local ws_header=Sec-WebSocket-Key
  local out
  # The connection stays open after the 101, so curl ends at --max-time.
  out="$("${CURL[@]}" --max-time 4 --http1.1 -i -b "$COOKIES" \
    -H 'Connection: Upgrade' -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' \
    -H "${ws_header}: $(openssl rand -base64 16)" -H "Origin: $BASE" "$BASE/api/ws" 2>/dev/null || true)"
  awk 'NR==1{print $2; exit}' <<<"$out"
}

# proxy_check prints the codes of the reverse_proxy and public_url setup checks.
proxy_check() {
  "${CURL[@]}" -f -b "$COOKIES" "$BASE/api/admin/status" | python3 -c '
import json, sys
st = json.load(sys.stdin)
codes = {c["id"]: c.get("code", "") for c in st.get("setup", [])}
print(codes.get("reverse_proxy", "none"), codes.get("public_url", "none"))'
}

echo "==> /api/ws through $PROXY"
[ "$(ws_status)" = 101 ] || fail "/api/ws did not upgrade (101) through the proxy; got '$(ws_status)'"

echo "==> Status without TRUSTED_PROXY_CIDRS (the warning must be visible)"
read -r rp _ <<<"$(proxy_check)"
[ "$rp" = reverse_proxy.not_trusted ] || fail "expected reverse_proxy.not_trusted before TRUSTED_PROXY_CIDRS is set, got $rp (the check cannot see an untrusted proxy)"

echo "==> Status with the documented TRUSTED_PROXY_CIDRS (the proxy container's address)"
proxy_ip="$(docker inspect -f "{{(index .NetworkSettings.Networks \"$NET\").IPAddress}}" "$PROXY_NAME")"
[ -n "$proxy_ip" ] || fail "cannot read the proxy container address"
set_env TRUSTED_PROXY_CIDRS "$proxy_ip"
"${COMPOSE[@]}" up -d --wait --wait-timeout 240 app
# nginx resolves app:8080 once at start; the recreated app has a new address.
if [ "$PROXY" = nginx ]; then docker exec "$PROXY_NAME" nginx -s reload >/dev/null; fi
ok=
for _ in $(seq 1 30); do
  if [ "$("${CURL[@]}" -o /dev/null -w '%{http_code}' "$BASE/api/health" || true)" = 200 ]; then ok=1; break; fi
  sleep 1
done
[ "$ok" = 1 ] || fail "$BASE/api/health did not answer 200 through the proxy after the app restart"
"${CURL[@]}" -f -c "$COOKIES" -H "Origin: $BASE" -H 'Content-Type: application/json' \
  --data "{\"username\":\"admin\",\"password\":\"$ADMIN_PASSWORD\"}" "$BASE/api/auth/login" >/dev/null \
  || fail "sign-in after the restart failed"
read -r rp pu <<<"$(proxy_check)"
[ "$rp" = reverse_proxy.ok ] || fail "TRUSTED_PROXY_CIDRS=$proxy_ip still leaves the Reverse proxy check at $rp"
[ "$pu" = public_url.ok ] || fail "the Public address check is $pu behind the documented $PROXY config (Host and X-Forwarded-Proto must reach the app)"
[ "$(ws_status)" = 101 ] || fail "/api/ws did not upgrade (101) after trusting the proxy"

echo "==> proxy matrix ($PROXY) passed"
