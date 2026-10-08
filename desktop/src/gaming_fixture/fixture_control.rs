//! Private owned stdin-pipe control record. Never renderer IPC or game discovery.
use crate::native_input::shortcuts::{Binding, Modifiers};
pub const HELLO_SIZE: usize = 32;
pub struct ParentHello {
    pub pid: u32,
    pub creation: u64,
    pub overlay: Binding,
}
impl ParentHello {
    pub fn encode(&self) -> [u8; HELLO_SIZE] {
        let mut bytes = [0; HELLO_SIZE];
        bytes[..4].copy_from_slice(b"FP01");
        bytes[4..8].copy_from_slice(&self.pid.to_le_bytes());
        bytes[8..16].copy_from_slice(&self.creation.to_le_bytes());
        bytes[16..18].copy_from_slice(&self.overlay.key().to_le_bytes());
        let modifiers = self.overlay.modifiers();
        bytes[18] = u8::from(modifiers.alt)
            | u8::from(modifiers.control) << 1
            | u8::from(modifiers.shift) << 2;
        bytes
    }
    pub fn decode(bytes: [u8; HELLO_SIZE]) -> Option<Self> {
        if bytes[..4] != *b"FP01"
            || bytes[18] & !7 != 0
            || bytes[19..].iter().any(|&value| value != 0)
        {
            return None;
        }
        let pid = u32::from_le_bytes(bytes[4..8].try_into().ok()?);
        let creation = u64::from_le_bytes(bytes[8..16].try_into().ok()?);
        if pid == 0 || creation == 0 {
            return None;
        }
        let overlay = Binding::new(
            u16::from_le_bytes(bytes[16..18].try_into().ok()?),
            Modifiers {
                alt: bytes[18] & 1 != 0,
                control: bytes[18] & 2 != 0,
                shift: bytes[18] & 4 != 0,
                windows: false,
            },
        )
        .ok()?;
        Some(Self {
            pid,
            creation,
            overlay,
        })
    }
    /// Both native values are obtained independently in the child: OS spawning
    /// parent and retained QUERY_LIMITED_INFORMATION process creation FILETIME.
    pub fn peer_matches(&self, os_parent: u32, retained_creation: u64, child_pid: u32) -> bool {
        self.creation != 0
            && os_parent != 0
            && self.pid == os_parent
            && self.pid != child_pid
            && self.creation == retained_creation
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    fn hello() -> ParentHello {
        ParentHello {
            pid: 7,
            creation: 100,
            overlay: Binding::new(
                b'M' as u16,
                Modifiers {
                    alt: true,
                    ..Default::default()
                },
            )
            .unwrap(),
        }
    }
    #[test]
    fn pipe_header_cannot_choose_unrelated_same_child_or_reused_parent_pid() {
        let value = ParentHello::decode(hello().encode()).unwrap();
        assert!(value.peer_matches(7, 100, 9));
        assert!(!value.peer_matches(8, 100, 9));
        assert!(!value.peer_matches(7, 101, 9));
        assert!(!value.peer_matches(7, 100, 7));
    }
    #[test]
    fn malformed_and_reserved_control_records_reject() {
        for offset in [0, 18, 19] {
            let mut bytes = hello().encode();
            bytes[offset] = 255;
            assert!(ParentHello::decode(bytes).is_none());
        }
        let mut bytes = hello().encode();
        bytes[16..18].copy_from_slice(&0x73u16.to_le_bytes());
        assert!(ParentHello::decode(bytes).is_none());
    }
}
