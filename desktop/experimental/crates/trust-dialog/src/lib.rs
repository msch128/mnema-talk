//! Native OS confirmation boundary. No renderer approval input, decision parser
//! or public decision constructor. This confirms a native user's gesture only;
//! actual Core, fresh server role and current membership remain caller duties.
#[cfg(any(test, feature = "os-modal"))]
use base64::{Engine, engine::general_purpose::STANDARD};
use mnema_private_native_client_broker::NativeAuthenticatedScope;
use std::{
    fmt,
    future::Future,
    pin::Pin,
    sync::{
        Arc,
        atomic::{AtomicBool, Ordering},
    },
    time::Instant,
};
use uuid::Uuid;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum DialogError {
    Denied,
    Cancelled,
    Expired,
    Unavailable,
    Stale,
}
impl fmt::Display for DialogError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("native trust confirmation unavailable")
    }
}
impl std::error::Error for DialogError {}

pub struct NativeTrustDisplayFacts {
    channel: Uuid,
    device: Uuid,
    group: Vec<u8>,
    root: [u8; 32],
    fingerprint: [u8; 32],
    device_key: [u8; 32],
}
impl fmt::Debug for NativeTrustDisplayFacts {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("NativeTrustDisplayFacts(REDACTED)")
    }
}
impl NativeTrustDisplayFacts {
    /// Native Core-derived public facts only. These bytes are display input,
    /// not proof of possession, roster admission or a server-provided root pin.
    pub fn from_native_core(
        channel: Uuid,
        device: Uuid,
        group: &[u8],
        root: [u8; 32],
        fingerprint: [u8; 32],
        device_key: [u8; 32],
    ) -> Result<Self, DialogError> {
        if channel.is_nil()
            || device.is_nil()
            || group.is_empty()
            || group.len() > 128
            || root == [0; 32]
            || device_key == [0; 32]
        {
            return Err(DialogError::Denied);
        }
        Ok(Self {
            channel,
            device,
            group: group.to_vec(),
            root,
            fingerprint,
            device_key,
        })
    }
    pub fn channel_id(&self) -> Uuid {
        self.channel
    }
    pub fn device_id(&self) -> Uuid {
        self.device
    }
    pub fn group_id(&self) -> &[u8] {
        &self.group
    }
    pub fn root_public_key(&self) -> [u8; 32] {
        self.root
    }
    pub fn root_fingerprint(&self) -> [u8; 32] {
        self.fingerprint
    }
    pub fn device_public_key(&self) -> [u8; 32] {
        self.device_key
    }
}
enum ConfirmationPurpose {
    FirstRoot,
    DeviceRemoval { account: Uuid, identity: Vec<u8> },
}
struct Request {
    purpose: ConfirmationPurpose,
    operation: Uuid,
    scope: Arc<NativeAuthenticatedScope>,
    facts: NativeTrustDisplayFacts,
    deadline: Instant,
}
/// Clone preserves the exact immutable native operation allocation; renderer
/// JSON cannot create this type, and fields have no setters or Serde conversion.
#[derive(Clone)]
pub struct NativeDialogRequest(Arc<Request>);
impl fmt::Debug for NativeDialogRequest {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("NativeDialogRequest(REDACTED)")
    }
}
impl NativeDialogRequest {
    pub fn first_root(
        operation: Uuid,
        scope: Arc<NativeAuthenticatedScope>,
        facts: NativeTrustDisplayFacts,
        deadline: Instant,
    ) -> Result<Self, DialogError> {
        if operation.is_nil()
            || scope.role() != "admin"
            || scope.native_context().is_none()
            || deadline <= Instant::now()
            || deadline > scope.monotonic_access_deadline()
        {
            return Err(DialogError::Denied);
        }
        Ok(Self(Arc::new(Request {
            purpose: ConfirmationPurpose::FirstRoot,
            operation,
            scope,
            facts,
            deadline,
        })))
    }
    /// Exact native Core-derived device selection. This does not authorize
    /// removal: the caller must recheck current MLS, native auth and the issuer.
    pub fn device_removal(
        operation: Uuid,
        scope: Arc<NativeAuthenticatedScope>,
        facts: NativeTrustDisplayFacts,
        deadline: Instant,
        account: Uuid,
        identity: &[u8],
    ) -> Result<Self, DialogError> {
        if account.is_nil() || identity.is_empty() || identity.len() > 256 {
            return Err(DialogError::Denied);
        }
        let base = Self::first_root(operation, scope, facts, deadline)?;
        let mut request = Arc::try_unwrap(base.0).map_err(|_| DialogError::Denied)?;
        request.purpose = ConfirmationPurpose::DeviceRemoval {
            account,
            identity: identity.into(),
        };
        Ok(Self(Arc::new(request)))
    }
    pub fn target_account(&self) -> Option<Uuid> {
        match &self.0.purpose {
            ConfirmationPurpose::FirstRoot => None,
            ConfirmationPurpose::DeviceRemoval { account, .. } => Some(*account),
        }
    }
    pub fn target_identity(&self) -> Option<&[u8]> {
        match &self.0.purpose {
            ConfirmationPurpose::FirstRoot => None,
            ConfirmationPurpose::DeviceRemoval { identity, .. } => Some(identity),
        }
    }
    pub fn is_device_removal(&self) -> bool {
        matches!(self.0.purpose, ConfirmationPurpose::DeviceRemoval { .. })
    }
    pub fn confirmation_title(&self) -> &'static str {
        if self.is_device_removal() {
            "Remove a Mnema device"
        } else {
            "Confirm a new Mnema root"
        }
    }
    pub fn confirmation_action(&self) -> &'static str {
        if self.is_device_removal() {
            "Remove device"
        } else {
            "Create root"
        }
    }
    pub fn operation_id(&self) -> Uuid {
        self.0.operation
    }
    pub fn authenticated_scope(&self) -> &NativeAuthenticatedScope {
        &self.0.scope
    }
    pub fn facts(&self) -> &NativeTrustDisplayFacts {
        &self.0.facts
    }
    pub fn deadline(&self) -> Instant {
        self.0.deadline
    }
    pub fn check_deadline(&self) -> Result<(), DialogError> {
        if Instant::now() >= self.deadline() {
            Err(DialogError::Expired)
        } else {
            Ok(())
        }
    }
    #[cfg(any(test, feature = "os-modal"))]
    fn text(&self) -> Result<String, DialogError> {
        self.check_deadline()?;
        let scope = self.authenticated_scope();
        let origin = url::Url::parse(scope.origin())
            .map_err(|_| DialogError::Denied)?
            .origin()
            .ascii_serialization();
        fn hex(bytes: &[u8]) -> String {
            bytes.iter().map(|b| format!("{b:02x}")).collect()
        }
        let community: String = scope
            .community_id()
            .chars()
            .flat_map(char::escape_default)
            .collect();
        // Origin uses canonical IDNA ASCII. Non-ASCII community/control text is
        // visibly escaped, so remote labels cannot hide identity with bidi text.
        if let ConfirmationPurpose::DeviceRemoval { account, identity } = &self.0.purpose {
            return Ok(format!(
                "Remove this Mnema device from future protected communication?\n\nOrigin: {origin}\nCommunity: {community}\nAdministrator account: {}\nChannel: {}\nTarget account: {account}\nTarget device: {}\nDevice identity (base64): {}\nGroup: {}\n\nFull root fingerprint:\n{}\nRoot public key:\n{}\nFull target device public key:\n{}\n\nPreviously received content cannot be withdrawn. This confirms only this exact device removal; it does not create a root or admit another device.",
                scope.account_id(),
                self.facts().channel,
                self.facts().device,
                STANDARD.encode(identity),
                STANDARD.encode(&self.facts().group),
                hex(&self.facts().fingerprint),
                hex(&self.facts().root),
                hex(&self.facts().device_key)
            ));
        }
        Ok(format!(
            "Create a new Mnema community root?\n\nOrigin: {origin}\nCommunity: {community}\nAccount: {}\nChannel: {}\nDevice: {}\nGroup: {}\n\nFull root fingerprint:\n{}\nRoot public key:\n{}\nDevice public key:\n{}\n\nKeep the full root fingerprint for an independent comparison with other devices. This first-root confirmation does not approve another device or an existing remote root.",
            scope.account_id(),
            self.facts().channel,
            self.facts().device,
            STANDARD.encode(&self.facts().group),
            hex(&self.facts().fingerprint),
            hex(&self.facts().root),
            hex(&self.facts().device_key)
        ))
    }
}
/// Created only by this crate's actual native OS dialog result callback. No
/// Clone, Serde or public constructor. Consuming it proves a native gesture for
/// exactly one immutable request; caller MUST recheck current auth/Core/registry.
/// ```compile_fail
/// use mnema_private_native_trust_dialog::NativeDialogDecision;
/// let _=serde_json::from_str::<NativeDialogDecision>("{\"approved\":true}");
/// ```
/// ```compile_fail
/// use mnema_private_native_trust_dialog::NativeDialogDecision;
/// fn no_export(decision:&NativeDialogDecision){let _=serde_json::to_vec(decision);}
/// ```
pub struct NativeDialogDecision {
    request: NativeDialogRequest,
    approved: bool,
    cancelled: Arc<AtomicBool>,
}
impl fmt::Debug for NativeDialogDecision {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("NativeDialogDecision(REDACTED)")
    }
}
impl NativeDialogDecision {
    pub fn matches_request(&self, request: &NativeDialogRequest) -> bool {
        Arc::ptr_eq(&self.request.0, &request.0)
    }
    pub fn consume(self, request: &NativeDialogRequest) -> Result<(), DialogError> {
        if !self.matches_request(request) {
            return Err(DialogError::Denied);
        }
        request.check_deadline()?;
        if self.cancelled.load(Ordering::Acquire) {
            return Err(DialogError::Cancelled);
        }
        if self.approved {
            Ok(())
        } else {
            Err(DialogError::Cancelled)
        }
    }
}
pub type NativeDialogFuture =
    Pin<Box<dyn Future<Output = Result<NativeDialogDecision, DialogError>> + Send + 'static>>;
pub trait NativeTrustDialog: Send + Sync {
    fn confirm_first_root(&self, request: NativeDialogRequest) -> NativeDialogFuture;
    fn confirm_device_removal(&self, _request: NativeDialogRequest) -> NativeDialogFuture {
        Box::pin(async { Err(DialogError::Unavailable) })
    }
    fn cancel_device_removal(&self, _operation: Uuid) -> Result<(), DialogError> {
        Err(DialogError::Unavailable)
    }
    fn cancel_first_root(&self, _operation: Uuid) -> Result<(), DialogError> {
        Err(DialogError::Unavailable)
    }
}
pub struct DeniedNativeTrustDialog;
impl NativeTrustDialog for DeniedNativeTrustDialog {
    fn confirm_first_root(&self, _: NativeDialogRequest) -> NativeDialogFuture {
        Box::pin(async { Err(DialogError::Unavailable) })
    }
}
#[cfg(feature = "os-modal")]
mod native_os;
#[cfg(feature = "os-modal")]
pub use native_os::{NativeRegistryValidator, TauriNativeTrustDialog};
#[cfg(all(test, feature = "research-fixture"))]
mod tests;
