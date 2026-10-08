//! Native authentication evidence only, never a device/MLS/channel/media grant.
use super::*;
use std::time::{Duration, Instant};

/// Opaque, non-Serde native scope. Public getters contain nonsecret provenance;
/// getters alone do not authorize work. Revalidate through the locked callback.
/// Native random identities label actual window/profile/session state and are
/// never server account-version values. Rotation retires the old session ID.
///
/// ```compile_fail
/// use mnema_private_native_client_broker::NativeAuthenticatedScope;
/// fn cannot_export(scope: &NativeAuthenticatedScope) {
///     let _ = serde_json::to_vec(scope);
/// }
/// ```
///
/// ```compile_fail
/// use mnema_private_native_client_broker::NativeAuthenticatedScope;
/// let _ = serde_json::from_str::<NativeAuthenticatedScope>("{}");
/// ```
pub struct NativeAuthenticatedScope {
    publication: PublicationScope,
    origin: String,
    community: String,
    account: Uuid,
    family: Uuid,
    instance: Uuid,
    sequence: u64,
    role: String,
    window_identity: Uuid,
    profile_identity: Uuid,
    session_identity: Uuid,
    authentication_identity: Uuid,
    deadline: Instant,
    pub(super) client_binding: Option<(Uuid, u64)>,
}
impl fmt::Debug for NativeAuthenticatedScope {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("NativeAuthenticatedScope(REDACTED)")
    }
}
impl NativeAuthenticatedScope {
    pub fn origin(&self) -> &str {
        &self.origin
    }
    pub fn community_id(&self) -> &str {
        &self.community
    }
    pub fn account_id(&self) -> Uuid {
        self.account
    }
    pub fn family_id(&self) -> Uuid {
        self.family
    }
    pub fn client_instance_id(&self) -> Uuid {
        self.instance
    }
    pub fn refresh_sequence(&self) -> u64 {
        self.sequence
    }
    /// Server role gates administrative workflow, never cryptographic root trust.
    pub fn role(&self) -> &str {
        &self.role
    }
    pub fn native_window_identity(&self) -> Uuid {
        self.window_identity
    }
    pub fn native_profile_identity(&self) -> Uuid {
        self.profile_identity
    }
    pub fn native_session_identity(&self) -> Uuid {
        self.session_identity
    }
    pub fn authentication_intent(&self) -> Uuid {
        self.authentication_identity
    }
    pub fn monotonic_access_deadline(&self) -> Instant {
        self.deadline
    }
    pub fn native_context(&self) -> Option<Uuid> {
        self.client_binding.map(|(context, _)| context)
    }
}

/// Anchor at accepted grant admission, not at every later scope mint. A wall
/// clock rollback cannot repeatedly re-extend native capability lifetime.
pub(super) fn admission_deadline(grant: &Grant) -> Result<Instant, Error> {
    let now = Utc::now();
    let remaining = std::cmp::min(grant.access_expires_at, grant.family_expires_at)
        .signed_duration_since(now)
        .to_std()
        .map_err(|_| Error::Expired)?;
    if remaining.is_zero() {
        return Err(Error::Expired);
    }
    Instant::now()
        .checked_add(remaining.min(Duration::from_secs(300)))
        .ok_or(Error::Exhausted)
}

impl State {
    fn authenticated(
        &self,
        broker: u64,
        owner: WindowOwner,
    ) -> Result<(&Window, &Active, &Session), Error> {
        let window = self.main(broker, owner)?;
        if window.cancel.is_cancelled() {
            return Err(Error::Stale);
        }
        let active = self.active.as_ref().ok_or(Error::NoProfile)?;
        if active.pending_invocation.is_some()
            || active.status == Status::Rotating
            || active.flight.is_some()
        {
            return Err(Error::Busy);
        }
        if active.status != Status::Authenticated {
            return Err(Error::ReauthRequired);
        }
        if active.cancel.is_cancelled() {
            return Err(Error::Stale);
        }
        let session = active.session.as_ref().ok_or(Error::ReauthRequired)?;
        if session.grant.access_expires_at <= Utc::now()
            || session.grant.family_expires_at <= Utc::now()
            || Instant::now() >= session.access_deadline
        {
            return Err(Error::Expired);
        }
        Ok((window, active, session))
    }
    pub(super) fn check_authenticated(
        &self,
        broker: u64,
        owner: WindowOwner,
        scope: &NativeAuthenticatedScope,
    ) -> Result<(), Error> {
        if owner != scope.publication.window {
            return Err(Error::Denied);
        }
        let (window, active, session) = self.authenticated(broker, owner)?;
        let profile = self
            .profiles
            .operation_profile(scope.publication.profile)
            .map_err(|_| Error::Stale)?;
        if active.epoch != scope.publication.epoch
            || window.native_identity != scope.window_identity
            || active.native_identity != scope.profile_identity
            || session.native_identity != scope.session_identity
            || session.authentication_identity != scope.authentication_identity
            || profile.origin().as_str() != scope.origin
            || profile.community_id() != scope.community
            || session.grant.user.account_id() != scope.account
            || session.grant.family_id.0 != scope.family
            || session.grant.client_instance_id.0 != scope.instance
            || session.grant.refresh_sequence != scope.sequence
            || session.accepted_role != scope.role
            || session.access_deadline != scope.deadline
        {
            return Err(Error::Stale);
        }
        if Instant::now() >= scope.deadline {
            return Err(Error::Expired);
        }
        Ok(())
    }
}
impl Broker {
    /// Native-only. Caller must own an actual native window, never an IPC label.
    pub fn authenticated_scope(
        &self,
        owner: WindowOwner,
    ) -> Result<NativeAuthenticatedScope, Error> {
        let s = self.inner.state.lock().map_err(|_| Error::Internal)?;
        let (window, active, session) = s.authenticated(self.inner.owner, owner)?;
        let operation = s
            .profiles
            .begin_operation(active.handle)
            .map_err(|_| Error::Stale)?;
        let profile = s
            .profiles
            .operation_profile(operation)
            .map_err(|_| Error::Stale)?;
        Ok(NativeAuthenticatedScope {
            publication: PublicationScope {
                window: owner,
                profile: operation,
                epoch: active.epoch,
            },
            origin: profile.origin().as_str().to_owned(),
            community: profile.community_id().to_owned(),
            account: session.grant.user.account_id(),
            family: session.grant.family_id.0,
            instance: session.grant.client_instance_id.0,
            sequence: session.grant.refresh_sequence,
            role: session.accepted_role.clone(),
            window_identity: window.native_identity,
            profile_identity: active.native_identity,
            session_identity: session.native_identity,
            authentication_identity: session.authentication_identity,
            deadline: session.access_deadline,
            client_binding: None,
        })
    }
    pub fn check_authenticated_scope(
        &self,
        owner: WindowOwner,
        scope: &NativeAuthenticatedScope,
    ) -> Result<(), Error> {
        let s = self.inner.state.lock().map_err(|_| Error::Internal)?;
        s.check_authenticated(self.inner.owner, owner, scope)
    }
    /// Trusted native synchronous transform/enqueue only. The broker state lock
    /// covers final authorization and enqueue. Callback must be bounded and
    /// nonblocking, must not re-enter broker/client, or acquire window/registry
    /// locks. Expiry cannot be paused: downstream must discard work past deadline.
    pub fn with_authenticated_publication<T>(
        &self,
        owner: WindowOwner,
        scope: &NativeAuthenticatedScope,
        publish: impl FnOnce() -> T,
    ) -> Result<T, Error> {
        let s = self.inner.state.lock().map_err(|_| Error::Internal)?;
        s.check_authenticated(self.inner.owner, owner, scope)?;
        let output = publish();
        // Do not claim that callback can stop time or undo external side effects.
        // This second check suppresses returned completion if deadline expired.
        s.check_authenticated(self.inner.owner, owner, scope)?;
        Ok(output)
    }
}
