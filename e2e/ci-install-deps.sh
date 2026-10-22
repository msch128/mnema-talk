#!/usr/bin/env bash
# CI only: install the system libraries Playwright's browsers need, in bounded
# attempts. Usage: e2e/ci-install-deps.sh chromium webkit
#
# Why not just `playwright install --with-deps`: the Azure Ubuntu mirror of the
# GitHub runners sometimes trickles bytes (a few KB/s). apt's own timeout only
# fires when no data arrives, so a trickle never trips it and the job ran into
# its 20-30 minute limit. Here every attempt has a hard time limit; apt resumes
# partial downloads (cached in ~/apt-archives, saved by actions/cache), and the
# later attempts switch to archive.ubuntu.com.
set -euo pipefail

cd "$(dirname "$0")"
ARCHIVES="$HOME/apt-archives"
ATTEMPTS="${ATTEMPTS:-3}"
ATTEMPT_SECONDS="${ATTEMPT_SECONDS:-150}"

mkdir -p "$ARCHIVES/partial"
sudo tee /etc/apt/apt.conf.d/99ci-fast-fail >/dev/null <<APT
Acquire::Retries "5";
Acquire::http::Timeout "20";
Acquire::https::Timeout "20";
Dir::Cache::Archives "$ARCHIVES";
APT

npm ci --no-audit --no-fund

for attempt in $(seq 1 "$ATTEMPTS"); do
  if [ "$attempt" -gt 1 ] && [ -f /etc/apt/apt-mirrors.txt ]; then
    echo "==> attempt $attempt: switching to archive.ubuntu.com"
    echo "http://archive.ubuntu.com/ubuntu/ priority:1" | sudo tee /etc/apt/apt-mirrors.txt >/dev/null
  fi
  echo "==> attempt $attempt of $ATTEMPTS (limit ${ATTEMPT_SECONDS}s)"
  sudo dpkg --configure -a || true
  # Root needs the limit: only root can signal the apt that Playwright starts.
  if sudo timeout -k 10 "$ATTEMPT_SECONDS" env "PATH=$PATH" "HOME=$HOME" \
      npx playwright install-deps "$@"; then
    sudo chmod -R a+rX "$ARCHIVES"
    exit 0
  fi
  echo "==> attempt $attempt did not finish" >&2
done
sudo chmod -R a+rX "$ARCHIVES" || true
echo "system dependencies could not be installed" >&2
exit 1
