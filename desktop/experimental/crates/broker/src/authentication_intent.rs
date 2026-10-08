//! Actual native admission tickets. Public UUIDs are comparisons, never grants.
use super::*;
use std::sync::atomic::AtomicBool;

#[derive(Clone, Copy, PartialEq, Eq)]
pub(super) enum InvocationKind {
    Read,
    Login,
    Logout,
    Password,
    Register,
}
impl InvocationKind {
    fn is_transition(self) -> bool {
        matches!(
            self,
            Self::Login | Self::Logout | Self::Password | Self::Register
        )
    }
}
pub(super) struct InvocationTicket {
    inner: Arc<Inner>,
    id: Uuid,
    request_id: Uuid,
    window: WindowOwner,
    profile: Uuid,
    epoch: u64,
    authentication: Option<Uuid>,
    authority_revision: Option<Uuid>,
    kind: InvocationKind,
    retained_scope: Option<Arc<NativeAuthenticatedScope>>,
    consumed: AtomicBool,
}
impl fmt::Debug for InvocationTicket {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("InvocationTicket(REDACTED)")
    }
}
impl Drop for InvocationTicket {
    fn drop(&mut self) {
        if !self.kind.is_transition() || self.consumed.load(Ordering::Acquire) {
            return;
        }
        if let Ok(mut state) = self.inner.state.lock()
            && let Some(active) = state.active.as_mut()
            && active.native_identity == self.profile
            && active.pending_invocation == Some(self.id)
        {
            // Submission failure/drop never restores already-retired authority.
            active.pending_invocation = None;
            active.cancel.cancel();
            active.session = None;
            active.status = Status::ReauthRequired;
            if let Some(flight) = active.flight.take() {
                flight.finish(Err(Error::Stale));
            }
        }
    }
}
impl State {
    fn expected_authentication(&self, expected: Option<Uuid>) -> Result<&Active, Error> {
        if expected.is_some_and(|id| id.is_nil()) {
            return Err(Error::InvalidInput);
        }
        let active = self.active.as_ref().ok_or(Error::NoProfile)?;
        if active.pending_invocation.is_some()
            || matches!(
                active.status,
                Status::SigningIn | Status::ChangingCredentials | Status::Registering
            )
        {
            return Err(Error::Busy);
        }
        let live = active.session.is_some()
            && matches!(active.status, Status::Authenticated | Status::Rotating);
        if live {
            if active
                .session
                .as_ref()
                .map(|session| session.authentication_identity)
                != expected
            {
                return Err(Error::Stale);
            }
        } else if let Some(expected) = expected {
            return Err(if active.last_authentication_identity == Some(expected) {
                Error::ReauthRequired
            } else {
                Error::Stale
            });
        }
        Ok(active)
    }
}
impl Broker {
    /// Nonsecret currently accepted family selector; pending transitions are
    /// deliberately unavailable. A retired selector never matches a new family.
    pub fn authentication_intent(&self, owner: WindowOwner) -> Result<Option<Uuid>, Error> {
        let state = self.inner.state.lock().map_err(|_| Error::Internal)?;
        state.main(self.inner.owner, owner)?;
        let Some(active) = state.active.as_ref() else {
            return Ok(None);
        };
        if active.pending_invocation.is_some()
            || matches!(
                active.status,
                Status::SigningIn | Status::ChangingCredentials | Status::Registering
            )
        {
            return Err(Error::Busy);
        }
        if active.session.is_some()
            && matches!(active.status, Status::Authenticated | Status::Rotating)
        {
            Ok(active
                .session
                .as_ref()
                .map(|session| session.authentication_identity))
        } else {
            Ok(None)
        }
    }

    /// Atomic nonsecret selection/authentication descriptor. The caller holds
    /// the actual native window admission lock; both selectors are read under
    /// this same State lock, never assembled from different incarnations.
    pub(super) fn selected_intents(
        &self,
        owner: WindowOwner,
    ) -> Result<(Option<Uuid>, Option<Uuid>), Error> {
        let state = self.inner.state.lock().map_err(|_| Error::Internal)?;
        state.main(self.inner.owner, owner)?;
        let Some(active) = state.active.as_ref() else {
            return Ok((None, None));
        };
        state
            .profiles
            .profile(active.handle)
            .map_err(|_| Error::Stale)?;
        if active.pending_invocation.is_some()
            || matches!(
                active.status,
                Status::SigningIn | Status::ChangingCredentials | Status::Registering
            )
        {
            return Err(Error::Busy);
        }
        let authentication = if matches!(active.status, Status::Authenticated | Status::Rotating) {
            active
                .session
                .as_ref()
                .map(|session| session.authentication_identity)
        } else {
            None
        };
        Ok((Some(active.native_identity), authentication))
    }

    pub(super) fn with_expected_authentication<T>(
        &self,
        owner: WindowOwner,
        expected: Option<Uuid>,
        publish: impl FnOnce() -> T,
    ) -> Result<T, Error> {
        let state = self.inner.state.lock().map_err(|_| Error::Internal)?;
        state.main(self.inner.owner, owner)?;
        if state.active.is_some() {
            state.expected_authentication(expected)?;
        } else if expected.is_some() {
            return Err(Error::Stale);
        }
        Ok(publish())
    }
    pub(super) fn invalidate_with_authentication(
        &self,
        owner: WindowOwner,
        expected: Option<Uuid>,
        retire: impl FnOnce(),
    ) -> Result<(), Error> {
        let mut state = self.inner.state.lock().map_err(|_| Error::Internal)?;
        state.main(self.inner.owner, owner)?;
        if state.active.is_some() {
            state.expected_authentication(expected)?;
        } else if expected.is_some() {
            return Err(Error::Stale);
        }
        // Native custody retirement is bounded/non-reentrant. Exact comparison,
        // retirement and profile destruction are one admission under State.
        retire();
        if let Some(active) = state.active.take() {
            active.cancel.cancel();
            if let Some(flight) = active.flight {
                flight.finish(Err(Error::Stale));
            }
        }
        state.cancel_requests(None);
        state.profiles.teardown().map_err(|_| Error::Exhausted)?;
        self.inner.idle.notify_waiters();
        Ok(())
    }
    /// A new native facade shares the SAME broker and vault, not a new session
    /// actor. Ticket creation is private; only native client admission calls it.
    pub(super) fn bind_invocation(
        &self,
        owner: WindowOwner,
        expected: Option<Uuid>,
        request_id: Uuid,
        kind: InvocationKind,
    ) -> Result<Self, Error> {
        self.bind_invocation_retained(owner, expected, request_id, kind, None)
    }
    pub(super) fn bind_invocation_retained(
        &self,
        owner: WindowOwner,
        expected: Option<Uuid>,
        request_id: Uuid,
        kind: InvocationKind,
        retained_scope: Option<Arc<NativeAuthenticatedScope>>,
    ) -> Result<Self, Error> {
        if request_id.is_nil() {
            return Err(Error::InvalidInput);
        }
        let mut state = self.inner.state.lock().map_err(|_| Error::Internal)?;
        state.main(self.inner.owner, owner)?;
        if let Some(scope) = retained_scope.as_ref() {
            if kind != InvocationKind::Read {
                return Err(Error::Denied);
            }
            state.check_authenticated(self.inner.owner, owner, scope)?;
        }
        let active = state.expected_authentication(expected)?;
        if kind == InvocationKind::Register && active.session.is_some() {
            return Err(Error::Denied);
        }
        if matches!(kind, InvocationKind::Logout | InvocationKind::Password)
            && active.session.is_none()
        {
            return Err(Error::ReauthRequired);
        }
        if kind.is_transition() && active.status == Status::Rotating {
            return Err(Error::Busy);
        }
        let id = Uuid::new_v4();
        if kind.is_transition() {
            state.cancel_requests(None);
            let active = state.active.as_mut().unwrap();
            active.epoch = active.epoch.checked_add(1).ok_or(Error::Exhausted)?;
            active.cancel.cancel();
            active.pending_invocation = Some(id);
        }
        let active = state.active.as_ref().unwrap();
        let ticket = InvocationTicket {
            inner: self.inner.clone(),
            id,
            request_id,
            window: owner,
            profile: active.native_identity,
            epoch: active.epoch,
            authentication: active
                .session
                .as_ref()
                .map(|session| session.authentication_identity)
                .or(active.last_authentication_identity),
            authority_revision: active
                .session
                .as_ref()
                .map(|session| session.native_identity),
            kind,
            retained_scope,
            consumed: AtomicBool::new(false),
        };
        Ok(Self {
            inner: self.inner.clone(),
            journal: self.journal.clone(),
            invocation: Some(Arc::new(ticket)),
            memory: self.memory.clone(),
            #[cfg(any(test, feature = "synthetic-transport-fixture"))]
            root: self.root.clone(),
            #[cfg(feature = "synthetic-transport-fixture")]
            fixture_scope: self.fixture_scope.clone(),
        })
    }
    pub(super) fn check_invocation_job(&self, request_id: Uuid) -> Result<(), Error> {
        if let Some(ticket) = &self.invocation
            && (ticket.request_id != request_id || ticket.consumed.load(Ordering::Acquire))
        {
            return Err(Error::Stale);
        }
        Ok(())
    }
    /// Called under the original Broker state mutex, before grant selection or
    /// credential/session mutation. A delayed invocation cannot recapture state.
    pub(super) fn consume_invocation(
        &self,
        state: &mut State,
        owner: WindowOwner,
        kind: InvocationKind,
    ) -> Result<(), Error> {
        let Some(ticket) = &self.invocation else {
            // Trusted native operations retain their existing opaque scope fences.
            return Ok(());
        };
        state.main(self.inner.owner, owner)?;
        if let Some(scope) = ticket.retained_scope.as_ref() {
            state.check_authenticated(self.inner.owner, owner, scope)?;
        }
        let active = state.active.as_mut().ok_or(Error::Stale)?;
        if owner != ticket.window
            || ticket.kind != kind
            || ticket.profile != active.native_identity
            || ticket.epoch != active.epoch
            || ticket.authentication
                != active
                    .session
                    .as_ref()
                    .map(|session| session.authentication_identity)
                    .or(active.last_authentication_identity)
            || ticket.authority_revision
                != active
                    .session
                    .as_ref()
                    .map(|session| session.native_identity)
            || (kind.is_transition() && active.pending_invocation != Some(ticket.id))
            || (!kind.is_transition() && active.pending_invocation.is_some())
            || ticket.consumed.swap(true, Ordering::AcqRel)
        {
            return Err(Error::Stale);
        }
        if kind.is_transition() {
            active.pending_invocation = None;
        }
        Ok(())
    }
}
