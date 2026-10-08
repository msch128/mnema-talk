//! Fresh native crypto seed custody; no imported or restored-live constructor.
//! Seed namespaces belong only to this process-created actual authenticated
//! owner. Persistence protects seeds at rest; it does not prove crash recovery,
//! CAS, fsync, or resumed MLS ratchet freshness.
#![cfg(any(windows, test))]
#[cfg(windows)]
use mnema_private_native_client_broker::NativeAuthenticatedScope;
use sha2::{Digest, Sha256};
use std::{cell::RefCell, collections::HashMap, fmt, marker::PhantomData, rc::Rc};
#[cfg(windows)]
use uuid::Uuid;
use zeroize::Zeroizing;
const RECORD_LEN: usize = 32;
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Error {
    InvalidInput,
    Busy,
    OwnershipUncertain,
    StoreUnavailable,
    WriteUncertain,
    Corrupt,
    Quarantined,
}
impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("native crypto custody unavailable")
    }
}
impl std::error::Error for Error {}
#[cfg(windows)]
fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}
struct Namespace {
    digest: [u8; 32],
}
impl Namespace {
    fn alias(&self, alias: &str) -> Self {
        let mut hash = Sha256::new();
        hash.update(b"mnema/native/crypto-seed/alias/v1\0");
        hash.update(self.digest);
        hash.update(alias.as_bytes());
        Self {
            digest: hash.finalize().into(),
        }
    }
    #[cfg(windows)]
    fn target(&self) -> String {
        format!("MnemaTalk/Dev/CryptoSeed/v1/{}", hex(&self.digest))
    }
}
/// No Debug/Serde/byte projection; temporary OS buffers remain zeroizing.
struct SecretBlob(Zeroizing<Vec<u8>>);
impl SecretBlob {
    fn new(value: Vec<u8>) -> Self {
        Self(Zeroizing::new(value))
    }
    fn as_slice(&self) -> &[u8] {
        &self.0
    }
}
trait Boundary {
    type Guard;
    fn acquire(&self, namespace: &Namespace) -> Result<Self::Guard, Error>;
    fn abandoned(&self, guard: &Self::Guard) -> bool;
    fn read(&self, guard: &mut Self::Guard) -> Result<Option<SecretBlob>, Error>;
    fn write(&self, guard: &mut Self::Guard, bytes: &[u8]) -> Result<(), Error>;
}
#[derive(Clone, Copy)]
enum State {
    Live([u8; 32]),
    Quarantined,
}
struct Vault<B: Boundary> {
    boundary: B,
    namespace: Namespace,
    states: RefCell<HashMap<String, State>>,
    _thread_bound: PhantomData<Rc<()>>,
}
impl<B: Boundary> Vault<B> {
    fn alias(alias: &str) -> Result<(), Error> {
        if ["issuer-root", "database-key", "device-key"].contains(&alias) {
            Ok(())
        } else {
            Err(Error::InvalidInput)
        }
    }
    fn create(&self, alias: &str) -> Result<Zeroizing<[u8; 32]>, Error> {
        Self::alias(alias)?;
        if self.states.borrow().contains_key(alias) {
            return Err(Error::Quarantined);
        }
        // Before any possible OS write, a missing/ambiguous ACK becomes a burned
        // alias. No automatic retry or import of an orphaned credential exists.
        self.states
            .borrow_mut()
            .insert(alias.into(), State::Quarantined);
        let mut guard = self.boundary.acquire(&self.namespace.alias(alias))?;
        if self.boundary.abandoned(&guard) {
            return Err(Error::OwnershipUncertain);
        }
        if self.boundary.read(&mut guard)?.is_some() {
            return Err(Error::Quarantined);
        }
        let mut seed = Zeroizing::new([0; 32]);
        getrandom::fill(seed.as_mut()).map_err(|_| Error::StoreUnavailable)?;
        self.boundary
            .write(&mut guard, seed.as_slice())
            .map_err(|_| Error::WriteUncertain)?;
        let observed = self
            .boundary
            .read(&mut guard)
            .map_err(|_| Error::WriteUncertain)?
            .ok_or(Error::WriteUncertain)?;
        if observed.as_slice() != seed.as_slice() {
            return Err(Error::WriteUncertain);
        }
        self.states.borrow_mut().insert(
            alias.into(),
            State::Live(Sha256::digest(seed.as_slice()).into()),
        );
        Ok(seed)
    }
    fn read(&self, alias: &str) -> Result<Zeroizing<[u8; 32]>, Error> {
        Self::alias(alias)?;
        let expected = match self.states.borrow().get(alias).copied() {
            Some(State::Live(d)) => d,
            _ => return Err(Error::Quarantined),
        };
        let result = (|| {
            let mut guard = self.boundary.acquire(&self.namespace.alias(alias))?;
            if self.boundary.abandoned(&guard) {
                return Err(Error::OwnershipUncertain);
            }
            let bytes = self.boundary.read(&mut guard)?.ok_or(Error::Corrupt)?;
            if bytes.as_slice().len() != RECORD_LEN
                || <[u8; 32]>::from(Sha256::digest(bytes.as_slice())) != expected
            {
                return Err(Error::Corrupt);
            }
            let mut seed = Zeroizing::new([0; 32]);
            seed.copy_from_slice(bytes.as_slice());
            Ok(seed)
        })();
        if result.is_err() {
            self.states
                .borrow_mut()
                .insert(alias.into(), State::Quarantined);
        }
        result
    }
}
#[cfg(windows)]
fn namespace(scope: &NativeAuthenticatedScope) -> Result<Namespace, Error> {
    if scope.native_context().is_none()
        || std::time::Instant::now() >= scope.monotonic_access_deadline()
    {
        return Err(Error::InvalidInput);
    }
    let mut hash = Sha256::new();
    hash.update(b"mnema/native/crypto-seed/owner/v1\0");
    for field in [scope.origin(), scope.community_id()] {
        hash.update((field.len() as u64).to_be_bytes());
        hash.update(field.as_bytes());
    }
    for id in [
        scope.account_id(),
        scope.native_profile_identity(),
        scope.native_window_identity(),
        scope.family_id(),
        scope.client_instance_id(),
        Uuid::new_v4(),
    ] {
        hash.update(id.as_bytes());
    }
    Ok(Namespace {
        digest: hash.finalize().into(),
    })
}
#[cfg(windows)]
mod windows;
/// Process-fresh native seed owner, borrowing the actual opaque authenticated
/// scope for namespace derivation. Core caller must additionally recheck actual
/// current scope/window before and after provisioning and every publication.
#[cfg(windows)]
pub struct WindowsNativeSeeds(Vault<windows::WindowsCredentialManager>);
#[cfg(windows)]
impl WindowsNativeSeeds {
    pub fn from_authenticated_native_owner(
        scope: &NativeAuthenticatedScope,
    ) -> Result<Self, Error> {
        Ok(Self(Vault {
            boundary: windows::WindowsCredentialManager,
            namespace: namespace(scope)?,
            states: RefCell::new(HashMap::new()),
            _thread_bound: PhantomData,
        }))
    }
    pub fn create_seed(&self, alias: &str) -> Result<[u8; 32], Error> {
        let seed = self.0.create(alias)?;
        Ok(ed25519_dalek::SigningKey::from_bytes(&seed)
            .verifying_key()
            .to_bytes())
    }
    pub fn read_seed(&self, alias: &str) -> Result<Zeroizing<[u8; 32]>, Error> {
        self.0.read(alias)
    }
}
#[cfg(test)]
mod tests;
