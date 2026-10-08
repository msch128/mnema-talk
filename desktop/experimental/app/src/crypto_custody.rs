//! Native-owned fresh DEV ceremony custody. Never default Keychain selection,
//! remembered authentication, restored-live activation or renderer paths.
use mnema_crypto_enrollment_prototype::{Error, NativeSecrets, Result};
use mnema_private_native_client_broker::NativeAuthenticatedScope;
use std::path::Path;
use zeroize::Zeroizing;

#[cfg(windows)]
pub(super) struct Secrets(mnema_private_native_crypto_seeds::WindowsNativeSeeds);
#[cfg(windows)]
impl Secrets {
    pub(super) fn create(scope: &NativeAuthenticatedScope, _: &Path) -> Result<Self> {
        Ok(Self(
            mnema_private_native_crypto_seeds::WindowsNativeSeeds::from_authenticated_native_owner(
                scope,
            )
            .map_err(|_| Error::NativeUnavailable)?,
        ))
    }
}
#[cfg(windows)]
impl NativeSecrets for Secrets {
    fn create_seed(&self, alias: &str) -> Result<[u8; 32]> {
        self.0
            .create_seed(alias)
            .map_err(|_| Error::NativeUnavailable)
    }
    fn read_seed(&self, alias: &str) -> Result<Zeroizing<[u8; 32]>> {
        self.0
            .read_seed(alias)
            .map_err(|_| Error::NativeUnavailable)
    }
}
#[cfg(target_os = "macos")]
mod mac {
    use super::*;
    use core_foundation::base::{CFTypeRef, TCFType};
    use mnema_crypto_enrollment_prototype::NativeVault;
    use security_framework::os::macos::keychain::SecKeychain;
    use security_framework_sys::keychain::SecKeychainCreate;
    use std::{ffi::CString, os::unix::ffi::OsStrExt, ptr};
    #[link(name = "Security", kind = "framework")]
    unsafe extern "C" {
        fn SecKeychainLock(keychain: security_framework_sys::base::SecKeychainRef) -> i32;
        fn SecKeychainDelete(keychain: CFTypeRef) -> i32;
    }
    pub(super) struct Owned {
        pub(super) vault: NativeVault,
        keychain: SecKeychain,
        // This DEV owner deliberately cannot reactivate a restored ratchet. The
        // fresh owned Keychain's unlock credential remains native memory only.
        _password: Zeroizing<String>,
        adopted: std::cell::Cell<bool>,
    }
    impl Owned {
        pub(super) fn create(scope: &NativeAuthenticatedScope, dir: &Path) -> Result<Self> {
            if scope.native_context().is_none() {
                return Err(Error::Invalid);
            }
            let path = dir.join("owned-dev.keychain");
            if path.exists() {
                return Err(Error::Invalid);
            }
            let path = CString::new(path.as_os_str().as_bytes()).map_err(|_| Error::Invalid)?;
            let mut random = Zeroizing::new([0; 32]);
            getrandom::fill(random.as_mut()).map_err(|_| Error::Random)?;
            let mut password = Zeroizing::new(String::new());
            use std::fmt::Write;
            for b in random.iter() {
                write!(&mut *password, "{b:02x}").map_err(|_| Error::Invalid)?;
            }
            let mut raw = ptr::null_mut();
            // Exact native-owned fresh path/password; promptUser=false; no
            // search-list/default Keychain or unrelated-entry mutation.
            if unsafe {
                SecKeychainCreate(
                    path.as_ptr(),
                    password.len() as u32,
                    password.as_ptr().cast(),
                    0,
                    ptr::null_mut(),
                    &mut raw,
                )
            } != 0
                || raw.is_null()
            {
                return Err(Error::NativeUnavailable);
            }
            let keychain = unsafe { SecKeychain::wrap_under_create_rule(raw) };
            let service = format!("mnema-dev-root-{}", uuid::Uuid::new_v4());
            let vault = match NativeVault::from_owned_native_keychain(keychain.clone(), &service) {
                Ok(vault) => vault,
                Err(error) => {
                    // A failed façade construction must not leave an unlocked
                    // fresh keychain outside the normal Owned drop guard.
                    unsafe {
                        SecKeychainDelete(keychain.as_CFTypeRef());
                    }
                    return Err(error);
                }
            };
            Ok(Self {
                vault,
                keychain,
                _password: password,
                adopted: std::cell::Cell::new(false),
            })
        }
        pub(super) fn adopted(&self) {
            self.adopted.set(true);
        }
    }
    impl Drop for Owned {
        fn drop(&mut self) {
            // Only the exact reference created above. Confirmed encrypted state
            // is preserved inspection-only; canceled setup removes owned keys.
            unsafe {
                if self.adopted.get() {
                    SecKeychainLock(self.keychain.as_concrete_TypeRef());
                } else {
                    SecKeychainDelete(self.keychain.as_CFTypeRef());
                }
            }
        }
    }
}
#[cfg(target_os = "macos")]
pub(super) struct Secrets(mac::Owned);
#[cfg(target_os = "macos")]
impl Secrets {
    pub(super) fn create(scope: &NativeAuthenticatedScope, dir: &Path) -> Result<Self> {
        Ok(Self(mac::Owned::create(scope, dir)?))
    }
    pub(super) fn adopted(&self) {
        self.0.adopted();
    }
}
#[cfg(target_os = "macos")]
impl NativeSecrets for Secrets {
    fn create_seed(&self, a: &str) -> Result<[u8; 32]> {
        self.0.vault.create_seed(a)
    }
    fn read_seed(&self, a: &str) -> Result<Zeroizing<[u8; 32]>> {
        self.0.vault.read_seed(a)
    }
}
#[cfg(not(target_os = "macos"))]
impl Secrets {
    pub(super) fn adopted(&self) {}
}
#[cfg(not(any(target_os = "macos", windows)))]
pub(super) struct Secrets;
#[cfg(not(any(target_os = "macos", windows)))]
impl Secrets {
    pub(super) fn create(_: &NativeAuthenticatedScope, _: &Path) -> Result<Self> {
        Err(Error::NativeUnavailable)
    }
}
#[cfg(not(any(target_os = "macos", windows)))]
impl NativeSecrets for Secrets {
    fn create_seed(&self, _: &str) -> Result<[u8; 32]> {
        Err(Error::NativeUnavailable)
    }
    fn read_seed(&self, _: &str) -> Result<Zeroizing<[u8; 32]>> {
        Err(Error::NativeUnavailable)
    }
}
