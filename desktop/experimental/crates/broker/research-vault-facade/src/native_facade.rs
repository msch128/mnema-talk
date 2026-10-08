//! Native-only ignored research access; no renderer, Serialize, persistent activation or diagnostics.
use super::{Error, Namespace, Phase, RECORD_LEN, Record, SecretBlob};
use zeroize::Zeroizing;
impl SecretBlob {
    /// Copies a bounded native store/ephemeral journal read, not arbitrary IPC bytes.
    pub fn from_native_journal_read(bytes: &[u8]) -> Result<Self, Error> {
        if bytes.len() != RECORD_LEN {
            return Err(Error::Corrupt);
        }
        Ok(Self(Zeroizing::new(bytes.to_vec())))
    }
    #[cfg(feature = "test-fixture")]
    pub fn from_test_fixture(bytes: Vec<u8>) -> Self {
        Self(Zeroizing::new(bytes))
    }
}
impl Record {
    /// A native closure controls the only secret export. No wire serializer is added.
    pub fn with_native_refresh<R>(&self, consume: impl FnOnce(&[u8; 32]) -> R) -> R {
        consume(&self.refresh.0)
    }
    pub fn native_metadata(&self) -> ([u8; 16], u64, Phase) {
        (self.family, self.sequence, self.phase)
    }
}

impl Namespace {
    /// Native metadata lookup key, never an authorization credential or renderer API.
    pub fn native_namespace_key(&self) -> [u8; 32] {
        self.digest
    }
}
