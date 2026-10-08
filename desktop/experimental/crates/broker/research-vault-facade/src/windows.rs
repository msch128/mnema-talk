//! WinCred + current-user protected global mutex. Not executed on Windows here.
use crate::{Boundary, Error, Namespace, RECORD_LEN, SecretBlob, hex};
use sha2::{Digest, Sha256};
use std::{
    collections::HashSet,
    fmt,
    marker::PhantomData,
    mem::{offset_of, size_of},
    ptr::null_mut,
    rc::Rc,
    sync::{Mutex, OnceLock},
};
use windows_sys::Win32::{
    Foundation::{
        CloseHandle, ERROR_NO_TOKEN, ERROR_NOT_FOUND, GetLastError, HANDLE, LocalFree,
        WAIT_ABANDONED, WAIT_OBJECT_0, WAIT_TIMEOUT,
    },
    Security::{Authorization::*, Credentials::*, *},
    System::Threading::{
        CreateMutexExW, GetCurrentProcess, GetCurrentThread, GetCurrentThreadId, OpenProcessToken,
        OpenThreadToken, ReleaseMutex, WaitForSingleObject,
    },
};
use zeroize::{Zeroize, Zeroizing};
// SYNCHRONIZE | READ_CONTROL | MUTEX_MODIFY_STATE, without WRITE_DAC/WRITE_OWNER.
const RIGHTS: u32 = 0x00120001;
static RESERVED: OnceLock<Mutex<HashSet<String>>> = OnceLock::new();
struct Reservation(String);
impl Reservation {
    fn new(name: String) -> Result<Self, Error> {
        let mut registry = RESERVED
            .get_or_init(|| Mutex::new(HashSet::new()))
            .lock()
            .map_err(|_| Error::OwnershipUncertain)?;
        if !registry.insert(name.clone()) {
            return Err(Error::Busy);
        }
        Ok(Self(name))
    }
}
impl Drop for Reservation {
    fn drop(&mut self) {
        if let Some(registry) = RESERVED.get()
            && let Ok(mut names) = registry.lock()
        {
            names.remove(&self.0);
        }
    }
}
struct Handle(HANDLE);
impl Drop for Handle {
    fn drop(&mut self) {
        unsafe {
            CloseHandle(self.0);
        }
    }
}
struct LocalAllocation(*mut std::ffi::c_void);
impl Drop for LocalAllocation {
    fn drop(&mut self) {
        unsafe {
            LocalFree(self.0);
        }
    }
}
struct CredentialAllocation(*mut CREDENTIALW);
impl Drop for CredentialAllocation {
    fn drop(&mut self) {
        unsafe {
            if !self.0.is_null() {
                let c = &*self.0;
                if !c.CredentialBlob.is_null()
                    && c.CredentialBlobSize <= CRED_MAX_CREDENTIAL_BLOB_SIZE
                {
                    std::slice::from_raw_parts_mut(c.CredentialBlob, c.CredentialBlobSize as usize)
                        .zeroize();
                }
                CredFree(self.0.cast());
            }
        }
    }
}
fn wide(text: &str) -> Vec<u16> {
    text.encode_utf16().chain(Some(0)).collect()
}
fn reject_impersonation() -> Result<(), Error> {
    unsafe {
        let mut token = null_mut();
        if OpenThreadToken(GetCurrentThread(), TOKEN_QUERY, 1, &mut token) != 0 {
            let _token = Handle(token);
            return Err(Error::OwnershipUncertain);
        }
        if GetLastError() != ERROR_NO_TOKEN {
            return Err(Error::OwnershipUncertain);
        }
        Ok(())
    }
}
struct UserSid {
    words: Zeroizing<Vec<u32>>,
    len: usize,
    text: String,
}
impl UserSid {
    fn ptr(&self) -> PSID {
        self.words.as_ptr().cast_mut().cast()
    }
}
fn current_user() -> Result<UserSid, Error> {
    reject_impersonation()?;
    unsafe {
        let mut raw_token = null_mut();
        if OpenProcessToken(GetCurrentProcess(), TOKEN_QUERY, &mut raw_token) == 0 {
            return Err(Error::OwnershipUncertain);
        }
        let token = Handle(raw_token);
        let mut needed = 0;
        GetTokenInformation(token.0, TokenUser, null_mut(), 0, &mut needed);
        if needed < size_of::<TOKEN_USER>() as u32 || needed > 4096 {
            return Err(Error::OwnershipUncertain);
        }
        // Native token structure requires pointer alignment; byte Vec alone does not promise it.
        let mut buffer =
            Zeroizing::new(vec![0usize; (needed as usize).div_ceil(size_of::<usize>())]);
        if GetTokenInformation(
            token.0,
            TokenUser,
            buffer.as_mut_ptr().cast(),
            needed,
            &mut needed,
        ) == 0
        {
            return Err(Error::OwnershipUncertain);
        }
        let user = &*buffer.as_ptr().cast::<TOKEN_USER>();
        if IsValidSid(user.User.Sid) == 0 {
            return Err(Error::OwnershipUncertain);
        }
        let len = GetLengthSid(user.User.Sid) as usize;
        if !(8..=68).contains(&len) {
            return Err(Error::OwnershipUncertain);
        }
        let mut words = Zeroizing::new(vec![0u32; len.div_ceil(4)]);
        if CopySid(len as u32, words.as_mut_ptr().cast(), user.User.Sid) == 0 {
            return Err(Error::OwnershipUncertain);
        }
        let mut raw_string = null_mut();
        if ConvertSidToStringSidW(words.as_mut_ptr().cast(), &mut raw_string) == 0
            || raw_string.is_null()
        {
            return Err(Error::OwnershipUncertain);
        }
        let allocation = LocalAllocation(raw_string.cast());
        let mut count = 0;
        while count < 256 && *raw_string.add(count) != 0 {
            count += 1
        }
        if count == 256 {
            return Err(Error::OwnershipUncertain);
        }
        let text = String::from_utf16(std::slice::from_raw_parts(raw_string, count))
            .map_err(|_| Error::OwnershipUncertain)?;
        drop(allocation);
        if !text.starts_with("S-1-")
            || !text
                .bytes()
                .all(|b| b.is_ascii_digit() || b == b'-' || b == b'S')
        {
            return Err(Error::OwnershipUncertain);
        }
        Ok(UserSid { words, len, text })
    }
}
/// Verifies newly created AND pre-existing kernel object. Creation attributes are ignored for existing objects.
fn validate_security(handle: HANDLE, user: &UserSid) -> Result<(), Error> {
    unsafe {
        let mut owner = null_mut();
        let mut dacl = null_mut();
        let mut descriptor = null_mut();
        if GetSecurityInfo(
            handle,
            SE_KERNEL_OBJECT,
            OWNER_SECURITY_INFORMATION | DACL_SECURITY_INFORMATION,
            &mut owner,
            null_mut(),
            &mut dacl,
            null_mut(),
            &mut descriptor,
        ) != 0
            || descriptor.is_null()
        {
            return Err(Error::OwnershipUncertain);
        }
        let _allocation = LocalAllocation(descriptor);
        let mut control = 0;
        let mut revision = 0;
        if owner.is_null()
            || IsValidSid(owner) == 0
            || EqualSid(owner, user.ptr()) == 0
            || dacl.is_null()
            || IsValidAcl(dacl) == 0
            || GetSecurityDescriptorControl(descriptor, &mut control, &mut revision) == 0
            || control & SE_DACL_PROTECTED == 0
            || (*dacl).AceCount != 1
        {
            return Err(Error::OwnershipUncertain);
        }
        let mut raw_ace = null_mut();
        if GetAce(dacl, 0, &mut raw_ace) == 0 || raw_ace.is_null() {
            return Err(Error::OwnershipUncertain);
        }
        let header = &*raw_ace.cast::<ACE_HEADER>();
        let sid_offset = offset_of!(ACCESS_ALLOWED_ACE, SidStart);
        if header.AceType != 0 || header.AceFlags != 0 || (header.AceSize as usize) < sid_offset + 8
        {
            return Err(Error::OwnershipUncertain);
        }
        let ace = &*raw_ace.cast::<ACCESS_ALLOWED_ACE>();
        if ace.Mask != RIGHTS {
            return Err(Error::OwnershipUncertain);
        }
        let sid_bytes = raw_ace.cast::<u8>().add(sid_offset);
        let sub_authorities = *sid_bytes.add(1) as usize;
        if sub_authorities > 15 || sid_offset + 8 + sub_authorities * 4 != header.AceSize as usize {
            return Err(Error::OwnershipUncertain);
        }
        let ace_sid = sid_bytes.cast();
        if IsValidSid(ace_sid) == 0
            || GetLengthSid(ace_sid) as usize + sid_offset != ace.Header.AceSize as usize
            || EqualSid(ace_sid, user.ptr()) == 0
        {
            return Err(Error::OwnershipUncertain);
        }
        Ok(())
    }
}
/// Native guard is thread-bound; Win32 mutex ownership belongs to the acquiring thread.
pub struct OwnedCredential {
    mutex: Handle,
    target: Vec<u16>,
    abandoned: bool,
    thread: u32,
    user: UserSid,
    _reservation: Reservation,
    _thread_bound: PhantomData<Rc<()>>,
}
impl fmt::Debug for OwnedCredential {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("OwnedCredential(REDACTED)")
    }
}
impl Drop for OwnedCredential {
    fn drop(&mut self) {
        unsafe {
            if GetCurrentThreadId() == self.thread {
                ReleaseMutex(self.mutex.0);
            }
        }
    }
}
impl OwnedCredential {
    fn validate(&self) -> Result<(), Error> {
        if unsafe { GetCurrentThreadId() } != self.thread {
            return Err(Error::OwnershipUncertain);
        }
        reject_impersonation()?;
        validate_security(self.mutex.0, &self.user)
    }
}
#[derive(Default)]
pub struct WindowsCredentialManager;
impl fmt::Debug for WindowsCredentialManager {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("WindowsCredentialManager(REDACTED)")
    }
}
impl Boundary for WindowsCredentialManager {
    type Guard = OwnedCredential;
    fn acquire(&self, namespace: &Namespace) -> Result<Self::Guard, Error> {
        let user = current_user()?;
        let user_bytes =
            unsafe { std::slice::from_raw_parts(user.words.as_ptr().cast::<u8>(), user.len) };
        let user_hash: [u8; 32] = Sha256::digest(user_bytes).into();
        // Global user-scoped name covers same-user concurrent Windows logon sessions.
        let name = format!(
            "Global\\MnemaTalk.Dev.Vault.v1.{}.{}",
            hex(&user_hash),
            hex(&namespace.digest)
        );
        if name.encode_utf16().count() >= 260 {
            return Err(Error::InvalidInput);
        }
        let reservation = Reservation::new(name.clone())?;
        let sddl = wide(&format!(
            "O:{}D:P(A;;0x00120001;;;{})",
            user.text, user.text
        ));
        let mut descriptor = null_mut();
        unsafe {
            if ConvertStringSecurityDescriptorToSecurityDescriptorW(
                sddl.as_ptr(),
                SDDL_REVISION_1,
                &mut descriptor,
                null_mut(),
            ) == 0
                || descriptor.is_null()
            {
                return Err(Error::OwnershipUncertain);
            }
            let allocation = LocalAllocation(descriptor);
            let attributes = SECURITY_ATTRIBUTES {
                nLength: size_of::<SECURITY_ATTRIBUTES>() as u32,
                lpSecurityDescriptor: descriptor,
                bInheritHandle: 0,
            };
            let raw = CreateMutexExW(&attributes, wide(&name).as_ptr(), 0, RIGHTS);
            drop(allocation);
            if raw.is_null() {
                return Err(Error::OwnershipUncertain);
            }
            let mutex = Handle(raw);
            validate_security(mutex.0, &user)?;
            let abandoned = match WaitForSingleObject(mutex.0, 0) {
                WAIT_OBJECT_0 => false,
                WAIT_ABANDONED => true,
                WAIT_TIMEOUT => return Err(Error::Busy),
                _ => return Err(Error::OwnershipUncertain),
            };
            Ok(OwnedCredential {
                mutex,
                target: wide(&namespace.target()),
                abandoned,
                thread: GetCurrentThreadId(),
                user,
                _reservation: reservation,
                _thread_bound: PhantomData,
            })
        }
    }
    fn abandoned(&self, guard: &Self::Guard) -> bool {
        guard.abandoned
    }
    fn read(&self, guard: &mut Self::Guard) -> Result<Option<SecretBlob>, Error> {
        guard.validate()?;
        unsafe {
            let mut raw = null_mut();
            if CredReadW(guard.target.as_ptr(), CRED_TYPE_GENERIC, 0, &mut raw) == 0 {
                return if GetLastError() == ERROR_NOT_FOUND {
                    Ok(None)
                } else {
                    Err(Error::StoreUnavailable)
                };
            }
            if raw.is_null() {
                return Err(Error::Corrupt);
            }
            let _allocation = CredentialAllocation(raw);
            let c = &*raw;
            if c.Type != CRED_TYPE_GENERIC
                || c.Flags != 0
                || c.Persist != CRED_PERSIST_LOCAL_MACHINE
                || c.AttributeCount != 0
                || c.CredentialBlobSize as usize != RECORD_LEN
                || c.CredentialBlob.is_null()
                || c.TargetName.is_null()
            {
                return Err(Error::Corrupt);
            }
            // Bound comparison reads only the known expected target length, including NUL.
            for (i, expected) in guard.target.iter().enumerate() {
                let actual = *c.TargetName.add(i);
                if actual != *expected {
                    return Err(Error::Corrupt);
                }
                if actual == 0 {
                    break;
                }
            }
            let bytes =
                SecretBlob::new(std::slice::from_raw_parts(c.CredentialBlob, RECORD_LEN).to_vec());
            guard.validate()?;
            Ok(Some(bytes))
        }
    }
    fn write(&self, guard: &mut Self::Guard, bytes: &[u8]) -> Result<(), Error> {
        guard.validate()?;
        if bytes.len() != RECORD_LEN {
            return Err(Error::InvalidInput);
        }
        // Zeroizing owns exactly one heap copy for the OS call; no String conversion/logging.
        let mut blob = Zeroizing::new(bytes.to_vec());
        let credential = CREDENTIALW {
            Flags: 0,
            Type: CRED_TYPE_GENERIC,
            TargetName: guard.target.as_mut_ptr(),
            Comment: null_mut(),
            LastWritten: Default::default(),
            CredentialBlobSize: RECORD_LEN as u32,
            CredentialBlob: blob.as_mut_ptr(),
            Persist: CRED_PERSIST_LOCAL_MACHINE,
            AttributeCount: 0,
            Attributes: null_mut(),
            TargetAlias: null_mut(),
            UserName: null_mut(),
        };
        // CredWrite replaces current bytes; no CAS/precondition argument exists.
        if unsafe { CredWriteW(&credential, 0) } == 0 {
            return Err(Error::WriteUncertain);
        }
        // An OS success is not a disk-flush guarantee. Read-back verifies current visibility only.
        let observed = self.read(guard).map_err(|_| Error::WriteUncertain)?;
        if observed.as_ref().map(|b| b.as_slice()) != Some(bytes) {
            return Err(Error::WriteUncertain);
        }
        Ok(())
    }
}
