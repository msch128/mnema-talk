//! Ignored native-vault experiment. No UI, transport, real credential fixtures, or readiness claim.
#![deny(unsafe_op_in_unsafe_fn)]
use sha2::{Digest, Sha256};
use std::fmt;
use url::Url;
use zeroize::{Zeroize, Zeroizing};

#[cfg(test)]
mod tests;
#[cfg(windows)]
pub mod windows;

pub const RECORD_LEN: usize = 104;
const MAX_SEQUENCE: u64 = 65535;
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Error {
    InvalidInput,
    Busy,
    OwnershipUncertain,
    StoreUnavailable,
    WriteUncertain,
    Corrupt,
    Conflict,
    ReauthRequired,
    Poisoned,
}
impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("native vault operation rejected")
    }
}
impl std::error::Error for Error {}

#[derive(Clone, PartialEq, Eq)]
pub struct Namespace {
    digest: [u8; 32],
}
impl fmt::Debug for Namespace {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("Namespace(REDACTED)")
    }
}
impl Namespace {
    /// Lexical validation only. Caller must separately verify community/account/instance identity.
    pub fn checked(
        origin: &str,
        base_path: &str,
        community: &str,
        account: [u8; 16],
        instance: [u8; 16],
    ) -> Result<Self, Error> {
        if origin.len() > 2048
            || !origin.starts_with("https://")
            || origin.contains('@')
            || origin
                .get(8..)
                .is_none_or(|s| s.is_empty() || s.strip_suffix('/').unwrap_or(s).contains('/'))
            || origin
                .bytes()
                .any(|b| b.is_ascii_whitespace() || b.is_ascii_control() || b == b'\\')
        {
            return Err(Error::InvalidInput);
        }
        let parsed = Url::parse(origin).map_err(|_| Error::InvalidInput)?;
        if parsed.scheme() != "https"
            || parsed.host_str().is_none()
            || !parsed.username().is_empty()
            || parsed.password().is_some()
            || parsed.query().is_some()
            || parsed.fragment().is_some()
            || parsed.path() != "/"
        {
            return Err(Error::InvalidInput);
        }
        if base_path.len() > 256
            || !base_path.starts_with('/')
            || !base_path.ends_with('/')
            || (base_path != "/"
                && base_path[1..base_path.len() - 1].split('/').any(|s| {
                    s.is_empty()
                        || !s
                            .bytes()
                            .all(|b| b.is_ascii_alphanumeric() || b == b'-' || b == b'_')
                }))
        {
            return Err(Error::InvalidInput);
        }
        if community.is_empty()
            || community.len() > 128
            || !community
                .bytes()
                .all(|b| b.is_ascii_alphanumeric() || b == b'-' || b == b'_')
            || account == [0; 16]
            || instance == [0; 16]
        {
            return Err(Error::InvalidInput);
        }
        let canonical = parsed.origin().ascii_serialization();
        let mut h = Sha256::new();
        h.update(b"mnema-native-vault-namespace-v1\0");
        for field in [
            canonical.as_bytes(),
            base_path.as_bytes(),
            community.as_bytes(),
            &account,
            &instance,
        ] {
            h.update((field.len() as u32).to_be_bytes());
            h.update(field);
        }
        Ok(Self {
            digest: h.finalize().into(),
        })
    }
    #[cfg(any(windows, test))]
    pub(crate) fn target(&self) -> String {
        format!("MnemaTalk.Dev.NativeRefresh.v1.{}", hex(&self.digest))
    }
}
#[cfg(any(windows, test))]
fn hex(bytes: &[u8]) -> String {
    const H: &[u8; 16] = b"0123456789abcdef";
    let mut out = String::with_capacity(bytes.len() * 2);
    for &b in bytes {
        out.push(H[(b >> 4) as usize] as char);
        out.push(H[(b & 15) as usize] as char);
    }
    out
}

/// Opaque owned OS-read bytes. Debug never delegates to Zeroizing's Vec formatting.
pub struct SecretBlob(Zeroizing<Vec<u8>>);
impl SecretBlob {
    #[cfg(any(windows, test))]
    pub(crate) fn new(bytes: Vec<u8>) -> Self {
        Self(Zeroizing::new(bytes))
    }
    pub(crate) fn as_slice(&self) -> &[u8] {
        self.0.as_slice()
    }
}
impl fmt::Debug for SecretBlob {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("SecretBlob(REDACTED)")
    }
}

pub struct Refresh([u8; 32]);
impl Refresh {
    pub fn from_native_bytes(bytes: [u8; 32]) -> Result<Self, Error> {
        if bytes == [0; 32] {
            return Err(Error::InvalidInput);
        }
        Ok(Self(bytes))
    }
}
impl Drop for Refresh {
    fn drop(&mut self) {
        self.0.zeroize();
    }
}
impl fmt::Debug for Refresh {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("Refresh(REDACTED)")
    }
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Phase {
    Idle,
    InFlight,
}
pub struct Record {
    namespace: [u8; 32],
    family: [u8; 16],
    sequence: u64,
    attempt: u64,
    phase: Phase,
    refresh: Refresh,
}
impl fmt::Debug for Record {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("Record(REDACTED)")
    }
}
impl Record {
    pub fn fresh(
        namespace: &Namespace,
        family: [u8; 16],
        sequence: u64,
        refresh: Refresh,
    ) -> Result<Self, Error> {
        if family == [0; 16] || sequence > MAX_SEQUENCE {
            return Err(Error::InvalidInput);
        }
        Ok(Self {
            namespace: namespace.digest,
            family,
            sequence,
            attempt: 0,
            phase: Phase::Idle,
            refresh,
        })
    }
    fn encode(&self) -> Zeroizing<Vec<u8>> {
        let mut bytes = Zeroizing::new(vec![0; RECORD_LEN]);
        bytes[..4].copy_from_slice(b"MNVR");
        bytes[4] = 1;
        bytes[5] = if self.phase == Phase::Idle { 0 } else { 1 };
        bytes[8..40].copy_from_slice(&self.namespace);
        bytes[40..56].copy_from_slice(&self.family);
        bytes[56..64].copy_from_slice(&self.sequence.to_be_bytes());
        bytes[64..72].copy_from_slice(&self.attempt.to_be_bytes());
        bytes[72..104].copy_from_slice(&self.refresh.0);
        bytes
    }
    fn decode(bytes: &[u8], namespace: &Namespace) -> Result<Self, Error> {
        if bytes.len() != RECORD_LEN
            || &bytes[..4] != b"MNVR"
            || bytes[4] != 1
            || bytes[6] != 0
            || bytes[7] != 0
            || bytes[8..40] != namespace.digest
        {
            return Err(Error::Corrupt);
        }
        let phase = match bytes[5] {
            0 => Phase::Idle,
            1 => Phase::InFlight,
            _ => return Err(Error::Corrupt),
        };
        let family = bytes[40..56].try_into().map_err(|_| Error::Corrupt)?;
        let sequence = u64::from_be_bytes(bytes[56..64].try_into().map_err(|_| Error::Corrupt)?);
        let attempt = u64::from_be_bytes(bytes[64..72].try_into().map_err(|_| Error::Corrupt)?);
        let refresh =
            Refresh::from_native_bytes(bytes[72..104].try_into().map_err(|_| Error::Corrupt)?)
                .map_err(|_| Error::Corrupt)?;
        if family == [0; 16]
            || sequence > MAX_SEQUENCE
            || (phase == Phase::Idle && attempt != 0)
            || (phase == Phase::InFlight && attempt == 0)
        {
            return Err(Error::Corrupt);
        }
        Ok(Self {
            namespace: namespace.digest,
            family,
            sequence,
            attempt,
            phase,
            refresh,
        })
    }
}
/// Native guard must hold process ownership until drop, including across network dispatch.
/// Implementations never accept arbitrary renderer target names or return OS error payloads.
pub trait Boundary {
    type Guard;
    fn acquire(&self, namespace: &Namespace) -> Result<Self::Guard, Error>;
    fn abandoned(&self, guard: &Self::Guard) -> bool;
    fn read(&self, guard: &mut Self::Guard) -> Result<Option<SecretBlob>, Error>;
    /// Any uncertain acknowledgement is Err(WriteUncertain), including failure-after-mutation.
    fn write(&self, guard: &mut Self::Guard, bytes: &[u8]) -> Result<(), Error>;
}
pub struct Vault<B: Boundary> {
    boundary: B,
}
impl<B: Boundary> Vault<B> {
    pub fn new(boundary: B) -> Self {
        Self { boundary }
    }
    pub fn own(&self, namespace: Namespace) -> Result<Session<'_, B>, Error> {
        let mut guard = self.boundary.acquire(&namespace)?;
        if self.boundary.abandoned(&guard) {
            // Preserve a fail-closed journal for subsequent cooperating openers; releasing
            // an abandoned Win32 mutex alone clears its abandonment indication.
            if let Some(bytes) = self.boundary.read(&mut guard)? {
                let mut record = Record::decode(bytes.as_slice(), &namespace)?;
                if record.phase == Phase::Idle {
                    record.phase = Phase::InFlight;
                    record.attempt = u64::MAX;
                    self.boundary.write(&mut guard, &record.encode())?;
                }
            }
            return Err(Error::OwnershipUncertain);
        }
        Ok(Session {
            boundary: &self.boundary,
            namespace,
            guard,
            poisoned: false,
        })
    }
}
pub struct Session<'a, B: Boundary> {
    boundary: &'a B,
    namespace: Namespace,
    guard: B::Guard,
    poisoned: bool,
}
impl<B: Boundary> fmt::Debug for Session<'_, B> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("OwnedVaultSession(REDACTED)")
    }
}
impl<B: Boundary> Session<'_, B> {
    fn ready(&self) -> Result<(), Error> {
        if self.poisoned {
            Err(Error::Poisoned)
        } else if self.boundary.abandoned(&self.guard) {
            Err(Error::OwnershipUncertain)
        } else {
            Ok(())
        }
    }
    fn read_record(&mut self) -> Result<Option<Record>, Error> {
        self.ready()?;
        match self.boundary.read(&mut self.guard).and_then(|b| {
            b.map(|v| Record::decode(v.as_slice(), &self.namespace))
                .transpose()
        }) {
            Ok(v) => Ok(v),
            Err(e) => {
                self.poisoned = true;
                Err(e)
            }
        }
    }
    fn write_record(&mut self, record: &Record) -> Result<(), Error> {
        self.ready()?;
        let bytes = record.encode();
        match self.boundary.write(&mut self.guard, &bytes) {
            Ok(()) => Ok(()),
            Err(e) => {
                self.poisoned = true;
                Err(e)
            }
        }
    }
    /// Fresh grant fixture/native login proof must be independently verified before this call.
    /// Existing credentials are never overwritten by initial installation.
    pub fn install_new(&mut self, record: Record) -> Result<(), Error> {
        if record.namespace != self.namespace.digest || record.phase != Phase::Idle {
            return Err(Error::Conflict);
        }
        if self.read_record()?.is_some() {
            return Err(Error::Conflict);
        }
        self.write_record(&record)
    }
    /// Returns only a native secret object, never access authorization or a renderer DTO.
    pub fn restore_idle(&mut self) -> Result<Option<Record>, Error> {
        let record = self.read_record()?;
        if record.as_ref().is_some_and(|r| r.phase == Phase::InFlight) {
            return Err(Error::ReauthRequired);
        }
        Ok(record)
    }
    /// Journal write must complete before caller dispatches any refresh request.
    /// The returned private record keeps the old token; it must never be retried after dispatch uncertainty.
    pub fn begin_rotation(
        &mut self,
        expected_family: [u8; 16],
        expected_sequence: u64,
        attempt: u64,
    ) -> Result<Record, Error> {
        if attempt == 0 {
            return Err(Error::InvalidInput);
        }
        let mut r = self.restore_idle()?.ok_or(Error::ReauthRequired)?;
        if r.family != expected_family
            || r.sequence != expected_sequence
            || r.sequence == MAX_SEQUENCE
        {
            return Err(Error::Conflict);
        }
        r.phase = Phase::InFlight;
        r.attempt = attempt;
        self.write_record(&r)?;
        Ok(r)
    }
    /// Cooperating-process compare-under-lock, explicitly NOT OS credential-store CAS.
    pub fn commit_successor(&mut self, previous: &Record, successor: Record) -> Result<(), Error> {
        self.ready()?;
        if previous.namespace != self.namespace.digest
            || successor.namespace != self.namespace.digest
            || previous.phase != Phase::InFlight
            || successor.phase != Phase::Idle
            || successor.family != previous.family
            || previous.sequence.checked_add(1) != Some(successor.sequence)
            || successor.refresh.0 == previous.refresh.0
        {
            return Err(Error::Conflict);
        }
        let actual = self.read_record()?.ok_or(Error::Conflict)?;
        if actual.phase != Phase::InFlight
            || actual.family != previous.family
            || actual.sequence != previous.sequence
            || actual.attempt != previous.attempt
            || actual.refresh.0 != previous.refresh.0
        {
            return Err(Error::Conflict);
        }
        self.write_record(&successor)
    }
}

// New separately reviewed research facade; frozen source prefix above is unchanged.
#[cfg(feature = "native-facade")]
mod native_facade;
