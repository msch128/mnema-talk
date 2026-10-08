//! Actual native first-root ceremony composition. No renderer approval value,
//! server-selected authority key or restored-owner activation constructor.
use super::*;
use base64::{Engine, engine::general_purpose::STANDARD};
use mnema_crypto_enrollment_prototype::{Issuer, NativeRootPin, NativeSecrets};
use mnema_private_first_community_bootstrap::{FreshCommunity, NativeAdminSubject};
use mnema_private_native_client_broker::{MetadataResource, NativeRequest};
use mnema_private_native_trust_dialog::{
    NativeDialogRequest, NativeTrustDialog, NativeTrustDisplayFacts,
};
use serde::Serialize;
use std::{
    path::Path,
    sync::{
        Arc,
        atomic::{AtomicU8, Ordering},
    },
    time::{Duration, Instant},
};

const PENDING: u8 = 0;
const ADOPTED: u8 = 1;
const CANCELLED: u8 = 2;
const FAILED: u8 = 3;
/// One native winner; this is a lifecycle fence, never cryptographic approval.
struct AdoptionGate(AtomicU8);
impl AdoptionGate {
    fn new() -> Self {
        Self(AtomicU8::new(PENDING))
    }
    fn is_pending(&self) -> bool {
        self.0.load(Ordering::Acquire) == PENDING
    }
    fn cancel(&self) -> Result<()> {
        self.0
            .compare_exchange(PENDING, CANCELLED, Ordering::AcqRel, Ordering::Acquire)
            .map(|_| ())
            .map_err(|_| Error::Retired)
    }
    fn adopt(&self, deadline: Instant) -> Result<()> {
        if Instant::now() >= deadline {
            self.fail();
            return Err(Error::Auth);
        }
        self.0
            .compare_exchange(PENDING, ADOPTED, Ordering::AcqRel, Ordering::Acquire)
            .map_err(|_| Error::Auth)?;
        // Construction may finish after ceremony timeout; no owner escapes.
        if Instant::now() >= deadline {
            self.0.store(FAILED, Ordering::Release);
            return Err(Error::Auth);
        }
        Ok(())
    }
    fn fail(&self) {
        let _ = self
            .0
            .compare_exchange(PENDING, FAILED, Ordering::AcqRel, Ordering::Acquire);
    }
}
struct FailureGuard(Arc<AdoptionGate>);
impl Drop for FailureGuard {
    fn drop(&mut self) {
        self.0.fail();
    }
}

#[derive(Serialize)]
pub struct NativeRootPreview {
    pub operation_id: String,
    pub origin: String,
    pub community_id: String,
    pub account_id: String,
    pub channel_id: String,
    pub device_id: String,
    pub group_id: String,
    pub root_public_key: String,
    pub root_fingerprint: String,
    pub device_public_key: String,
}
/// Native cancellation handle; Tauri must match actual window and opaque native
/// operation before invoking it. It cannot authorize confirmation.
pub struct PendingRootCancellation {
    operation: Uuid,
    outcome: Arc<AdoptionGate>,
}
impl PendingRootCancellation {
    pub fn operation_id(&self) -> Uuid {
        self.operation
    }
    /// Success means cancellation won before adoption. A completed/failed
    /// operation returns Retired; it never reports that a live owner was cancelled.
    pub fn cancel(&self) -> Result<()> {
        self.outcome.cancel()
    }
}
pub struct PendingFirstRoot<'a, S: NativeSecrets> {
    client: NativeClient,
    lease: NativeWindowLease,
    fresh: FreshCommunity<'a, S>,
    request: NativeDialogRequest,
    auth: Arc<NativeAuthenticatedScope>,
    channel: Uuid,
    device: Uuid,
    outcome: Arc<AdoptionGate>,
}
/// Local native actor owns issuer and live chat SDK together. Issuance must be
/// serialized with Core/source/publication jobs; no separate authority mutator.
pub struct NativeFirstRootOwner<'a, S: NativeSecrets> {
    pub(crate) issuer: Issuer<'a, S>,
    pub(crate) chat: NativeChatOwner,
    adoption: NativeDialogRequest,
}
fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}
fn channel_visible(body: &serde_json::Value, channel: Uuid) -> bool {
    let matches =
        |row: &serde_json::Value| row["id"] == channel.to_string() && row["type"] == "text";
    body["uncategorized"]
        .as_array()
        .is_some_and(|a| a.iter().any(matches))
        || body["categories"].as_array().is_some_and(|a| {
            a.iter().any(|category| {
                category["channels"]
                    .as_array()
                    .is_some_and(|rows| rows.iter().any(matches))
            })
        })
}
/// A fresh current DB-backed native Me and visible hierarchy are workflow
/// evidence. They do not create root authority, device approval or MLS trust.
async fn fresh_root_metadata(
    client: &NativeClient,
    lease: &NativeWindowLease,
    channel: Uuid,
    auth: &Arc<NativeAuthenticatedScope>,
) -> Result<()> {
    if channel.is_nil() {
        return Err(Error::Invalid);
    }
    client
        .check_authenticated_scope(lease, auth)
        .map_err(|_| Error::Auth)?;
    let response = client
        .request_retained_authenticated(lease, auth.clone(), Uuid::new_v4(), NativeRequest::Me)
        .await
        .map_err(|_| Error::Auth)?;
    let me = client.commit(lease, response).map_err(|_| Error::Auth)?;
    client
        .check_authenticated_scope(lease, auth)
        .map_err(|_| Error::Auth)?;
    if me.status != 200 || auth.role() != "admin" {
        return Err(Error::Auth);
    }
    let response = client
        .request_retained_authenticated(
            lease,
            auth.clone(),
            Uuid::new_v4(),
            NativeRequest::Metadata {
                resource: MetadataResource::Channels,
            },
        )
        .await
        .map_err(|_| Error::Auth)?;
    let hierarchy = client.commit(lease, response).map_err(|_| Error::Auth)?;
    client
        .check_authenticated_scope(lease, auth)
        .map_err(|_| Error::Auth)?;
    if hierarchy.status != 200 || !channel_visible(&hierarchy.body, channel) {
        return Err(Error::Auth);
    }
    Ok(())
}
impl<'a, S: NativeSecrets> PendingFirstRoot<'a, S> {
    /// The path/custody are native-owned fresh namespace, never renderer input.
    /// The selected UUID is checked against actual visible text metadata. Device
    /// and root/group identity are locally generated, not provided by server/UI.
    pub async fn begin(
        client: NativeClient,
        lease: NativeWindowLease,
        path: &Path,
        secrets: &'a S,
        channel: Uuid,
    ) -> Result<(Self, PendingRootCancellation)> {
        let auth = Arc::new(
            client
                .authenticated_scope(&lease)
                .map_err(|_| Error::Auth)?,
        );
        fresh_root_metadata(&client, &lease, channel, &auth).await?;
        Self::provision_for_admitted_scope(client, lease, path, secrets, channel, auth)
    }
    /// Retain the host's original admitted identity before generating any
    /// device/root/database state or constructing a native modal request.
    pub async fn begin_retained(
        client: NativeClient,
        lease: NativeWindowLease,
        path: &Path,
        secrets: &'a S,
        channel: Uuid,
        auth: Arc<NativeAuthenticatedScope>,
    ) -> Result<(Self, PendingRootCancellation)> {
        fresh_root_metadata(&client, &lease, channel, &auth).await?;
        Self::provision_for_admitted_scope(client, lease, path, secrets, channel, auth)
    }
    fn provision_for_admitted_scope(
        client: NativeClient,
        lease: NativeWindowLease,
        path: &Path,
        secrets: &'a S,
        channel: Uuid,
        auth: Arc<NativeAuthenticatedScope>,
    ) -> Result<(Self, PendingRootCancellation)> {
        client
            .check_authenticated_scope(&lease, &auth)
            .map_err(|_| Error::Auth)?;
        let scope = canonical_scope(&auth)?;
        let device = Uuid::new_v4();
        let identity = format!("{}.{}", auth.account_id(), device);
        let subject = NativeAdminSubject::from_native_admin_workflow(
            &auth.account_id().to_string(),
            &device.to_string(),
            identity.as_bytes(),
        )
        .map_err(|_| Error::Binding)?;
        client
            .check_authenticated_scope(&lease, &auth)
            .map_err(|_| Error::Auth)?;
        let fresh = FreshCommunity::provision_first_group(path, secrets, scope, subject, now()?)
            .map_err(|_| Error::Crypto)?;
        let public = fresh.public_for_native_oob_ceremony();
        if public.account != auth.account_id().to_string() || public.device != device.to_string() {
            return Err(Error::Binding);
        }
        let facts = NativeTrustDisplayFacts::from_native_core(
            channel,
            device,
            &public.group,
            public.authority,
            public.fingerprint,
            public.device_key,
        )
        .map_err(|_| Error::Binding)?;
        let deadline = Instant::now()
            .checked_add(Duration::from_secs(120))
            .ok_or(Error::Invalid)?
            .min(auth.monotonic_access_deadline());
        client
            .check_authenticated_scope(&lease, &auth)
            .map_err(|_| Error::Auth)?;
        let operation = Uuid::new_v4();
        let outcome = Arc::new(AdoptionGate::new());
        let request = NativeDialogRequest::first_root(operation, auth.clone(), facts, deadline)
            .map_err(|_| Error::Auth)?;
        Ok((
            Self {
                client,
                lease,
                fresh,
                request,
                auth,
                channel,
                device,
                outcome: outcome.clone(),
            },
            PendingRootCancellation { operation, outcome },
        ))
    }
    pub fn native_deadline(&self) -> Instant {
        self.request.deadline()
    }
    pub fn authenticated_scope(&self) -> &NativeAuthenticatedScope {
        self.request.authenticated_scope()
    }
    pub fn public_preview(&self) -> NativeRootPreview {
        let p = self.fresh.public_for_native_oob_ceremony();
        NativeRootPreview {
            operation_id: self.request.operation_id().to_string(),
            origin: p.origin.clone(),
            community_id: p.community.clone(),
            account_id: p.account.clone(),
            channel_id: self.channel.to_string(),
            device_id: p.device.clone(),
            group_id: STANDARD.encode(p.group),
            root_public_key: hex(&p.authority),
            root_fingerprint: hex(&p.fingerprint),
            device_public_key: hex(&p.device_key),
        }
    }
    /// One-time consumption requires the opaque decision created by actual OS
    /// callback. Public preview/operation echo has no approval authority.
    pub async fn confirm(
        self,
        dialog: &dyn NativeTrustDialog,
    ) -> Result<NativeFirstRootOwner<'a, S>> {
        let Self {
            client,
            lease,
            fresh,
            request,
            auth: admitted,
            channel,
            device,
            outcome,
        } = self;
        let _failure = FailureGuard(outcome.clone());
        if !outcome.is_pending() {
            return Err(Error::Auth);
        }
        request.check_deadline().map_err(|_| Error::Auth)?;
        client
            .check_authenticated_scope(&lease, request.authenticated_scope())
            .map_err(|_| Error::Auth)?;
        let decision = dialog
            .confirm_first_root(request.clone())
            .await
            .map_err(|_| Error::Auth)?;
        if !outcome.is_pending() {
            return Err(Error::Auth);
        }
        decision.consume(&request).map_err(|_| Error::Auth)?;
        request.check_deadline().map_err(|_| Error::Auth)?;
        fresh_root_metadata(&client, &lease, channel, &admitted).await?;
        if !outcome.is_pending() {
            return Err(Error::Auth);
        }
        let auth = request.authenticated_scope();
        let binding = NativeBinding::from_native_actor(
            &channel.to_string(),
            &auth.native_profile_identity().to_string(),
            &auth.native_window_identity().to_string(),
            &session(auth),
            &auth.account_id().to_string(),
            &device.to_string(),
        )
        .map_err(|_| Error::Binding)?;
        client
            .check_authenticated_scope(&lease, auth)
            .map_err(|_| Error::Auth)?;
        let (issuer, transfer) = fresh.into_native_host();
        // Storage I/O outside broker locks. Losing current scope during durable
        // adoption retires keys before any Core/renderer/transport output.
        let mut sdk =
            Sdk::from_fresh_native_host(transfer, binding, now()?).map_err(|_| Error::Crypto)?;
        if !outcome.is_pending()
            || request.check_deadline().is_err()
            || client.check_authenticated_scope(&lease, auth).is_err()
        {
            sdk.retire_native();
            return Err(Error::Auth);
        }
        let mut chat = NativeChatOwner::from_proven_sdk(client.clone(), lease.clone(), sdk)?;
        if request.check_deadline().is_err()
            || client.check_authenticated_scope(&lease, auth).is_err()
            || outcome.adopt(request.deadline()).is_err()
        {
            chat.retire();
            return Err(Error::Auth);
        }
        Ok(NativeFirstRootOwner {
            issuer,
            chat,
            adoption: request,
        })
    }
}
impl<S: NativeSecrets> NativeFirstRootOwner<'_, S> {
    /// Initial completion fence only; later chat jobs capture a fresh native scope.
    pub fn adoption_scope(&self) -> &NativeAuthenticatedScope {
        self.adoption.authenticated_scope()
    }
    pub fn adoption_deadline(&self) -> Instant {
        self.adoption.deadline()
    }
    pub fn retire(&mut self) {
        self.chat.retire()
    }
    pub fn root_pin_for_native_oob(&self) -> NativeRootPin {
        self.issuer.pin_for_native_out_of_band_transfer()
    }
    /// Borrowing prevents independent native issuer mutation while a chat job is
    /// outstanding. Caller must use the same native current-thread owner actor.
    pub fn chat_mut(&mut self) -> &mut NativeChatOwner {
        &mut self.chat
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::Barrier;

    #[test]
    fn cancellation_between_validation_and_adoption_wins_no_usable_owner() {
        let gate = Arc::new(AdoptionGate::new());
        let validated = Arc::new(Barrier::new(2));
        let release = Arc::new(Barrier::new(2));
        let worker_gate = gate.clone();
        let worker_validated = validated.clone();
        let worker_release = release.clone();
        let deadline = Instant::now() + Duration::from_secs(10);
        let worker = std::thread::spawn(move || {
            assert!(worker_gate.is_pending());
            worker_validated.wait(); // same final validation→adoption boundary
            worker_release.wait();
            worker_gate.adopt(deadline)
        });
        validated.wait();
        assert_eq!(gate.cancel(), Ok(()));
        release.wait();
        assert_eq!(worker.join().unwrap(), Err(Error::Auth));
        assert_eq!(gate.0.load(Ordering::Acquire), CANCELLED);
    }

    #[test]
    fn adopted_owner_makes_late_cancellation_explicitly_stale() {
        let gate = Arc::new(AdoptionGate::new());
        let (adopted_tx, adopted_rx) = std::sync::mpsc::channel();
        let (return_tx, return_rx) = std::sync::mpsc::channel();
        let worker_gate = gate.clone();
        let worker = std::thread::spawn(move || {
            worker_gate
                .adopt(Instant::now() + Duration::from_secs(10))
                .unwrap();
            adopted_tx.send(()).unwrap();
            return_rx.recv().unwrap(); // cancellation before owner return
            assert_eq!(worker_gate.0.load(Ordering::Acquire), ADOPTED);
        });
        adopted_rx.recv().unwrap();
        assert_eq!(gate.cancel(), Err(Error::Retired));
        return_tx.send(()).unwrap();
        worker.join().unwrap();
    }

    #[test]
    fn expired_final_construction_boundary_fails_before_adoption() {
        let gate = AdoptionGate::new();
        let deadline = Instant::now();
        assert!(gate.is_pending());
        assert_eq!(gate.adopt(deadline), Err(Error::Auth));
        assert_eq!(gate.0.load(Ordering::Acquire), FAILED);
        assert_eq!(gate.cancel(), Err(Error::Retired));
    }

    #[test]
    fn failed_or_dropped_confirmation_cannot_report_successful_cancellation() {
        let gate = Arc::new(AdoptionGate::new());
        drop(FailureGuard(gate.clone()));
        assert_eq!(
            gate.adopt(Instant::now() + Duration::from_secs(10)),
            Err(Error::Auth)
        );
        assert_eq!(gate.cancel(), Err(Error::Retired));
    }
}
