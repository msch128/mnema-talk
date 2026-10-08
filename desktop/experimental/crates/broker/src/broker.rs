use crate::{
    Error, Password,
    http::{Endpoint, Http},
    journal::Ephemeral,
    wire::{Grant, User},
};
use chrono::Utc;
use mnema_desktop_probe::profiles::{OperationHandle, ProfileHandle, Profiles};
use mnema_private_vault_facade::{Namespace, Record, Refresh, Vault};
use reqwest::Url;
use std::{
    collections::HashMap,
    fmt,
    sync::{
        Arc, Mutex,
        atomic::{AtomicU64, Ordering},
    },
};
use tokio::sync::Notify;
use tokio_util::sync::CancellationToken;
use uuid::Uuid;
use zeroize::Zeroizing;
static NEXT_BROKER: AtomicU64 = AtomicU64::new(1);
#[derive(Clone, Copy, PartialEq, Eq)]
pub struct WindowOwner {
    broker: u64,
    incarnation: u64,
}
impl fmt::Debug for WindowOwner {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("WindowOwner(REDACTED)")
    }
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum WindowRole {
    Main,
    Overlay,
    Widget,
}
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Status {
    Empty,
    Selected,
    SigningIn,
    Authenticated,
    Rotating,
    ReauthRequired,
}
struct Window {
    native_identity: Uuid,
    label: String,
    role: WindowRole,
    cancel: CancellationToken,
}
struct Session {
    accepted_role: String,
    native_identity: Uuid,
    access_deadline: std::time::Instant,
    grant: Arc<Grant>,
    namespace: Namespace,
}
struct Active {
    native_identity: Uuid,
    handle: ProfileHandle,
    epoch: u64,
    origin: Url,
    http: Http,
    cancel: CancellationToken,
    status: Status,
    session: Option<Session>,
    flight: Option<Arc<Flight>>,
}
#[derive(Clone, Copy, PartialEq, Eq)]
enum RequestPhase {
    Prepared,
    Running,
    Delivered,
}
struct RequestEntry {
    window: WindowOwner,
    cancel: CancellationToken,
    phase: RequestPhase,
}
struct State {
    profiles: Profiles,
    active: Option<Active>,
    windows: HashMap<u64, Window>,
    requests: HashMap<u64, RequestEntry>,
    next_window: u64,
    next_request: u64,
    selection: u64,
}
struct Inner {
    owner: u64,
    state: Mutex<State>,
    idle: Notify,
}
struct Flight {
    outcome: Mutex<Option<Result<u64, Error>>>,
    changed: Notify,
}
impl Flight {
    fn new() -> Self {
        Self {
            outcome: Mutex::new(None),
            changed: Notify::new(),
        }
    }
    fn result(&self) -> Option<Result<u64, Error>> {
        *self.outcome.lock().unwrap_or_else(|p| p.into_inner())
    }
    fn finish(&self, result: Result<u64, Error>) {
        let mut value = self.outcome.lock().unwrap_or_else(|p| p.into_inner());
        if value.is_none() {
            *value = Some(result);
            self.changed.notify_waiters()
        }
    }
}
struct RequestGuard {
    inner: Arc<Inner>,
    id: u64,
}
impl Drop for RequestGuard {
    fn drop(&mut self) {
        if let Ok(mut state) = self.inner.state.lock() {
            state.requests.remove(&self.id);
        }
        self.inner.idle.notify_waiters();
    }
}
pub struct Operation {
    auth_revision: Option<Uuid>,
    window: WindowOwner,
    profile: OperationHandle,
    epoch: u64,
    id: u64,
    http: Http,
    origin: Url,
    grant: Option<Arc<Grant>>,
    cancel: CancellationToken,
    scope_cancel: CancellationToken,
    window_cancel: CancellationToken,
    _guard: RequestGuard,
}
impl fmt::Debug for Operation {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("Operation(REDACTED)")
    }
}
#[derive(Clone, Copy)]
pub struct CancelHandle {
    window: WindowOwner,
    id: u64,
}
impl fmt::Debug for CancelHandle {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("CancelHandle(REDACTED)")
    }
}
impl Operation {
    pub fn cancel_handle(&self) -> CancelHandle {
        CancelHandle {
            window: self.window,
            id: self.id,
        }
    }
    async fn cancelled(&self) {
        tokio::select! {_ = self.cancel.cancelled()=>{},_ = self.scope_cancel.cancelled()=>{},_ = self.window_cancel.cancelled()=>{}}
    }
}
pub struct MeDelivery {
    operation: Operation,
    value: User,
}
#[derive(Clone, Copy)]
pub struct PublicationScope {
    window: WindowOwner,
    profile: OperationHandle,
    epoch: u64,
}
impl fmt::Debug for PublicationScope {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("PublicationScope(REDACTED)")
    }
}
pub struct LoginDelivery {
    scope: PublicationScope,
    value: User,
}
impl fmt::Debug for LoginDelivery {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("LoginDelivery(REDACTED)")
    }
}
pub struct LogoutDelivery {
    scope: PublicationScope,
}
impl fmt::Debug for LogoutDelivery {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("LogoutDelivery(REDACTED)")
    }
}
impl fmt::Debug for MeDelivery {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("MeDelivery(REDACTED)")
    }
}
struct LoginGuard {
    inner: Arc<Inner>,
    profile: OperationHandle,
    epoch: u64,
    armed: bool,
}
impl Drop for LoginGuard {
    fn drop(&mut self) {
        if !self.armed {
            return;
        }
        if let Ok(mut state) = self.inner.state.lock()
            && state.profiles.is_current(self.profile)
            && let Some(active) = state.active.as_mut()
            && active.epoch == self.epoch
            && active.status == Status::SigningIn
        {
            active.status = Status::ReauthRequired;
            active.session = None;
            active.cancel.cancel();
        }
    }
}
struct RotationGuard {
    inner: Arc<Inner>,
    profile: OperationHandle,
    epoch: u64,
    flight: Arc<Flight>,
    armed: bool,
}
impl Drop for RotationGuard {
    fn drop(&mut self) {
        if !self.armed {
            return;
        }
        if let Ok(mut state) = self.inner.state.lock()
            && state.profiles.is_current(self.profile)
            && let Some(active) = state.active.as_mut()
            && active.epoch == self.epoch
            && active
                .flight
                .as_ref()
                .is_some_and(|f| Arc::ptr_eq(f, &self.flight))
        {
            active.status = Status::ReauthRequired;
            active.session = None;
            active.flight = None;
            active.cancel.cancel();
        }
        self.flight.finish(Err(Error::UncertainRotation));
    }
}

pub struct Broker {
    inner: Arc<Inner>,
    journal: Vault<Ephemeral>,
    memory: Ephemeral,
    #[cfg(any(test, feature = "synthetic-transport-fixture"))]
    root: Option<Vec<u8>>,
    #[cfg(feature = "synthetic-transport-fixture")]
    fixture_scope: Option<(String, String, Uuid)>,
}
impl fmt::Debug for Broker {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("Broker(REDACTED)")
    }
}
impl State {
    fn window(&self, broker: u64, owner: WindowOwner) -> Result<&Window, Error> {
        if owner.broker != broker {
            return Err(Error::Denied);
        }
        self.windows.get(&owner.incarnation).ok_or(Error::Stale)
    }
    fn main(&self, broker: u64, owner: WindowOwner) -> Result<&Window, Error> {
        let window = self.window(broker, owner)?;
        if window.role != WindowRole::Main {
            return Err(Error::Denied);
        }
        Ok(window)
    }
    fn current(&self, broker: u64, op: &Operation) -> Result<(), Error> {
        self.main(broker, op.window)?;
        if op.cancel.is_cancelled() {
            return Err(Error::Cancelled);
        }
        if op.scope_cancel.is_cancelled()
            || op.window_cancel.is_cancelled()
            || !self.profiles.is_current(op.profile)
            || self.active.as_ref().is_none_or(|a| a.epoch != op.epoch)
        {
            return Err(Error::Stale);
        }
        Ok(())
    }
    fn cancel_requests(&mut self, owner: Option<WindowOwner>) {
        self.requests.retain(|_, request| {
            if owner.is_none_or(|o| o == request.window) {
                request.cancel.cancel();
                request.phase == RequestPhase::Running
            } else {
                true
            }
        });
    }
}
impl Broker {
    /// Native lifecycle hook only. Seal synchronously; pending futures drain
    /// under the dedicated actor before the process-memory journal is reused.
    pub fn invalidate_native_context(&self) -> Result<(), Error> {
        let mut s = self.inner.state.lock().map_err(|_| Error::Internal)?;
        if let Some(active) = s.active.take() {
            active.cancel.cancel();
            if let Some(flight) = active.flight {
                flight.finish(Err(Error::Stale))
            }
        }
        s.cancel_requests(None);
        s.profiles.teardown().map_err(|_| Error::Exhausted)?;
        self.inner.idle.notify_waiters();
        Ok(())
    }
    fn scope_from_operation(op: &Operation) -> PublicationScope {
        PublicationScope {
            window: op.window,
            profile: op.profile,
            epoch: op.epoch,
        }
    }
    pub fn check_publication_scope(
        &self,
        owner: WindowOwner,
        scope: PublicationScope,
    ) -> Result<(), Error> {
        let s = self.inner.state.lock().map_err(|_| Error::Internal)?;
        s.main(self.inner.owner, owner)?;
        if owner != scope.window {
            return Err(Error::Denied);
        }
        if !s.profiles.is_current(scope.profile)
            || s.active.as_ref().is_none_or(|a| a.epoch != scope.epoch)
        {
            return Err(Error::Stale);
        }
        Ok(())
    }
    /// Trusted native enqueue only. Never re-enter broker from the callback.
    pub(crate) fn with_publication<T>(
        &self,
        owner: WindowOwner,
        scope: PublicationScope,
        publish: impl FnOnce() -> T,
    ) -> Result<T, Error> {
        let s = self.inner.state.lock().map_err(|_| Error::Internal)?;
        s.main(self.inner.owner, owner)?;
        if owner != scope.window {
            return Err(Error::Denied);
        }
        if !s.profiles.is_current(scope.profile)
            || s.active.as_ref().is_none_or(|a| a.epoch != scope.epoch)
        {
            return Err(Error::Stale);
        }
        Ok(publish())
    }
    pub fn selected_publication_scope(
        &self,
        owner: WindowOwner,
    ) -> Result<PublicationScope, Error> {
        let s = self.inner.state.lock().map_err(|_| Error::Internal)?;
        s.main(self.inner.owner, owner)?;
        let a = s.active.as_ref().ok_or(Error::NoProfile)?;
        Ok(PublicationScope {
            window: owner,
            profile: s
                .profiles
                .begin_operation(a.handle)
                .map_err(|_| Error::Stale)?,
            epoch: a.epoch,
        })
    }
    pub fn ephemeral() -> Result<Self, Error> {
        let owner = NEXT_BROKER
            .try_update(Ordering::AcqRel, Ordering::Acquire, |n| n.checked_add(1))
            .map_err(|_| Error::Exhausted)?;
        let memory = Ephemeral::default();
        Ok(Self {
            inner: Arc::new(Inner {
                owner,
                state: Mutex::new(State {
                    profiles: Profiles::new().map_err(|_| Error::Exhausted)?,
                    active: None,
                    windows: HashMap::new(),
                    requests: HashMap::new(),
                    next_window: 0,
                    next_request: 0,
                    selection: 0,
                }),
                idle: Notify::new(),
            }),
            journal: Vault::new(memory.clone()),
            memory,
            #[cfg(any(test, feature = "synthetic-transport-fixture"))]
            root: None,
            #[cfg(feature = "synthetic-transport-fixture")]
            fixture_scope: None,
        })
    }
    #[cfg(test)]
    pub(crate) fn fixture(root: Vec<u8>) -> Self {
        let mut b = Self::ephemeral().unwrap();
        b.root = Some(root);
        b
    }
    #[cfg(feature = "synthetic-transport-fixture")]
    pub fn qualification_fixture(
        origin: String,
        community: String,
        account: Uuid,
        root: Vec<u8>,
    ) -> Result<Self, Error> {
        let u = reqwest::Url::parse(&origin).map_err(|_| Error::InvalidInput)?;
        if u.scheme() != "https"
            || u.host_str() != Some("127.0.0.1")
            || u.port().is_none()
            || u.path() != "/"
            || u.query().is_some()
            || u.fragment().is_some()
            || !u.username().is_empty()
            || u.password().is_some()
            || community.is_empty()
            || community.len() > 128
            || account.is_nil()
            || root.len() > 16384
        {
            return Err(Error::InvalidInput);
        }
        let origin = u.as_str().to_owned();
        let mut b = Self::ephemeral()?;
        b.root = Some(root);
        b.fixture_scope = Some((origin, community, account));
        Ok(b)
    }
    pub fn remembered_login_supported(&self) -> bool {
        false
    }
    pub fn status(&self) -> Result<Status, Error> {
        let s = self.inner.state.lock().map_err(|_| Error::Internal)?;
        Ok(s.active.as_ref().map(|a| a.status).unwrap_or(Status::Empty))
    }
    /// Nonsecret comparison selector for actual selected profile. It conveys no
    /// authentication, channel membership or cryptographic readiness.
    pub fn selected_profile_identity(&self, owner: WindowOwner) -> Result<Uuid, Error> {
        let state = self.inner.state.lock().map_err(|_| Error::Internal)?;
        state.main(self.inner.owner, owner)?;
        let active = state.active.as_ref().ok_or(Error::NoProfile)?;
        // Selection identity is comparison data, independent of authentication.
        // A rejected login cancels old auth operations but does not change the
        // selected origin. Login owns a fresh auth epoch/cancel token on retry.
        state
            .profiles
            .profile(active.handle)
            .map_err(|_| Error::Stale)?;
        Ok(active.native_identity)
    }
    /// Must be called by a future native-created-window registry, never a renderer command.
    pub fn register_native_window(
        &self,
        label: &str,
        role: WindowRole,
    ) -> Result<WindowOwner, Error> {
        if label.is_empty()
            || label.len() > 32
            || !label
                .bytes()
                .all(|b| b.is_ascii_alphanumeric() || b == b'-' || b == b'_')
        {
            return Err(Error::InvalidInput);
        }
        let mut s = self.inner.state.lock().map_err(|_| Error::Internal)?;
        let old = s
            .windows
            .iter()
            .filter_map(|(id, w)| (w.label == label).then_some(*id))
            .collect::<Vec<_>>();
        for id in old {
            if let Some(w) = s.windows.remove(&id) {
                w.cancel.cancel();
            }
            s.cancel_requests(Some(WindowOwner {
                broker: self.inner.owner,
                incarnation: id,
            }));
        }
        s.next_window = s.next_window.checked_add(1).ok_or(Error::Exhausted)?;
        let owner = WindowOwner {
            broker: self.inner.owner,
            incarnation: s.next_window,
        };
        s.windows.insert(
            owner.incarnation,
            Window {
                native_identity: Uuid::new_v4(),
                label: label.into(),
                role,
                cancel: CancellationToken::new(),
            },
        );
        self.inner.idle.notify_waiters();
        Ok(owner)
    }
    pub fn destroy_native_window(&self, owner: WindowOwner) -> Result<(), Error> {
        let mut s = self.inner.state.lock().map_err(|_| Error::Internal)?;
        s.window(self.inner.owner, owner)?;
        if let Some(w) = s.windows.remove(&owner.incarnation) {
            w.cancel.cancel();
        }
        s.cancel_requests(Some(owner));
        self.inner.idle.notify_waiters();
        Ok(())
    }
    async fn drain(&self, except: Option<u64>) -> Result<(), Error> {
        loop {
            let changed = self.inner.idle.notified();
            tokio::pin!(changed);
            changed.as_mut().enable();
            if self
                .inner
                .state
                .lock()
                .map_err(|_| Error::Internal)?
                .requests
                .iter()
                .all(|(id, r)| r.phase != RequestPhase::Running || Some(*id) == except)
            {
                return Ok(());
            }
            changed.await;
        }
    }
    pub async fn select_confirmed_profile(
        &self,
        owner: WindowOwner,
        address: &str,
        community: &str,
    ) -> Result<(), Error> {
        #[cfg(feature = "synthetic-transport-fixture")]
        if let Some((fixed, cid, _)) = &self.fixture_scope {
            let selected = mnema_desktop_probe::discovery::normalize_address(address)
                .map_err(|_| Error::InvalidInput)?;
            if selected.as_str() != fixed || community != cid {
                return Err(Error::Denied);
            }
        }
        let intent = {
            let mut s = self.inner.state.lock().map_err(|_| Error::Internal)?;
            s.main(self.inner.owner, owner)?;
            if let Some(active) = s.active.take() {
                active.cancel.cancel();
                if let Some(flight) = active.flight {
                    flight.finish(Err(Error::Stale));
                }
            }
            s.cancel_requests(None);
            s.profiles.teardown().map_err(|_| Error::Exhausted)?;
            s.selection = s.selection.checked_add(1).ok_or(Error::Exhausted)?;
            s.selection
        };
        self.inner.idle.notify_waiters();
        self.drain(None).await?;
        let http = {
            #[cfg(any(test, feature = "synthetic-transport-fixture"))]
            {
                match &self.root {
                    Some(root) => Http::local_fixture(root)?,
                    None => Http::new()?,
                }
            }
            #[cfg(not(any(test, feature = "synthetic-transport-fixture")))]
            {
                Http::new()?
            }
        };
        let mut s = self.inner.state.lock().map_err(|_| Error::Internal)?;
        s.main(self.inner.owner, owner)?;
        if s.selection != intent {
            return Err(Error::Stale);
        }
        self.memory.clear().map_err(|_| Error::Vault)?;
        let handle = s
            .profiles
            .select(address, community)
            .map_err(|_| Error::InvalidInput)?;
        let selected = s.profiles.profile(handle).map_err(|_| Error::Stale)?;
        let origin = selected.origin().clone();
        s.active = Some(Active {
            native_identity: Uuid::new_v4(),
            handle,
            epoch: 0,
            origin,
            http,
            cancel: CancellationToken::new(),
            status: Status::Selected,
            session: None,
            flight: None,
        });
        Ok(())
    }
    fn reserve(
        &self,
        s: &mut State,
        owner: WindowOwner,
        phase: RequestPhase,
    ) -> Result<Operation, Error> {
        let window = s.main(self.inner.owner, owner)?;
        let window_cancel = window.cancel.clone();
        if s.requests.len() >= 16 {
            return Err(Error::Busy);
        }
        let active = s.active.as_ref().ok_or(Error::NoProfile)?;
        let profile = s
            .profiles
            .begin_operation(active.handle)
            .map_err(|_| Error::Stale)?;
        let id = s.next_request.checked_add(1).ok_or(Error::Exhausted)?;
        let cancel = CancellationToken::new();
        let op = Operation {
            auth_revision: active.session.as_ref().map(|g| g.native_identity),
            window: owner,
            profile,
            epoch: active.epoch,
            id,
            http: active.http.clone(),
            origin: active.origin.clone(),
            grant: active.session.as_ref().map(|g| g.grant.clone()),
            cancel: cancel.clone(),
            scope_cancel: active.cancel.clone(),
            window_cancel,
            _guard: RequestGuard {
                inner: self.inner.clone(),
                id,
            },
        };
        s.next_request = id;
        s.requests.insert(
            id,
            RequestEntry {
                window: owner,
                cancel,
                phase,
            },
        );
        Ok(op)
    }
    pub fn cancel(&self, owner: WindowOwner, handle: CancelHandle) -> Result<(), Error> {
        let s = self.inner.state.lock().map_err(|_| Error::Internal)?;
        s.window(self.inner.owner, owner)?;
        if owner != handle.window {
            return Err(Error::Denied);
        }
        let entry = s.requests.get(&handle.id).ok_or(Error::Stale)?;
        if entry.window != owner {
            return Err(Error::Denied);
        }
        entry.cancel.cancel();
        Ok(())
    }
    fn verify(&self, op: &Operation) -> Result<(), Error> {
        self.inner
            .state
            .lock()
            .map_err(|_| Error::Internal)?
            .current(self.inner.owner, op)
    }
    async fn send(
        &self,
        op: &Operation,
        endpoint: Endpoint,
        access: Option<&crate::secret::Secret>,
        body: Option<Zeroizing<Vec<u8>>>,
    ) -> Result<crate::http::Body, Error> {
        self.verify(op)?;
        if matches!(endpoint, Endpoint::Me | Endpoint::Logout)
            && op
                .grant
                .as_ref()
                .is_some_and(|g| g.access_expires_at <= Utc::now())
        {
            return Err(Error::Expired);
        }
        let result = tokio::select! {biased;_=op.cancelled()=>Err(self.verify(op).err().unwrap_or(Error::Cancelled)),result=op.http.request(endpoint,&op.origin,access,body)=>result};
        self.verify(op)?;
        if matches!(endpoint, Endpoint::Me | Endpoint::Logout)
            && op
                .grant
                .as_ref()
                .is_some_and(|g| g.access_expires_at <= Utc::now())
        {
            return Err(Error::Expired);
        }
        result
    }
    pub async fn login_delivery(
        &self,
        owner: WindowOwner,
        username: &str,
        password: Password,
    ) -> Result<LoginDelivery, Error> {
        if username.len() < 3
            || username.len() > 32
            || !username
                .bytes()
                .all(|b| b.is_ascii_alphanumeric() || b"_.-".contains(&b))
        {
            return Err(Error::InvalidInput);
        }
        let (op, instance) = {
            let mut s = self.inner.state.lock().map_err(|_| Error::Internal)?;
            s.main(self.inner.owner, owner)?;
            let active = s.active.as_ref().ok_or(Error::NoProfile)?;
            if !matches!(active.status, Status::Selected | Status::ReauthRequired) {
                return Err(Error::Busy);
            }
            s.cancel_requests(None);
            let active = s.active.as_mut().unwrap();
            active.cancel.cancel();
            active.cancel = CancellationToken::new();
            active.status = Status::ReauthRequired;
            active.session = None;
            active.epoch = active.epoch.checked_add(1).ok_or(Error::Exhausted)?;
            let op = self.reserve(&mut s, owner, RequestPhase::Running)?;
            s.active.as_mut().unwrap().status = Status::SigningIn;
            (op, Uuid::new_v4())
        };
        let mut completion = LoginGuard {
            inner: self.inner.clone(),
            profile: op.profile,
            epoch: op.epoch,
            armed: true,
        };
        self.drain(Some(op.id)).await?;
        self.verify(&op)?;
        self.memory.clear().map_err(|_| Error::Vault)?;
        #[derive(serde::Serialize)]
        struct LoginWire<'a> {
            username: &'a str,
            password: &'a str,
            client_instance_id: String,
        }
        let bytes = Zeroizing::new(
            serde_json::to_vec(&LoginWire {
                username,
                password: password.text(),
                client_instance_id: instance.hyphenated().to_string(),
            })
            .map_err(|_| Error::Internal)?,
        );
        drop(password);
        let body = self.send(&op, Endpoint::Login, None, Some(bytes)).await?;
        let grant: Grant = body.decode()?;
        grant.validate(instance)?;
        #[cfg(feature = "synthetic-transport-fixture")]
        if self
            .fixture_scope
            .as_ref()
            .is_some_and(|(_, _, account)| *account != grant.user.account_id())
        {
            return Err(Error::Protocol);
        }
        if grant.refresh_sequence != 0 {
            return Err(Error::Protocol);
        }
        let access_deadline = authenticated_scope::admission_deadline(&grant)?;
        let user = grant.user.clone();
        let mut s = self.inner.state.lock().map_err(|_| Error::Internal)?;
        s.current(self.inner.owner, &op)?;
        let community = s
            .profiles
            .operation_profile(op.profile)
            .map_err(|_| Error::Stale)?
            .community_id();
        let namespace = Namespace::checked(
            op.origin.as_str(),
            "/",
            community,
            *grant.user.id.0.as_bytes(),
            *instance.as_bytes(),
        )
        .map_err(|_| Error::Protocol)?;
        let record = Record::fresh(
            &namespace,
            *grant.family_id.0.as_bytes(),
            0,
            Refresh::from_native_bytes(grant.refresh_token.bytes()).map_err(|_| Error::Protocol)?,
        )
        .map_err(|_| Error::Vault)?;
        self.journal
            .own(namespace.clone())
            .map_err(|_| Error::Vault)?
            .install_new(record)
            .map_err(|_| Error::Vault)?;
        let active = s.active.as_mut().unwrap();
        active.session = Some(Session {
            accepted_role: grant.user.role.clone(),
            native_identity: Uuid::new_v4(),
            access_deadline,
            grant: Arc::new(grant),
            namespace,
        });
        active.status = Status::Authenticated;
        completion.armed = false;
        drop(s);
        Ok(LoginDelivery {
            scope: Self::scope_from_operation(&op),
            value: user,
        })
    }
    pub fn commit_login(&self, owner: WindowOwner, delivery: LoginDelivery) -> Result<User, Error> {
        self.check_publication_scope(owner, delivery.scope)?;
        let s = self.inner.state.lock().map_err(|_| Error::Internal)?;
        let a = s.active.as_ref().ok_or(Error::Stale)?;
        if a.status != Status::Authenticated
            || a.session.as_ref().is_none_or(|g| {
                g.grant.user.id != delivery.value.id || g.grant.access_expires_at <= Utc::now()
            })
        {
            return Err(Error::Stale);
        }
        // Repeat the scope comparison under the user/session check lock.
        if !s.profiles.is_current(delivery.scope.profile) || a.epoch != delivery.scope.epoch {
            return Err(Error::Stale);
        }
        Ok(delivery.value)
    }
    pub async fn login(
        &self,
        owner: WindowOwner,
        username: &str,
        password: Password,
    ) -> Result<User, Error> {
        let delivery = self.login_delivery(owner, username, password).await?;
        self.commit_login(owner, delivery)
    }
    pub fn prepare_me(&self, owner: WindowOwner) -> Result<Operation, Error> {
        let mut s = self.inner.state.lock().map_err(|_| Error::Internal)?;
        s.main(self.inner.owner, owner)?;
        let active = s.active.as_ref().ok_or(Error::NoProfile)?;
        if active.status != Status::Authenticated {
            return Err(if active.status == Status::Rotating {
                Error::Busy
            } else {
                Error::ReauthRequired
            });
        }
        if active
            .session
            .as_ref()
            .is_none_or(|session| session.grant.access_expires_at <= Utc::now())
        {
            return Err(Error::Expired);
        }
        self.reserve(&mut s, owner, RequestPhase::Prepared)
    }
    pub async fn execute_me(&self, mut op: Operation) -> Result<MeDelivery, Error> {
        {
            let mut s = self.inner.state.lock().map_err(|_| Error::Internal)?;
            s.current(self.inner.owner, &op)?;
            s.requests.get_mut(&op.id).ok_or(Error::Stale)?.phase = RequestPhase::Running;
        }
        let grant = op.grant.as_ref().ok_or(Error::Unauthorized)?;
        if grant.access_expires_at <= Utc::now() {
            return Err(Error::Expired);
        }
        let body = match self
            .send(&op, Endpoint::Me, Some(&grant.access_token), None)
            .await
        {
            Err(Error::Unauthorized) => {
                let mut s = self.inner.state.lock().map_err(|_| Error::Internal)?;
                s.current(self.inner.owner, &op)?;
                s.cancel_requests(None);
                if let Some(active) = s.active.as_mut() {
                    active.cancel.cancel();
                    active.status = Status::ReauthRequired;
                    active.session = None;
                    if let Some(flight) = active.flight.take() {
                        flight.finish(Err(Error::Unauthorized));
                    }
                }
                return Err(Error::Unauthorized);
            }
            result => result?,
        };
        let user: User = body.decode()?;
        user.validate()?;
        if user.id != grant.user.id {
            return Err(Error::Protocol);
        }
        {
            let mut s = self.inner.state.lock().map_err(|_| Error::Internal)?;
            s.current(self.inner.owner, &op)?;
            let revision = s
                .active
                .as_ref()
                .and_then(|a| a.session.as_ref())
                .map(|session| session.native_identity)
                .ok_or(Error::ReauthRequired)?;
            if op.auth_revision != Some(revision) {
                return Err(Error::Stale);
            }
            s.requests.get_mut(&op.id).ok_or(Error::Stale)?.phase = RequestPhase::Delivered;
            // A verified same-account /me can supersede an old grant role.
            // Retire native capabilities before delivering a downgraded reply.
            let session = s
                .active
                .as_mut()
                .and_then(|a| a.session.as_mut())
                .ok_or(Error::ReauthRequired)?;
            if session.accepted_role != user.role {
                session.accepted_role = user.role.clone();
                session.native_identity = Uuid::new_v4();
            }
            // This delivery carries the revision it installed. Other older
            // prepared/running/delivered /me results cannot undo its downgrade.
            op.auth_revision = Some(session.native_identity);
        }
        Ok(MeDelivery {
            operation: op,
            value: user,
        })
    }
    pub fn commit_me(&self, owner: WindowOwner, delivery: MeDelivery) -> Result<User, Error> {
        {
            let s = self.inner.state.lock().map_err(|_| Error::Internal)?;
            if owner != delivery.operation.window {
                return Err(Error::Denied);
            }
            s.current(self.inner.owner, &delivery.operation)?;
            if delivery.operation.auth_revision
                != s.active
                    .as_ref()
                    .and_then(|a| a.session.as_ref())
                    .map(|session| session.native_identity)
            {
                return Err(Error::Stale);
            }
            if delivery
                .operation
                .grant
                .as_ref()
                .is_some_and(|g| g.access_expires_at <= Utc::now())
            {
                return Err(Error::Expired);
            }
        }
        Ok(delivery.value)
    }
    pub async fn me(&self, owner: WindowOwner) -> Result<User, Error> {
        let op = self.prepare_me(owner)?;
        let delivery = self.execute_me(op).await?;
        self.commit_me(owner, delivery)
    }
    pub async fn refresh(&self, owner: WindowOwner) -> Result<(), Error> {
        let (op, old, namespace, flight, leader) = {
            let mut s = self.inner.state.lock().map_err(|_| Error::Internal)?;
            s.main(self.inner.owner, owner)?;
            let active = s.active.as_ref().ok_or(Error::NoProfile)?;
            if !matches!(active.status, Status::Authenticated | Status::Rotating) {
                return Err(Error::ReauthRequired);
            }
            let session = active.session.as_ref().ok_or(Error::ReauthRequired)?;
            let old = session.grant.clone();
            let namespace = session.namespace.clone();
            let existing = active.flight.clone();
            let op = self.reserve(&mut s, owner, RequestPhase::Running)?;
            let leader = existing.is_none();
            let flight = existing.unwrap_or_else(|| Arc::new(Flight::new()));
            if leader {
                let active = s.active.as_mut().unwrap();
                active.status = Status::Rotating;
                active.flight = Some(flight.clone());
            }
            (op, old, namespace, flight, leader)
        };
        if !leader {
            loop {
                let changed = flight.changed.notified();
                tokio::pin!(changed);
                changed.as_mut().enable();
                if let Some(result) = flight.result() {
                    let epoch = result?;
                    let s = self.inner.state.lock().map_err(|_| Error::Internal)?;
                    s.main(self.inner.owner, owner)?;
                    if op.cancel.is_cancelled() {
                        return Err(Error::Cancelled);
                    }
                    if !s.profiles.is_current(op.profile)
                        || s.active
                            .as_ref()
                            .is_none_or(|a| a.epoch != epoch || a.status != Status::Authenticated)
                    {
                        return Err(Error::Stale);
                    }
                    return Ok(());
                }
                tokio::select! {_=changed=>{},_=op.cancelled()=>{if flight.result().is_none(){return Err(self.verify(&op).err().unwrap_or(Error::Cancelled))}}}
            }
        }
        let mut completion = RotationGuard {
            inner: self.inner.clone(),
            profile: op.profile,
            epoch: op.epoch,
            flight: flight.clone(),
            armed: true,
        };
        let mut owned = self
            .journal
            .own(namespace.clone())
            .map_err(|_| Error::Vault)?;
        let previous = owned
            .begin_rotation(*old.family_id.0.as_bytes(), old.refresh_sequence, op.id)
            .map_err(|_| Error::Vault)?;
        if !previous.with_native_refresh(|bytes| *bytes == old.refresh_token.bytes()) {
            return Err(Error::Vault);
        }
        let wire = old.refresh_token.wire();
        let mut bytes = Zeroizing::new(Vec::with_capacity(65));
        bytes.extend_from_slice(b"{\"refresh_token\":\"");
        bytes.extend_from_slice(wire.as_bytes());
        bytes.extend_from_slice(b"\"}");
        drop(wire);
        let body = self
            .send(&op, Endpoint::Refresh, None, Some(bytes))
            .await
            .map_err(|error| {
                if matches!(error, Error::Stale | Error::Cancelled) {
                    error
                } else {
                    Error::UncertainRotation
                }
            })?;
        let successor: Grant = body.decode().map_err(|_| Error::UncertainRotation)?;
        successor
            .successor_of(&old)
            .map_err(|_| Error::UncertainRotation)?;
        let access_deadline = authenticated_scope::admission_deadline(&successor)
            .map_err(|_| Error::UncertainRotation)?;
        let next_record = Record::fresh(
            &namespace,
            *successor.family_id.0.as_bytes(),
            successor.refresh_sequence,
            Refresh::from_native_bytes(successor.refresh_token.bytes())
                .map_err(|_| Error::UncertainRotation)?,
        )
        .map_err(|_| Error::Vault)?;
        let mut s = self.inner.state.lock().map_err(|_| Error::Internal)?;
        s.current(self.inner.owner, &op)?;
        // An accepted /me role change after refresh dispatch retires that
        // request's authority revision. Never admit its older role snapshot.
        if op.auth_revision
            != s.active
                .as_ref()
                .and_then(|a| a.session.as_ref())
                .map(|session| session.native_identity)
        {
            return Err(Error::UncertainRotation);
        }
        let next_epoch = s
            .active
            .as_ref()
            .unwrap()
            .epoch
            .checked_add(1)
            .ok_or(Error::Exhausted)?;
        owned
            .commit_successor(&previous, next_record)
            .map_err(|_| Error::UncertainRotation)?;
        let active = s.active.as_mut().unwrap();
        active.cancel.cancel();
        active.cancel = CancellationToken::new();
        active.epoch = next_epoch;
        active.session = Some(Session {
            accepted_role: successor.user.role.clone(),
            native_identity: Uuid::new_v4(),
            access_deadline,
            grant: Arc::new(successor),
            namespace,
        });
        active.status = Status::Authenticated;
        active.flight = None;
        completion.armed = false;
        flight.finish(Ok(next_epoch));
        drop(s);
        Ok(())
    }
    pub async fn logout_delivery(&self, owner: WindowOwner) -> Result<LogoutDelivery, Error> {
        let (op, old) = {
            let mut s = self.inner.state.lock().map_err(|_| Error::Internal)?;
            s.main(self.inner.owner, owner)?;
            let active = s.active.as_ref().ok_or(Error::NoProfile)?;
            let old = active
                .session
                .as_ref()
                .ok_or(Error::ReauthRequired)?
                .grant
                .clone();
            s.cancel_requests(None);
            let active = s.active.as_mut().unwrap();
            active.cancel.cancel();
            active.cancel = CancellationToken::new();
            active.status = Status::ReauthRequired;
            active.session = None;
            if let Some(f) = active.flight.take() {
                f.finish(Err(Error::Stale))
            }
            active.epoch = active.epoch.checked_add(1).ok_or(Error::Exhausted)?;
            let mut op = self.reserve(&mut s, owner, RequestPhase::Running)?;
            op.grant = Some(old.clone());
            (op, old)
        };
        self.drain(Some(op.id)).await?;
        self.verify(&op)?;
        self.memory.clear().map_err(|_| Error::Vault)?;
        self.send(&op, Endpoint::Logout, Some(&old.access_token), None)
            .await?;
        Ok(LogoutDelivery {
            scope: Self::scope_from_operation(&op),
        })
    }
    pub fn commit_logout(&self, owner: WindowOwner, delivery: LogoutDelivery) -> Result<(), Error> {
        self.check_publication_scope(owner, delivery.scope)
    }
    pub async fn logout(&self, owner: WindowOwner) -> Result<(), Error> {
        let delivery = self.logout_delivery(owner).await?;
        self.commit_logout(owner, delivery)
    }
}

#[cfg(test)]
#[path = "tests.rs"]
mod tests;

#[path = "socket.rs"]
mod socket;
pub use socket::{MetadataEvent, NativeSocket, NativeSocketAction};

#[path = "client.rs"]
mod client;
pub use client::{
    ConnectedPreview, NativeClient, NativePublication, NativeReply, NativeRequest,
    NativeSocketNotice, NativeSocketObserver, NativeWindowLease,
};

#[path = "metadata.rs"]
mod metadata;
pub use metadata::{MetadataDelivery, MetadataResource, PublicMetadataResource};

#[path = "authenticated_scope.rs"]
mod authenticated_scope;
pub use authenticated_scope::NativeAuthenticatedScope;
#[path = "personal_metadata.rs"]
mod personal_metadata;
pub use personal_metadata::{MetadataLocale, MetadataPresence, PersonalMetadataOperation};
#[path = "opaque_relay.rs"]
mod opaque_relay;
pub use opaque_relay::{
    NativeOpaqueDelivery, NativeOpaqueEvent, NativeOpaqueReceipt, NativeOpaqueRecord,
    NativeOpaqueRelayOperation,
};
