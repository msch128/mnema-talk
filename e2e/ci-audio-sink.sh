#!/usr/bin/env bash
# CI only: gives the runner an audio output device (a PulseAudio null sink), as a
# desktop has. Without one Firefox never starts an AudioContext, so nothing that
# is routed through the Web Audio graph (the AI noise filter) processes audio.
# Usage: e2e/ci-audio-sink.sh
set -euo pipefail

sudo timeout -k 10 180 apt-get install -y --no-install-recommends pulseaudio pulseaudio-utils
pulseaudio --start --exit-idle-time=-1 --daemonize
pactl load-module module-null-sink sink_name=ci-sink >/dev/null
pactl set-default-sink ci-sink
pactl info | grep -E "Server Name|Default Sink"
