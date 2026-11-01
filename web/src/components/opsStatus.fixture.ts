// Test fixture: a GET /api/admin/system/ops response.
import type { OpsStatus, OpsSample } from '../types/domain'

export function opsSample(i: number, overrides: Partial<OpsSample> = {}): OpsSample {
  return {
    t: 1_790_000_000 + i * 10, cpu_millicores: 50 + i * 10, rss_bytes: (100 + i) * 1024 * 1024, heap_bytes: 40 * 1024 * 1024,
    goroutines: 120 + i, online_users: 3, peers: 2, screen_shares: 1, egress_bps: 2_500_000 + i, ingress_bps: 800, egress_pps: 300, ingress_pps: 30,
    requests: 15, errors_5xx: 0, ice_failures: 0, db_connections_acquired: 1, ws_connections: 4, ...overrides
  }
}

export function opsStatus(overrides: Partial<OpsStatus> = {}): OpsStatus {
  return {
    interval_seconds: 10,
    history: [opsSample(0), opsSample(1), opsSample(2, { errors_5xx: 2 })],
    totals: {
      requests: 1234, errors_4xx: 12, errors_5xx: 2, rate_limited: 3, ws_connections: 40, ws_dropped: 1,
      mls_handshakes_relayed: 7, mls_handshakes_rejected: 0, ice_connected: 9, ice_failed: 1, ice_disconnected: 2,
      nack_requests: 55, keyframe_requests: 6, layer_switches: 4, egress_bytes: 5 * 1024 * 1024, ingress_bytes: 1024,
      storage_errors: 0, storage_operations: 20
    },
    db_pool: { total: 4, idle: 3, acquired: 1, max: 10, waited: 0 },
    process: { cpu_ms: 12500, rss_bytes: 100 * 1024 * 1024, heap_bytes: 40 * 1024 * 1024, goroutines: 120, gc_cycles: 30, gc_pause_ms: 12 },
    ...overrides
  }
}
