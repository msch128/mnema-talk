//! Ignored native enrollment research. No product trust/bootstrap permission.
use zeroize::Zeroizing;
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Error {
    Invalid,
    NativeUnavailable,
    Random,
    Signature,
    Trust,
    Replay,
    Expired,
    Database,
    Restored,
    Limit,
}
pub type Result<T> = std::result::Result<T, Error>;
pub trait NativeSecrets {
    fn read_seed(&self, alias: &str) -> Result<Zeroizing<[u8; 32]>>;
    fn create_seed(&self, alias: &str) -> Result<[u8; 32]>;
}
fn alias(value: &str) -> Result<()> {
    if value.is_empty()
        || value.len() > 128
        || !value
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b"._-".contains(&b))
    {
        Err(Error::Invalid)
    } else {
        Ok(())
    }
}
#[cfg(target_os = "macos")]
mod native;
#[cfg(target_os = "macos")]
pub use native::NativeVault;
mod pairing;
pub use pairing::{Device, NativeAdmissionIntent, NativeRootPin, ProofResponse};
mod issuer;
pub use issuer::Issuer;
#[cfg(test)]
mod tests;
