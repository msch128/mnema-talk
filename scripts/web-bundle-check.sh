#!/usr/bin/env sh
# Signs web/dist with a throwaway key, verifies the bundle, and checks that the
# committed public key parses (doc/desktop-trusted-client.md). The real
# signing key only exists in the release workflow; this proves the tooling and
# the compat file work on the current build. Needs web/dist (make web).
set -eu
cd "$(dirname "$0")/.."
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
umask 077
openssl genpkey -algorithm ed25519 -out "$tmp/key.pem" 2>/dev/null
openssl pkey -in "$tmp/key.pem" -pubout -out "$tmp/pub.pem"
version="$(tr -d ' \n' < version.txt)"
go run ./cmd/webbundle build -dir web/dist -compat web/desktop-compat.json \
  -version "$version" -commit "$(git rev-parse HEAD 2>/dev/null || echo unknown)" \
  -key-file "$tmp/key.pem" -out "$tmp/out"
go run ./cmd/webbundle verify -zip "$tmp/out/mnema-web-$version.zip" -pub "$tmp/pub.pem"
go run ./cmd/webbundle keyid -pub web/desktop-signing.pub
