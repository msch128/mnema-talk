// Test fixture: a GET /api/admin/system response (used by the System tab tests).
export function systemStatus(overrides = {}) {
  return {
    version: { current: '0.4.0', revision: 'abc123def456', go_version: 'go1.27.0' },
    health: {
      database: { reachable: true, latest_migration: '0012_message_count.sql', applied_migrations: 12, pending_migrations: 0 },
      storage: { configured: true, reachable: true, files: 3, total_bytes: 3 * 1024 * 1024, attachment_bytes: 2 * 1024 * 1024, avatar_bytes: 1024 * 1024 },
      voice: {
        enabled: true, rooms: 1, participants: 3, media_connections: 3, screen_shares: 1, cameras: 2,
        websocket_connections: 5, online_users: 4, turn_configured: true, stun_configured: false
      },
      runtime: { started_at: '2026-10-05T10:00:00Z', uptime_seconds: 93784, go_version: 'go1.27.0', goroutines: 42, mem_alloc_bytes: 10 * 1024 * 1024, mem_sys_bytes: 20 * 1024 * 1024 }
    },
    update: {
      check_enabled: true, current_version: '0.4.0', latest_version: '0.4.0', update_available: false,
      release_url: '', release_notes: '', published_at: null, checked_at: '2026-10-05T11:00:00Z', check_error: '', retry_at: null
    },
    self_update: { configured: false, image: '', reach: 'unknown', reach_reason: 'unset', available: false, next_allowed_at: null },
    ...overrides
  }
}
