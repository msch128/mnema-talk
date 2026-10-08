use super::*;
use ed25519_dalek::SigningKey;
use security_framework::os::macos::keychain::SecKeychain;
/// The owner supplies an already reviewed, explicit native Keychain and namespace.
/// No default Keychain/root lookup, and no silent replace-existing behavior.
pub struct NativeVault {
    keychain: SecKeychain,
    service: String,
}
impl NativeVault {
    #[cfg(test)]
    pub(crate) fn service_for_fixture(&self) -> String {
        self.service.clone()
    }
    pub fn from_owned_native_keychain(keychain: SecKeychain, service: &str) -> Result<Self> {
        alias(service)?;
        Ok(Self {
            keychain,
            service: service.into(),
        })
    }
}
impl NativeSecrets for NativeVault {
    fn read_seed(&self, account: &str) -> Result<Zeroizing<[u8; 32]>> {
        alias(account)?;
        let (password, _) = self
            .keychain
            .find_generic_password(&self.service, account)
            .map_err(|_| Error::NativeUnavailable)?;
        if password.as_ref().len() != 32 {
            return Err(Error::NativeUnavailable);
        }
        let mut seed = Zeroizing::new([0; 32]);
        seed.copy_from_slice(password.as_ref());
        Ok(seed)
    }
    fn create_seed(&self, account: &str) -> Result<[u8; 32]> {
        alias(account)?;
        let mut seed = Zeroizing::new([0; 32]);
        getrandom::fill(seed.as_mut()).map_err(|_| Error::Random)?;
        self.keychain
            .add_generic_password(&self.service, account, seed.as_slice())
            .map_err(|_| Error::NativeUnavailable)?;
        Ok(SigningKey::from_bytes(&seed).verifying_key().to_bytes())
    }
}
impl mnema_crypto_core_prototype::NativeKeyProvider for NativeVault {
    fn database_key(&self) -> mnema_crypto_core_prototype::Result<Zeroizing<[u8; 32]>> {
        self.read_seed("database-key")
            .map_err(|_| mnema_crypto_core_prototype::Error::KeyUnavailable)
    }
}
#[cfg(test)]
pub(crate) mod fixture {
    use super::*;
    use core_foundation::{
        array::{CFArray, CFArrayRef},
        base::{CFType, CFTypeRef, TCFType},
    };
    use security_framework_sys::{
        base::SecKeychainRef,
        keychain::{
            SecKeychainCreate, SecKeychainGetUserInteractionAllowed,
            SecKeychainSetUserInteractionAllowed,
        },
    };
    use std::{ffi::CString, os::unix::ffi::OsStrExt, path::PathBuf, ptr};
    #[link(name = "Security", kind = "framework")]
    unsafe extern "C" {
        fn SecKeychainDelete(keychain: CFTypeRef) -> i32;
        fn SecKeychainCopySearchList(result: *mut CFArrayRef) -> i32;
        fn SecKeychainLock(keychain: SecKeychainRef) -> i32;
    }
    static FIXTURE_MUTEX: std::sync::Mutex<()> = std::sync::Mutex::new(());
    pub fn serial() -> Result<std::sync::MutexGuard<'static, ()>> {
        FIXTURE_MUTEX.lock().map_err(|_| Error::NativeUnavailable)
    }
    pub struct NoUi {
        previous: u8,
    }
    impl NoUi {
        pub fn begin() -> Result<Self> {
            let mut previous = 0; /* SAFETY: valid output byte; OS public API. */
            if unsafe { SecKeychainGetUserInteractionAllowed(&mut previous) } != 0 {
                return Err(Error::NativeUnavailable);
            } /* SAFETY: Boolean zero explicitly forbids dialogs in this isolated process. */
            if unsafe { SecKeychainSetUserInteractionAllowed(0) } != 0 {
                return Err(Error::NativeUnavailable);
            }
            Ok(Self { previous })
        }
    }
    impl Drop for NoUi {
        fn drop(&mut self) {
            /* SAFETY: restore exactly this process's preexisting Boolean flag. */
            unsafe { SecKeychainSetUserInteractionAllowed(self.previous) };
        }
    }
    pub fn snapshot() -> Result<CFArray<CFType>> {
        let mut raw = ptr::null(); /* SAFETY: valid pointer; result follows Create rule, wrapped and released. No item content is read. */
        if unsafe { SecKeychainCopySearchList(&mut raw) } != 0 || raw.is_null() {
            return Err(Error::NativeUnavailable);
        }
        Ok(unsafe { CFArray::wrap_under_create_rule(raw) })
    }
    pub struct OwnedFixture {
        pub vault: NativeVault,
        pub path: PathBuf,
        password: Zeroizing<String>,
        deleted: bool,
    }
    impl OwnedFixture {
        pub fn create(dir: &std::path::Path) -> Result<Self> {
            let expected = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("evidence/runs");
            if !dir.starts_with(&expected) || !dir.is_dir() {
                return Err(Error::Invalid);
            }
            let path = dir.join("isolated.keychain");
            if path.exists() {
                return Err(Error::Invalid);
            }
            let cpath = CString::new(path.as_os_str().as_bytes()).map_err(|_| Error::Invalid)?;
            let mut random = Zeroizing::new([0; 32]);
            getrandom::fill(random.as_mut()).map_err(|_| Error::Random)?;
            let mut password = Zeroizing::new(String::with_capacity(64));
            use std::fmt::Write;
            for byte in random.iter() {
                write!(&mut *password, "{byte:02x}").map_err(|_| Error::Invalid)?;
            }
            let nonce = hex_public_random()?;
            let service = format!("mnema-fixture-{nonce}");
            let mut raw = ptr::null_mut(); /* SAFETY: bounded NUL-terminated owned fixture path; valid canonical UTF8 password live throughout call; promptUser=false; output wraps retained nonnull reference. */
            let code = unsafe {
                SecKeychainCreate(
                    cpath.as_ptr(),
                    password.len() as u32,
                    password.as_ptr().cast(),
                    0,
                    ptr::null_mut(),
                    &mut raw,
                )
            };
            if code != 0 || raw.is_null() {
                return Err(Error::NativeUnavailable);
            }
            let keychain = unsafe { SecKeychain::wrap_under_create_rule(raw) };
            let vault = NativeVault { keychain, service };
            Ok(Self {
                vault,
                path,
                password,
                deleted: false,
            })
        }
        pub fn lock(&self) -> Result<()> {
            /* SAFETY: retained exact owned fixture, never a null/default keychain. */
            if unsafe { SecKeychainLock(self.vault.keychain.as_concrete_TypeRef()) } == 0 {
                Ok(())
            } else {
                Err(Error::NativeUnavailable)
            }
        }
        pub fn unlock(&self) -> Result<()> {
            let mut keychain = self.vault.keychain.clone();
            keychain
                .unlock(Some(&self.password))
                .map_err(|_| Error::NativeUnavailable)
        }
        pub fn delete(&mut self) -> Result<()> {
            if self.deleted {
                return Ok(());
            } /* SAFETY: exact retained owned fixture reference; never null/default/search-list array. */
            let code = unsafe { SecKeychainDelete(self.vault.keychain.as_CFTypeRef()) };
            if code != 0 {
                return Err(Error::NativeUnavailable);
            }
            self.deleted = true;
            Ok(())
        }
    }
    impl Drop for OwnedFixture {
        fn drop(&mut self) {
            if !self.deleted {
                let _ = self.delete();
            }
        }
    }
    fn hex_public_random() -> Result<String> {
        let mut bytes = [0; 16];
        getrandom::fill(&mut bytes).map_err(|_| Error::Random)?;
        let mut text = String::new();
        use std::fmt::Write;
        for byte in bytes {
            write!(&mut text, "{byte:02x}").map_err(|_| Error::Invalid)?;
        }
        Ok(text)
    }
}
