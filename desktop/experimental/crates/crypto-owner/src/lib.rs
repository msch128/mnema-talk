//! Native-only current authenticated owner composition. Not renderer authority.
//! Default denies relay; `opaque-relay` requires the new canonical broker seam.
#[cfg(feature = "opaque-relay")]
mod chat_history;
mod pending_root;
#[cfg(feature = "opaque-relay")]
mod relay;
#[cfg(feature = "opaque-relay")]
mod typed_relay;
use mnema_crypto_adapter_candidate::Scope;
#[cfg(feature = "opaque-relay")]
pub use mnema_crypto_sdk_prototype::{ChatEventClaim, ChatKind, ChatOperation, ReactionAction};
use mnema_crypto_sdk_prototype::{NativeBinding, Sdk};
use mnema_private_native_client_broker::{
    NativeAuthenticatedScope, NativeClient, NativeWindowLease,
};
pub use pending_root::{
    NativeFirstRootOwner, NativeRootPreview, PendingFirstRoot, PendingRootCancellation,
};
#[cfg(feature = "opaque-relay")]
pub use relay::NativeChatDisplay;
use sha2::{Digest, Sha256};
use std::{
    collections::HashMap,
    sync::Arc,
    time::{SystemTime, UNIX_EPOCH},
};
#[cfg(feature = "opaque-relay")]
pub use typed_relay::{
    NativeChatEventPublication, NativeChatEventReceipt, NativeChatHistoryPublication,
    NativePreparedChatEvent, NativeTypedChatChange, NativeTypedChatMessage,
};
use uuid::Uuid;
use zeroize::Zeroizing;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Error {
    Invalid,
    Auth,
    Binding,
    Crypto,
    Retired,
    Limit,
    Conflict,
}
pub type Result<T> = std::result::Result<T, Error>;
fn now() -> Result<u64> {
    let n = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_err(|_| Error::Invalid)?
        .as_secs();
    if n > i64::MAX as u64 {
        return Err(Error::Invalid);
    }
    Ok(n)
}
fn canonical_scope(auth: &NativeAuthenticatedScope) -> Result<Scope> {
    Scope::new(auth.origin(), auth.community_id()).map_err(|_| Error::Binding)
}
fn session(auth: &NativeAuthenticatedScope) -> String {
    format!("{}.{}", auth.family_id(), auth.client_instance_id())
}
/// Native caller uses this only after verified native root/device ceremony.
/// Selected channel is bounded routing; server authorization stays mandatory.
pub fn binding_for_current_native_scope(
    client: &NativeClient,
    lease: &NativeWindowLease,
    channel: Uuid,
    device: Uuid,
) -> Result<NativeBinding> {
    if channel.is_nil() || device.is_nil() {
        return Err(Error::Invalid);
    }
    let auth = client.authenticated_scope(lease).map_err(|_| Error::Auth)?;
    client
        .with_authenticated_publication(lease, &auth, || {
            NativeBinding::from_native_actor(
                &channel.to_string(),
                &auth.native_profile_identity().to_string(),
                &auth.native_window_identity().to_string(),
                &session(&auth),
                &auth.account_id().to_string(),
                &device.to_string(),
            )
            .map_err(|_| Error::Binding)
        })
        .map_err(|_| Error::Auth)?
}
struct StableOwner {
    origin: String,
    community: String,
    account: Uuid,
    family: Uuid,
    instance: Uuid,
    profile: Uuid,
    window: Uuid,
    context: Uuid,
    channel: Uuid,
    device: String,
    group: Vec<u8>,
}
struct PendingEvent {
    digest: [u8; 32],
    body: Zeroizing<String>,
    epoch: u64,
    generation: u64,
}
/// Mutable ownership serializes trust/epoch/outbox operations. No Clone/Serde.
/// Native hooks must retire before credential/window destruction.
pub struct NativeChatOwner {
    client: NativeClient,
    lease: NativeWindowLease,
    sdk: Sdk,
    owner: StableOwner,
    events: HashMap<Uuid, PendingEvent>,
    retired: bool,
    serial: Uuid,
    cursor: i64,
    #[cfg(feature = "opaque-relay")]
    typed_events: HashMap<Uuid, mnema_crypto_sdk_prototype::NativeReservedChatEvent>,
    #[cfg(feature = "opaque-relay")]
    typed_history: chat_history::NativeChatHistory,
    #[cfg(feature = "opaque-relay")]
    chat_protocol: Option<bool>, // false=legacy, true=typed; never fallback across lanes.
}
/// Native transport job, never renderer JSON or arbitrary ciphertext input.
pub struct NativePreparedChat {
    auth: Arc<NativeAuthenticatedScope>,
    event: Uuid,
    channel: Uuid,
    group: Vec<u8>,
    ciphertext: Zeroizing<Vec<u8>>,
    epoch: u64,
    generation: u64,
    owner: Uuid,
}
impl NativeChatOwner {
    /// Consumes a live SDK whose ownleaf/root-signed roster and binding must
    /// match opaque broker provenance. Provisioning/ceremony remains caller-owned.
    pub fn from_proven_sdk(
        client: NativeClient,
        lease: NativeWindowLease,
        mut sdk: Sdk,
    ) -> Result<Self> {
        let auth = match client.authenticated_scope(&lease) {
            Ok(a) => a,
            Err(_) => {
                sdk.retire_native();
                return Err(Error::Auth);
            }
        };
        let facts = match sdk.native_owner_facts(now()?) {
            Ok(f) => f,
            Err(_) => {
                sdk.retire_native();
                return Err(Error::Crypto);
            }
        };
        let scope = canonical_scope(&auth)?;
        let channel = Uuid::parse_str(facts.channel()).map_err(|_| Error::Binding)?;
        let context = auth.native_context().ok_or(Error::Binding)?;
        if channel.is_nil()
            || facts.origin() != scope.origin()
            || facts.community() != scope.community()
            || facts.account() != auth.account_id().to_string()
            || !sdk.matches_native_actor(
                &auth.native_profile_identity().to_string(),
                &auth.native_window_identity().to_string(),
                &session(&auth),
            )
        {
            sdk.retire_native();
            return Err(Error::Binding);
        }
        let owner = StableOwner {
            origin: scope.origin().into(),
            community: scope.community().into(),
            account: auth.account_id(),
            family: auth.family_id(),
            instance: auth.client_instance_id(),
            profile: auth.native_profile_identity(),
            window: auth.native_window_identity(),
            context,
            channel,
            device: facts.device().into(),
            group: facts.group().into(),
        };
        if client
            .with_authenticated_publication(&lease, &auth, || ())
            .is_err()
        {
            sdk.retire_native();
            return Err(Error::Auth);
        }
        #[cfg(feature = "opaque-relay")]
        let typed_history = chat_history::NativeChatHistory::for_current_native_owner(
            sdk.native_protected_event_scope(now()?)
                .map_err(|_| Error::Crypto)?,
        )?;
        Ok(Self {
            client,
            lease,
            sdk,
            owner,
            events: HashMap::new(),
            retired: false,
            serial: Uuid::new_v4(),
            cursor: 0,
            #[cfg(feature = "opaque-relay")]
            typed_events: HashMap::new(),
            #[cfg(feature = "opaque-relay")]
            typed_history,
            #[cfg(feature = "opaque-relay")]
            chat_protocol: None,
        })
    }
    pub fn retire(&mut self) {
        self.retired = true;
        self.sdk.retire_native();
        self.events.clear();
        #[cfg(feature = "opaque-relay")]
        {
            self.typed_events.clear();
            self.typed_history.retire();
        }
    }
    fn scope(&mut self) -> Result<NativeAuthenticatedScope> {
        if self.retired {
            return Err(Error::Retired);
        }
        let auth = self
            .client
            .authenticated_scope(&self.lease)
            .map_err(|_| Error::Auth)?;
        self.check_admitted_scope(&auth)?;
        Ok(auth)
    }
    /// Check the original admitted opaque native scope. This never samples a
    /// replacement scope or extends the original admission/deadline.
    fn check_admitted_scope(&mut self, auth: &NativeAuthenticatedScope) -> Result<()> {
        if self.retired {
            return Err(Error::Retired);
        }
        self.client
            .check_authenticated_scope(&self.lease, auth)
            .map_err(|_| Error::Auth)?;
        let scope = canonical_scope(auth)?;
        if scope.origin() != self.owner.origin
            || scope.community() != self.owner.community
            || auth.account_id() != self.owner.account
            || auth.family_id() != self.owner.family
            || auth.client_instance_id() != self.owner.instance
            || auth.native_profile_identity() != self.owner.profile
            || auth.native_window_identity() != self.owner.window
            || auth.native_context() != Some(self.owner.context)
        {
            self.retire();
            return Err(Error::Binding);
        }
        Ok(())
    }
    fn current(&mut self, when: u64) -> Result<(u64, u64)> {
        let facts = self
            .sdk
            .native_owner_facts(when)
            .map_err(|_| Error::Crypto)?;
        if facts.origin() != self.owner.origin
            || facts.community() != self.owner.community
            || facts.account() != self.owner.account.to_string()
            || facts.device() != self.owner.device
            || facts.channel() != self.owner.channel.to_string()
            || facts.group() != self.owner.group
        {
            return Err(Error::Binding);
        }
        Ok((facts.epoch(), facts.generation()))
    }
    /// Encrypt once for a fresh event; duplicate body uses only the same durable
    /// outbox. Different body is rejected and never re-encrypted.
    pub fn prepare_chat(&mut self, event: Uuid, body: &str) -> Result<NativePreparedChat> {
        let admitted = Arc::new(self.scope()?);
        self.prepare_chat_retained(admitted, event, body)
    }
    /// A host-queued operation must pass its ORIGINAL admitted scope. The same
    /// private Arc travels through durable preparation, HTTP and final enqueue.
    pub fn prepare_chat_retained(
        &mut self,
        auth: Arc<NativeAuthenticatedScope>,
        event: Uuid,
        body: &str,
    ) -> Result<NativePreparedChat> {
        self.check_admitted_scope(&auth)?;
        if event.is_nil() || body.is_empty() || body.len() > 24 * 1024 {
            return Err(Error::Invalid);
        }
        #[cfg(feature = "opaque-relay")]
        self.enter_chat_protocol(false)?;
        let when = now()?;
        let digest: [u8; 32] = Sha256::digest(body.as_bytes()).into();
        // Provider I/O stays outside broker locks. A concurrent auth retirement
        // may leave known-committed UNSENT ciphertext; it never releases a job.
        self.client
            .check_authenticated_scope(&self.lease, &auth)
            .map_err(|_| Error::Auth)?;
        let result = (|| {
            let (epoch, generation) = self.current(when)?;
            if let Some(previous) = self.events.get(&event) {
                if previous.digest != digest || previous.body.as_str() != body {
                    return Err(Error::Conflict);
                }
                if previous.epoch != epoch || previous.generation != generation {
                    return Err(Error::Binding);
                }
            } else {
                if self.events.len() >= 256 {
                    return Err(Error::Limit);
                }
                let bytes = self
                    .sdk
                    .send_chat(&event.to_string(), body, when)
                    .map_err(|_| Error::Crypto)?;
                let durable = self
                    .sdk
                    .pending_chat_for_native_publish(&event.to_string(), when)
                    .map_err(|_| Error::Crypto)?
                    .ok_or(Error::Crypto)?;
                if bytes != durable {
                    return Err(Error::Crypto);
                }
                self.events.insert(
                    event,
                    PendingEvent {
                        digest,
                        body: Zeroizing::new(body.into()),
                        epoch,
                        generation,
                    },
                );
            }
            let ciphertext = self
                .sdk
                .pending_chat_for_native_publish(&event.to_string(), when)
                .map_err(|_| Error::Crypto)?
                .ok_or(Error::Crypto)?;
            Ok((ciphertext, epoch, generation))
        })();
        let (ciphertext, epoch, generation) = match result {
            Ok(r) => r,
            Err(e) => {
                if matches!(e, Error::Crypto | Error::Binding) {
                    self.retire()
                }
                return Err(e);
            }
        };
        let current = self.current(now()?)?;
        if current != (epoch, generation) {
            self.retire();
            return Err(Error::Binding);
        }
        self.client
            .check_authenticated_scope(&self.lease, &auth)
            .map_err(|_| Error::Auth)?;
        Ok(NativePreparedChat {
            auth,
            event,
            channel: self.owner.channel,
            group: self.owner.group.clone(),
            ciphertext: Zeroizing::new(ciphertext),
            epoch,
            generation,
            owner: self.serial,
        })
    }
    /// Native validation without key/ciphertext projection to renderer. A job
    /// cannot cross owner instances even under the same account/group/epoch.
    pub fn validate_prepared(&mut self, job: &NativePreparedChat) -> Result<()> {
        if self.retired {
            return Err(Error::Retired);
        }
        if job.owner != self.serial
            || job.channel != self.owner.channel
            || job.group != self.owner.group
            || job.event.is_nil()
        {
            return Err(Error::Binding);
        }
        self.client
            .check_authenticated_scope(&self.lease, &job.auth)
            .map_err(|_| Error::Auth)?;
        if self.current(now()?)? != (job.epoch, job.generation) {
            return Err(Error::Binding);
        }
        let actual = self
            .sdk
            .pending_chat_for_native_publish(&job.event.to_string(), now()?)
            .map_err(|_| Error::Crypto)?
            .ok_or(Error::Crypto)?;
        if actual != job.ciphertext.as_slice() {
            return Err(Error::Crypto);
        }
        Ok(())
    }
    pub fn native_receive_cursor(&self) -> i64 {
        self.cursor
    }
}
