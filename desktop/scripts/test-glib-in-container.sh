#!/usr/bin/env bash
# Invoked only by test-glib-linux.sh in its owned throwaway container.
set -euo pipefail
apt-get update
apt-get install --yes --no-install-recommends libglib2.0-dev pkg-config
cargo test --manifest-path /desktop/Cargo.toml --locked --release --test glib_variant_iter

# Negative control: revert only the two backported lines in a disposable
# dependency copy. The checked source and real lock remain read-only.
cp -a /desktop/experimental/vendor/glib /build/unpatched-glib
sed -i \
  -e 's/let mut p: \*mut libc::c_char/let p: *mut libc::c_char/' \
  -e 's/\&mut p,/\&p,/' \
  /build/unpatched-glib/src/variant_iter.rs
# Cargo can retain the patched and unpatched executable. Select the executable
# from the last build's JSON metadata rather than relying on timestamps.
cargo --config 'patch.crates-io.glib.path="/build/unpatched-glib"' \
  test --manifest-path /desktop/Cargo.toml --locked --release --test glib_variant_iter --no-run \
  --message-format=json > /build/unpatched-build.jsonl
test_binary="$(sed -n 's/.*"executable":"\([^"]*\)".*/\1/p' /build/unpatched-build.jsonl)"
[[ -n "$test_binary" && "$test_binary" != *$'\n'* && -x "$test_binary" ]]
ulimit -c 0
set +e
"$test_binary" --exact repeated_full_traversals_preserve_all_items > /build/unpatched-result.log 2>&1
unpatched_status=$?
set -e
if [[ "$unpatched_status" != 139 && "$unpatched_status" != 134 ]]; then
  cat /build/unpatched-result.log
  echo "Unpatched negative control did not reproduce the expected memory failure (status $unpatched_status)." >&2
  exit 1
fi
echo "Unpatched GLib negative control reproduced memory failure (status $unpatched_status)."
