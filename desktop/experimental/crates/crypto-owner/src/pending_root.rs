//! Actual native first-root ceremony composition. No renderer approval value,
//! server-selected authority key or restored-owner activation constructor.
use super::*;
use base64::{Engine, engine::general_purpose::STANDARD};
use mnema_crypto_enrollment_prototype::{
    Issuer, NativeAdmissionIntent, NativeRootPin, NativeSecrets,
};
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
    fn removal_envelope_preserves_exact_bytes_and_rejects_oversize_before_reservation() {
        let roster = vec![0x91; 32768];
        let commit = vec![0x42; 32768];
        // Each input independently fits the relay allowance, while their
        // combined base64 control exceeds it. No capability may escape.
        assert_eq!(device_removal_envelope(&roster, &commit), Err(Error::Limit));
        assert_eq!(device_removal_envelope(&vec![1; 131072], &[2]), Err(Error::Limit));
        assert_eq!(device_removal_envelope(&[], &[2]), Err(Error::Invalid));
        let wire = device_removal_envelope(&roster[..1024], &commit[..2048]).unwrap();
        let (domain, encoded_roster, encoded_commit): (String, String, String) =
            serde_json::from_slice(&wire).unwrap();
        assert_eq!(domain, "MnemaTalk NativeDeviceRemoval/v1");
        assert_eq!(STANDARD.decode(encoded_roster).unwrap(), roster[..1024]);
        assert_eq!(STANDARD.decode(encoded_commit).unwrap(), commit[..2048]);
    }

    #[test]
    fn removal_envelope_maximum_encoding_fits_the_actual_broker_boundary() {
        // Find the last representable base64 length below the actual fixed
        // allowance, then exercise the real broker constructor with that wire.
        let commit = [7];
        let (mut max, mut rejected) = (1, 65536);
        while rejected - max > 1 {
            let candidate = max + (rejected - max) / 2;
            if device_removal_envelope(&vec![3; candidate], &commit).is_ok() {
                max = candidate;
            } else {
                rejected = candidate;
            }
        }
        let wire = device_removal_envelope(&vec![3; max], &commit).unwrap();
        assert!(wire.len() <= 65536 && wire.len() > 65532);
        assert!(mnema_private_native_client_broker::NativeOpaqueEvent::from_native_outbox(
            Uuid::new_v4(), Uuid::new_v4(), b"group", &wire,
        ).is_ok());
        assert_eq!(device_removal_envelope(&vec![3; max + 1], &commit), Err(Error::Limit));
    }

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

/// Native actor-owned control reservation. No Clone/Serde or renderer approval
/// constructor. It is not a completed publication or a recovery capability.
pub struct NativePreparedDeviceRemoval {
    serial: Uuid,
    event: Uuid,
    auth: Arc<NativeAuthenticatedScope>,
    epoch: u64,
    generation: u64,
    deadline: Instant,
    wire: Vec<u8>,
    commit: Vec<u8>,
}
fn device_removal_envelope(roster: &[u8], commit: &[u8]) -> Result<Vec<u8>> {
    // Bound allocation as well as the exact encoded envelope. Keep the existing
    // relay allowance; membership changes do not grant a larger transport.
    if roster.is_empty() || commit.is_empty() {
        return Err(Error::Invalid);
    }
    if roster.len() > 65536 || commit.len() > 65536 {
        return Err(Error::Limit);
    }
    let wire = serde_json::to_vec(&(
        "MnemaTalk NativeDeviceRemoval/v1",
        STANDARD.encode(roster),
        STANDARD.encode(commit),
    ))
    .map_err(|_| Error::Invalid)?;
    if wire.len() > 65536 {
        return Err(Error::Limit);
    }
    Ok(wire)
}
impl NativePreparedDeviceRemoval {
    pub fn client_event_id(&self) -> Uuid {
        self.event
    }
}
impl<S: NativeSecrets> NativeFirstRootOwner<'_, S> {
    /// The original native auth admission survives metadata, modal confirmation
    /// and actual issuer/Core processing. Target facts come from real MLS and
    /// signed membership; renderer account/device UUIDs select only a lookup.
    pub async fn prepare_device_removal_retained(
        &mut self,
        auth: Arc<NativeAuthenticatedScope>,
        account: Uuid,
        device: Uuid,
        dialog: &dyn NativeTrustDialog,
    ) -> Result<NativePreparedDeviceRemoval> {
        let result = self
            .prepare_device_removal_inner(auth, account, device, dialog)
            .await;
        if result.is_err() {
            self.retire();
        }
        result
    }
    async fn prepare_device_removal_inner(
        &mut self,
        auth: Arc<NativeAuthenticatedScope>,
        account: Uuid,
        device: Uuid,
        dialog: &dyn NativeTrustDialog,
    ) -> Result<NativePreparedDeviceRemoval> {
        if account.is_nil() || device.is_nil() || self.chat.membership_pending.is_some() {
            return Err(Error::Invalid);
        }
        self.chat.check_admitted_scope(&auth)?;
        self.chat.current(now()?)?;
        let client = self.chat.client.clone();
        let lease = self.chat.lease.clone();
        let channel = self.chat.owner.channel;
        fresh_root_metadata(&client, &lease, channel, &auth).await?;
        self.chat.check_admitted_scope(&auth)?;
        let target = self
            .chat
            .sdk
            .native_peer_for_removal(&account.to_string(), &device.to_string(), now()?)
            .map_err(|_| Error::Crypto)?;
        let pin = self.issuer.pin_for_native_out_of_band_transfer();
        let facts = NativeTrustDisplayFacts::from_native_core(
            channel,
            device,
            &self.chat.owner.group,
            pin.authority_key(),
            pin.fingerprint().map_err(|_| Error::Crypto)?,
            target
                .signature_key()
                .try_into()
                .map_err(|_| Error::Binding)?,
        )
        .map_err(|_| Error::Binding)?;
        let deadline = Instant::now()
            .checked_add(Duration::from_secs(120))
            .ok_or(Error::Invalid)?
            .min(auth.monotonic_access_deadline());
        let event = Uuid::new_v4();
        let request = NativeDialogRequest::device_removal(
            event,
            auth.clone(),
            facts,
            deadline,
            account,
            target.identity(),
        )
        .map_err(|_| Error::Auth)?;
        client
            .check_authenticated_scope(&lease, &auth)
            .map_err(|_| Error::Auth)?;
        let decision = dialog
            .confirm_device_removal(request.clone())
            .await
            .map_err(|_| Error::Auth)?;
        decision.consume(&request).map_err(|_| Error::Auth)?;
        fresh_root_metadata(&client, &lease, channel, &auth).await?;
        self.chat.check_admitted_scope(&auth)?;
        self.chat
            .sdk
            .check_native_peer_for_removal(&target, now()?)
            .map_err(|_| Error::Crypto)?;
        let intent = NativeAdmissionIntent::from_native_out_of_band_pin(
            target.account(),
            target.device(),
            target.identity(),
            target
                .signature_key()
                .try_into()
                .map_err(|_| Error::Binding)?,
        )
        .map_err(|_| Error::Binding)?;
        request.check_deadline().map_err(|_| Error::Auth)?;
        client
            .check_authenticated_scope(&lease, &auth)
            .map_err(|_| Error::Auth)?;
        // Native storage I/O stays outside broker locks. Any loss of auth,
        // deadline or partial issuer/Core failure retires before output.
        let when = now()?;
        let roster = self
            .issuer
            .revoke_exact_native_device(intent, when)
            .map_err(|_| Error::Crypto)?;
        let commit = self
            .chat
            .sdk
            .remove_root_revoked_native_peer(
                &event.to_string(),
                target.identity(),
                target.signature_key(),
                &roster,
                when,
            )
            .map_err(|_| Error::Crypto)?;
        request.check_deadline().map_err(|_| Error::Auth)?;
        self.chat.check_admitted_scope(&auth)?;
        let current = self
            .chat
            .sdk
            .native_owner_facts(now()?)
            .map_err(|_| Error::Crypto)?;
        // No prepared capability escapes for an unpublishable control. Issuer
        // and MLS transitions are already durable; failure retires this owner
        // rather than resuming chat or claiming transport completion.
        let wire = device_removal_envelope(&roster, &commit)?;
        let prepared = NativePreparedDeviceRemoval {
            serial: self.chat.serial,
            event,
            auth,
            epoch: current.epoch(),
            generation: current.generation(),
            deadline,
            wire,
            commit,
        };
        // The typed chat path must not send the new epoch before real control
        // transport acknowledges it. Only that future native ACK may clear this.
        self.chat.membership_pending = Some(event);
        client
            .with_authenticated_publication(&lease, &prepared.auth.clone(), || {
                request.check_deadline().map_err(|_| Error::Auth)?;
                Ok(prepared)
            })
            .map_err(|_| Error::Auth)?
    }
    /// Build only the exact native reserved control for transport. This is not
    /// an ACK or permission to clear the pending membership gate.
    pub fn device_removal_wire_for_native_relay(
        &mut self,
        prepared: &NativePreparedDeviceRemoval,
    ) -> Result<Vec<u8>> {
        let result = self.device_removal_wire_inner(prepared);
        if result.is_err() {
            self.retire();
        }
        result
    }
    fn device_removal_wire_inner(
        &mut self,
        prepared: &NativePreparedDeviceRemoval,
    ) -> Result<Vec<u8>> {
        self.chat.check_admitted_scope(&prepared.auth)?;
        if Instant::now() >= prepared.deadline {
            return Err(Error::Auth);
        }
        if self.chat.serial != prepared.serial
            || self.chat.membership_pending != Some(prepared.event)
        {
            return Err(Error::Binding);
        }
        let current = self
            .chat
            .sdk
            .native_owner_facts(now()?)
            .map_err(|_| Error::Crypto)?;
        if current.epoch() != prepared.epoch || current.generation() != prepared.generation {
            return Err(Error::Binding);
        }
        let pending = self
            .chat
            .sdk
            .pending_native_host_publication(&prepared.event.to_string(), now()?)
            .map_err(|_| Error::Crypto)?
            .ok_or(Error::Binding)?;
        if pending != prepared.commit {
            return Err(Error::Binding);
        }
        self.chat
            .client
            .with_authenticated_publication(&self.chat.lease, &prepared.auth, || {
                if Instant::now() >= prepared.deadline {
                    return Err(Error::Auth);
                }
                Ok(prepared.wire.clone())
            })
            .map_err(|_| Error::Auth)?
    }
}
