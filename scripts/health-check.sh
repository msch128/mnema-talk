#!/usr/bin/env sh
# ==============================================================================
# Mnema Talk - Health Check & Uptime Monitoring Script
# ==============================================================================
# Usage:
#   ./scripts/health-check.sh
#   MNEMA_URL=http://localhost:8080 ./scripts/health-check.sh
#
# Can be run via Cron on the host/NAS (e.g. every 5 minutes):
#   */5 * * * * /path/to/mnema-talk/scripts/health-check.sh >/dev/null 2>&1
#
# On failure:
#   - Triggers Unraid notify if available (/usr/local/emhttp/webGui/scripts/notify)
#   - Posts to $ALERT_WEBHOOK_URL if configured
#   - Exits with status code 1
# ==============================================================================

set -eu

URL="${MNEMA_URL:-http://127.0.0.1:8080}/api/health"
TIMEOUT="${HEALTH_TIMEOUT:-5}"

notify_failure() {
    msg="Mnema Talk health check failed at $(date -u +"%Y-%m-%dT%H:%M:%SZ") for ${URL}"
    echo "[ALERT] $msg" >&2

    # Unraid notification
    if [ -x "/usr/local/emhttp/webGui/scripts/notify" ]; then
        /usr/local/emhttp/webGui/scripts/notify \
            -e "Mnema Talk Alert" \
            -s "Mnema Talk Health Check Failed" \
            -d "$msg" \
            -i "alert" || true
    fi

    # Webhook alert (Discord, Slack, or generic webhook)
    if [ -n "${ALERT_WEBHOOK_URL:-}" ]; then
        curl -s -X POST -H "Content-Type: application/json" \
            -d "{\"text\":\"$msg\",\"content\":\"$msg\"}" \
            "${ALERT_WEBHOOK_URL}" >/dev/null 2>&1 || true
    fi
}

# Fetch health endpoint
RESPONSE=""
if command -v curl >/dev/null 2>&1; then
    RESPONSE=$(curl -sf --max-time "${TIMEOUT}" "${URL}" 2>/dev/null || echo "")
elif command -v wget >/dev/null 2>&1; then
    RESPONSE=$(wget -qO- -T "${TIMEOUT}" "${URL}" 2>/dev/null || echo "")
else
    echo "Error: neither curl nor wget found in PATH" >&2
    exit 1
fi

if [ -z "$RESPONSE" ]; then
    notify_failure
    exit 1
fi

# Check healthy status in response
case "$RESPONSE" in
    *"healthy"*)
        exit 0
        ;;
    *)
        notify_failure
        exit 1
        ;;
esac
