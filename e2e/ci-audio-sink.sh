#!/usr/bin/env bash
# CI only: gives the runner an audio output device (a PulseAudio null sink), as a
# desktop has. Without one Firefox never starts an AudioContext, so nothing that
# is routed through the Web Audio graph (the AI noise filter) processes audio.
# Usage: e2e/ci-audio-sink.sh
#
# The package install is bounded per attempt, like e2e/ci-install-deps.sh: the
# Azure Ubuntu mirror sometimes trickles bytes, which apt's idle timeout never
# catches, and a single 180s attempt then failed the whole job (exit 124). Later
# attempts switch to archive.ubuntu.com and resume the partial downloads.
#
# The mirror list holds the bare URL: a "priority:1" suffix separated by a space
# (apt wants a tab there) ends up inside the URL and every .deb request then
# fails with "400 Bad Request" (run 42090807808). Downloads go to the same
# ~/apt-archives as ci-install-deps.sh, which the workflow caches.
set -euo pipefail
# Before any cd: the path of this script may be relative.
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

ATTEMPTS="${ATTEMPTS:-3}"
ATTEMPT_SECONDS="${ATTEMPT_SECONDS:-60}"
ARCHIVES="$HOME/apt-archives"

mkdir -p "$ARCHIVES/partial"

sudo tee /etc/apt/apt.conf.d/99ci-fast-fail-audio >/dev/null <<APT
Acquire::Retries "5";
Acquire::http::Timeout "20";
Acquire::https::Timeout "20";
Dir::Cache::Archives "$ARCHIVES";
APT

installed=0
for attempt in $(seq 1 "$ATTEMPTS"); do
  if [ "$attempt" -gt 1 ] && [ -f /etc/apt/apt-mirrors.txt ]; then
    echo "==> attempt $attempt: switching to archive.ubuntu.com"
    echo "http://archive.ubuntu.com/ubuntu/" | sudo tee /etc/apt/apt-mirrors.txt >/dev/null
  fi
  echo "==> attempt $attempt of $ATTEMPTS (limit ${ATTEMPT_SECONDS}s)"
  sudo dpkg --configure -a || true
  "$REPO_ROOT/scripts/ci-dns-fallback.sh" azure.archive.ubuntu.com archive.ubuntu.com || true
  if sudo timeout -k 10 "$ATTEMPT_SECONDS" apt-get install -y --fix-missing --no-install-recommends pulseaudio pulseaudio-utils; then
    installed=1
    sudo chmod -R a+rX "$ARCHIVES"
    break
  fi
  echo "==> attempt $attempt did not finish" >&2
done
if [ "$installed" != 1 ]; then
  echo "pulseaudio could not be installed" >&2
  exit 1
fi

pulseaudio --start --exit-idle-time=-1 --daemonize
pactl load-module module-null-sink sink_name=ci-sink >/dev/null
pactl set-default-sink ci-sink
pactl info | grep -E "Server Name|Default Sink"
