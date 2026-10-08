//! PRIVATE synthetic voice control only. No real microphone, media keys, MLS,
//! SFrame or network exists in this fixture. Never a production media grant.
use crate::gaming_fixture::policy::{Generation, SessionGrant};
use std::time::{Duration, Instant};
pub const PACKET_SIZE: usize = 24;
pub const FIXTURE_DEADLINE: Duration = Duration::from_millis(250);

#[derive(Clone, Copy)]
pub struct Packet {
    pub sequence: u64,
    pub thread: u32,
    pub flags: u8,
    pub voice_epoch: u64,
}
impl Packet {
    fn valid(&self) -> bool {
        self.sequence != 0 && self.thread != 0 && self.voice_epoch != 0 && self.flags & !15 == 0
    }
    pub fn encode(self) -> [u8; PACKET_SIZE] {
        let mut bytes = [0; PACKET_SIZE];
        bytes[..8].copy_from_slice(&self.sequence.to_le_bytes());
        bytes[8..12].copy_from_slice(&self.thread.to_le_bytes());
        bytes[12] = self.flags;
        bytes[13..16].copy_from_slice(b"FX1");
        bytes[16..24].copy_from_slice(&self.voice_epoch.to_le_bytes());
        bytes
    }
    pub fn decode(bytes: [u8; PACKET_SIZE]) -> Option<Self> {
        let packet = Self {
            sequence: u64::from_le_bytes(bytes[..8].try_into().ok()?),
            thread: u32::from_le_bytes(bytes[8..12].try_into().ok()?),
            flags: bytes[12],
            voice_epoch: u64::from_le_bytes(bytes[16..24].try_into().ok()?),
        };
        (bytes[13..16] == *b"FX1" && packet.valid()).then_some(packet)
    }
}

pub struct SyntheticSession {
    thread: u32,
    last_sequence: u64,
    last_seen: Instant,
    voice_epoch: u64,
    flags: u8,
    retired: bool,
}
impl SyntheticSession {
    /// Called ONLY for a child stdout pipe owned by our explicit fixture spawn.
    /// No renderer command, server response or external process can enroll here.
    #[cfg(any(windows, test))]
    pub(crate) fn from_owned_child(first: Packet, now: Instant) -> Self {
        Self {
            thread: first.thread,
            last_sequence: first.sequence,
            last_seen: now,
            voice_epoch: first.voice_epoch.max(1),
            flags: if first.valid() { first.flags } else { 0 },
            retired: !first.valid(),
        }
    }
    pub fn thread(&self) -> u32 {
        self.thread
    }
    pub fn observe(&mut self, packet: Packet, received_at: Instant, now: Instant) -> bool {
        if self.retired
            || !packet.valid()
            || packet.thread != self.thread
            || self.last_sequence.checked_add(1) != Some(packet.sequence)
            || packet.voice_epoch < self.voice_epoch
            || (self.flags & 1 != packet.flags & 1 && packet.voice_epoch == self.voice_epoch)
            || received_at < self.last_seen
            || now.saturating_duration_since(received_at) >= FIXTURE_DEADLINE
            || now.saturating_duration_since(self.last_seen) >= FIXTURE_DEADLINE
        {
            self.retire();
            return false;
        }
        self.voice_epoch = packet.voice_epoch;
        self.last_sequence = packet.sequence;
        self.last_seen = received_at;
        self.flags = packet.flags;
        true
    }
    pub fn retire(&mut self) {
        self.retired = true;
        self.flags = 0;
    }
    pub fn active(&self, now: Instant) -> bool {
        !self.retired
            && self.flags & 1 != 0
            && now.saturating_duration_since(self.last_seen) < FIXTURE_DEADLINE
    }
    pub fn states(&self) -> (bool, bool, bool) {
        (
            self.flags & 2 != 0,
            self.flags & 4 != 0,
            self.flags & 8 != 0,
        )
    }
    pub fn grant(&self, now: Instant) -> SessionGrant {
        // true/true is confined to this explicitly synthetic session with NO
        // media. It means fixture permission, not evidence of product E2EE.
        let active = self.active(now);
        SessionGrant::new(
            Generation::new(1).unwrap(),
            Generation::new(self.voice_epoch).unwrap(),
            active,
            active,
            false,
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn packet(seq: u64, flags: u8) -> Packet {
        Packet {
            sequence: seq,
            thread: 7,
            flags,
            voice_epoch: seq.max(1),
        }
    }
    #[test]
    fn wire_rejects_unknown_flags_versions_and_zero_sequence() {
        assert!(Packet::decode(packet(1, 1).encode()).is_some());
        assert!(Packet::decode(packet(0, 1).encode()).is_none());
        assert!(Packet::decode(packet(1, 16).encode()).is_none());
        let mut bytes = packet(1, 1).encode();
        bytes[15] = 0;
        assert!(Packet::decode(bytes).is_none());
    }
    #[test]
    fn pipe_replay_retires_and_cannot_recover() {
        let now = Instant::now();
        let mut session = SyntheticSession::from_owned_child(packet(1, 1), now);
        assert!(session.active(now));
        assert!(!session.observe(packet(1, 1), now, now));
        assert!(!session.observe(packet(2, 1), now, now));
        assert!(!session.active(now));
    }
    #[test]
    fn overdue_packet_cannot_renew_fixture_authority() {
        let now = Instant::now();
        let mut session = SyntheticSession::from_owned_child(packet(1, 1), now);
        assert!(!session.observe(packet(2, 1), now, now + FIXTURE_DEADLINE));
        assert!(!session.active(now));
    }
    #[test]
    fn leave_and_rejoin_change_voice_grant() {
        let now = Instant::now();
        let mut session = SyntheticSession::from_owned_child(packet(1, 1), now);
        let before = session.grant(now);
        assert!(session.observe(packet(2, 0), now, now));
        assert_ne!(before, session.grant(now));
        assert!(!session.active(now));
        assert!(session.observe(packet(3, 1), now, now));
        assert_ne!(before, session.grant(now));
        assert!(session.active(now));
    }
    #[test]
    fn join_leave_between_two_heartbeats_still_changes_voice_identity() {
        let now = Instant::now();
        let mut session = SyntheticSession::from_owned_child(packet(1, 1), now);
        let before = session.grant(now);
        let mut next = packet(2, 1);
        next.voice_epoch = 3;
        assert!(session.observe(next, now, now));
        assert_ne!(before, session.grant(now));
    }
    #[test]
    fn skipped_packet_or_voice_epoch_regression_retires() {
        for mut next in [packet(3, 1), packet(2, 1)] {
            let now = Instant::now();
            let mut first = packet(1, 1);
            first.voice_epoch = 5;
            let mut session = SyntheticSession::from_owned_child(first, now);
            next.voice_epoch = 4;
            assert!(!session.observe(next, now, now));
        }
    }
    #[test]
    fn native_malformed_epoch_or_flags_cannot_panic_or_grant() {
        for kind in [0, 1] {
            let now = Instant::now();
            let mut invalid = packet(1, 1);
            if kind == 0 {
                invalid.voice_epoch = 0;
            } else {
                invalid.flags = 16;
            }
            let first = SyntheticSession::from_owned_child(invalid, now);
            assert!(!first.active(now));
            let _ = first.grant(now);
            let mut session = SyntheticSession::from_owned_child(packet(1, 1), now);
            invalid.sequence = 2;
            assert!(!session.observe(invalid, now, now));
        }
    }
    #[test]
    fn thread_replacement_fails_closed() {
        let now = Instant::now();
        let mut session = SyntheticSession::from_owned_child(packet(1, 1), now);
        let mut replacement = packet(2, 1);
        replacement.thread = 8;
        assert!(!session.observe(replacement, now, now));
    }
}
