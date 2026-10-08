//! Dedicated current-thread LocalSet worker and opaque native publication.
//! A Tauri adapter must mint/bind leases from real native windows, not IPC labels.
use super::*;
use serde::{Deserialize, Serialize};
use std::sync::atomic::AtomicU64;
use tokio::sync::{Semaphore, mpsc, oneshot};

#[derive(Clone)]
pub struct NativeWindowLease {
    owner: WindowOwner,
    context: Uuid,
    cancel: CancellationToken,
}
impl fmt::Debug for NativeWindowLease {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("NativeWindowLease(REDACTED)")
    }
}
impl NativeWindowLease {
    pub fn context_nonce(&self) -> String {
        self.context.hyphenated().to_string()
    }
}
pub enum NativeRequest {
    Connect {
        address: String,
    },
    Login {
        username: String,
        password: Password,
    },
    Register {
        username: String,
        display_name: String,
        password: Password,
        invite_code: Zeroizing<String>,
    },
    Password {
        current_password: Password,
        new_password: Password,
    },
    Me,
    Metadata {
        resource: MetadataResource,
    },
    PublicMetadata {
        resource: PublicMetadataResource,
    },
    PersonalMetadata {
        operation: PersonalMetadataOperation,
    },
    AdminMetadata {
        operation: AdminMetadataOperation,
    },
    OpaqueRelay {
        scope: Arc<NativeAuthenticatedScope>,
        operation: NativeOpaqueRelayOperation,
    },
    Refresh,
    Logout,
    OpenSocket {
        observer: NativeSocketObserver,
    },
}
impl fmt::Debug for NativeRequest {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("NativeRequest(REDACTED)")
    }
}
#[derive(Serialize)]
pub struct ConnectedPreview {
    pub profile_intent: String,
    pub origin: String,
    pub community_id: String,
    pub compatibility: String,
    pub transport_preview: bool,
    pub content_authorization: String,
}
impl fmt::Debug for ConnectedPreview {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("ConnectedPreview(REDACTED)")
    }
}
enum Value {
    Connected(PublicationScope, ConnectedPreview),
    Login(LoginDelivery),
    Password(LoginDelivery),
    Register(RegistrationDelivery),
    Admin(AdminDelivery),
    Me(MeDelivery),
    Metadata(MetadataDelivery),
    OpaqueRelay(NativeOpaqueDelivery),
    Refresh(PublicationScope),
    Logout(LogoutDelivery),
    SocketOpened(SocketOpened),
}
pub struct NativePublication {
    lease: NativeWindowLease,
    intent: u64,
    value: Value,
}
impl fmt::Debug for NativePublication {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("NativePublication(REDACTED)")
    }
}
impl NativePublication {
    /// Native Core inspection only; no Serde projection or renderer publication.
    pub fn opaque_relay_receipt(&self) -> Option<&NativeOpaqueReceipt> {
        match &self.value {
            Value::OpaqueRelay(delivery) => Some(delivery.receipt()),
            _ => None,
        }
    }
}
#[derive(Serialize)]
pub struct NativeReply {
    pub context: String,
    pub status: u16,
    pub body: serde_json::Value,
}
impl fmt::Debug for NativeReply {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("NativeReply(REDACTED)")
    }
}
struct Job {
    request_id: Uuid,
    broker: Option<Arc<Broker>>,
    lease: NativeWindowLease,
    intent: u64,
    request: NativeRequest,
    answer: oneshot::Sender<Result<NativePublication, Error>>,
    cancel: CancellationToken,
}
struct Shared {
    broker: Arc<Broker>,
    window: Mutex<Option<NativeWindowLease>>,
    intent: AtomicU64,
    ready: AtomicU64,
    requests: Mutex<HashMap<(Uuid, Uuid), SubmittedRequest>>,
    sockets: Mutex<HashMap<Uuid, SocketControl>>,
    #[cfg(test)]
    custody_before_drop: Mutex<Option<(std::sync::mpsc::Sender<()>, Arc<std::sync::Barrier>)>>,
    #[cfg(test)]
    connect_before_mutation: Mutex<Option<(std::sync::mpsc::Sender<()>, Arc<std::sync::Barrier>)>>,
    #[cfg(test)]
    before_prepare: Mutex<Option<(oneshot::Sender<()>, oneshot::Receiver<()>)>>,
}
struct SubmittedRequest {
    cancel: CancellationToken,
    profile: Option<Option<Uuid>>,
    authentication: Option<Option<Uuid>>,
}
struct Submission {
    shared: Arc<Shared>,
    key: (Uuid, Uuid),
}
impl Drop for Submission {
    fn drop(&mut self) {
        if let Ok(mut requests) = self.shared.requests.lock() {
            requests.remove(&self.key);
        }
    }
}
#[derive(Clone)]
pub struct NativeClient {
    shared: Arc<Shared>,
    sender: mpsc::Sender<Job>,
}
impl fmt::Debug for NativeClient {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("NativeClient(REDACTED)")
    }
}
impl Shared {
    fn current(&self, lease: &NativeWindowLease) -> Result<(), Error> {
        if lease.cancel.is_cancelled() {
            return Err(Error::Stale);
        }
        let w = self.window.lock().map_err(|_| Error::Internal)?;
        if w.as_ref()
            .is_none_or(|w| w.context != lease.context || w.owner != lease.owner)
        {
            return Err(Error::Stale);
        }
        Ok(())
    }
    fn ready(&self, lease: &NativeWindowLease, intent: u64) -> Result<(), Error> {
        self.current(lease)?;
        if intent == 0
            || self.intent.load(Ordering::Acquire) != intent
            || self.ready.load(Ordering::Acquire) != intent
        {
            return Err(Error::Stale);
        }
        Ok(())
    }
}
impl NativeClient {
    /// The trusted native Core adapter supplies a current native scope and its
    /// exact durable outbox. Transport does not grant cryptographic admission.
    pub async fn request_opaque_relay(
        &self,
        lease: &NativeWindowLease,
        scope: NativeAuthenticatedScope,
        operation: NativeOpaqueRelayOperation,
    ) -> Result<NativePublication, Error> {
        self.request_opaque_relay_retained(lease, Arc::new(scope), operation)
            .await
    }
    /// Exact admitted native scope allocation follows the job through actual
    /// HTTP and the final receipt enqueue; no authority/deadline reminting.
    pub async fn request_opaque_relay_retained(
        &self,
        lease: &NativeWindowLease,
        scope: Arc<NativeAuthenticatedScope>,
        operation: NativeOpaqueRelayOperation,
    ) -> Result<NativePublication, Error> {
        self.check_authenticated_scope(lease, &scope)?;
        self.request(lease, NativeRequest::OpaqueRelay { scope, operation })
            .await
    }
    /// Final native enqueue, after actual SDK proof/inner author verification.
    /// Outer Tauri registry must also keep its native window authority locked.
    pub fn with_opaque_relay_publication<T>(
        &self,
        lease: &NativeWindowLease,
        publication: NativePublication,
        publish: impl FnOnce(&NativeOpaqueReceipt) -> T,
    ) -> Result<T, Error> {
        let windows = self.shared.window.lock().map_err(|_| Error::Internal)?;
        if lease.cancel.is_cancelled()
            || windows
                .as_ref()
                .is_none_or(|w| w.context != lease.context || w.owner != lease.owner)
        {
            return Err(Error::Stale);
        }
        let intent = self.shared.intent.load(Ordering::Acquire);
        if intent == 0
            || self.shared.ready.load(Ordering::Acquire) != intent
            || publication.intent != intent
        {
            return Err(Error::Stale);
        }
        if publication.lease.context != lease.context || publication.lease.owner != lease.owner {
            return Err(Error::Denied);
        }
        let Value::OpaqueRelay(delivery) = publication.value else {
            return Err(Error::Denied);
        };
        if delivery.native_binding() != Some((lease.context, intent)) {
            return Err(Error::Denied);
        }
        self.shared
            .broker
            .with_opaque_relay_publication(lease.owner, delivery, publish)
    }
    #[cfg(test)]
    pub(crate) fn install_custody_drop_barrier(
        &self,
        start: std::sync::mpsc::Sender<()>,
        barrier: Arc<std::sync::Barrier>,
    ) {
        *self.shared.custody_before_drop.lock().unwrap() = Some((start, barrier));
    }
    #[cfg(test)]
    pub(crate) fn hold_next_prepare(&self) -> (oneshot::Receiver<()>, oneshot::Sender<()>) {
        let (started, observed) = oneshot::channel();
        let (release, wait) = oneshot::channel();
        *self.shared.before_prepare.lock().unwrap() = Some((started, wait));
        (observed, release)
    }
    pub fn ephemeral() -> Result<Self, Error> {
        Self::with_broker(Broker::ephemeral()?)
    }
    #[cfg(test)]
    pub(crate) fn fixture(root: Vec<u8>) -> Self {
        Self::with_broker(Broker::fixture(root)).unwrap()
    }
    #[cfg(feature = "synthetic-transport-fixture")]
    pub fn qualification_fixture(
        origin: String,
        community: String,
        account: Uuid,
        root: Vec<u8>,
    ) -> Result<Self, Error> {
        Self::with_broker(Broker::qualification_fixture(
            origin, community, account, root,
        )?)
    }
    fn with_broker(broker: Broker) -> Result<Self, Error> {
        let shared = Arc::new(Shared {
            broker: Arc::new(broker),
            window: Mutex::new(None),
            intent: AtomicU64::new(0),
            ready: AtomicU64::new(0),
            requests: Mutex::new(HashMap::new()),
            sockets: Mutex::new(HashMap::new()),
            #[cfg(test)]
            custody_before_drop: Mutex::new(None),
            #[cfg(test)]
            connect_before_mutation: Mutex::new(None),
            #[cfg(test)]
            before_prepare: Mutex::new(None),
        });
        let (sender, mut receive) = mpsc::channel::<Job>(16);
        let worker = shared.clone();
        std::thread::Builder::new().name("mnema-private-native-actor".to_owned()).spawn(move||{
            let Ok(runtime)=tokio::runtime::Builder::new_current_thread().enable_all().build() else{return};
            tokio::task::LocalSet::new().block_on(&runtime,async move{
                let budget=Arc::new(Semaphore::new(16));
                while let Some(mut job)=receive.recv().await {
                    let Ok(permit)=budget.clone().try_acquire_owned() else{let _=job.answer.send(Err(Error::Busy));continue};
                    let worker=worker.clone();
                    tokio::task::spawn_local(async move{
                        let result=tokio::select!{biased;_=job.lease.cancel.cancelled()=>Err(Error::Stale),_=job.cancel.cancelled()=>Err(Error::Cancelled),_=job.answer.closed()=>Err(Error::Cancelled),r=execute(&worker,job.lease.clone(),job.intent,job.request_id,job.broker,job.request)=>r};
                        let _=job.answer.send(result);drop(permit);
                    });
                }
            });
        }).map_err(|_|Error::Internal)?;
        Ok(Self { shared, sender })
    }
    /// Called by native lifecycle hooks only; there is exactly one main lease.
    pub fn attach_main_window(&self) -> Result<NativeWindowLease, Error> {
        let mut w = self.shared.window.lock().map_err(|_| Error::Internal)?;
        if let Some(old) = w.take() {
            old.cancel.cancel();
            let _ = self.shared.broker.destroy_native_window(old.owner);
        }
        self.shared.ready.store(0, Ordering::Release);
        self.shared.broker.invalidate_native_context()?;
        self.shared
            .intent
            .try_update(Ordering::AcqRel, Ordering::Acquire, |n| n.checked_add(1))
            .map_err(|_| Error::Exhausted)?;
        let lease = NativeWindowLease {
            owner: self
                .shared
                .broker
                .register_native_window("main", WindowRole::Main)?,
            context: Uuid::new_v4(),
            cancel: CancellationToken::new(),
        };
        *w = Some(lease.clone());
        Ok(lease)
    }
    pub fn detach_main_window(&self, lease: &NativeWindowLease) -> Result<(), Error> {
        let mut w = self.shared.window.lock().map_err(|_| Error::Internal)?;
        if w.as_ref().is_none_or(|w| w.context != lease.context) {
            return Err(Error::Stale);
        }
        lease.cancel.cancel();
        *w = None;
        self.shared.ready.store(0, Ordering::Release);
        self.shared.broker.invalidate_native_context()?;
        self.shared
            .intent
            .try_update(Ordering::AcqRel, Ordering::Acquire, |n| n.checked_add(1))
            .map_err(|_| Error::Exhausted)?;
        self.shared.broker.destroy_native_window(lease.owner)
    }
    pub fn check_window(&self, lease: &NativeWindowLease) -> Result<(), Error> {
        self.shared.current(lease)
    }
    /// Native registry supplies its real opaque window lease. No renderer
    /// command exposes scope construction or authenticated provenance.
    pub fn authenticated_scope(
        &self,
        lease: &NativeWindowLease,
    ) -> Result<NativeAuthenticatedScope, Error> {
        let windows = self.shared.window.lock().map_err(|_| Error::Internal)?;
        if lease.cancel.is_cancelled()
            || windows
                .as_ref()
                .is_none_or(|w| w.context != lease.context || w.owner != lease.owner)
        {
            return Err(Error::Stale);
        }
        let intent = self.shared.intent.load(Ordering::Acquire);
        if intent == 0 || self.shared.ready.load(Ordering::Acquire) != intent {
            return Err(Error::Stale);
        }
        let mut scope = self.shared.broker.authenticated_scope(lease.owner)?;
        scope.client_binding = Some((lease.context, intent));
        Ok(scope)
    }
    pub fn check_authenticated_scope(
        &self,
        lease: &NativeWindowLease,
        scope: &NativeAuthenticatedScope,
    ) -> Result<(), Error> {
        self.with_authenticated_publication(lease, scope, || ())
    }
    /// Hold native client ownership then broker state through bounded enqueue.
    /// Outer Tauri registry must retain its actual native-window identity lock;
    /// callback may not re-enter broker/client/registry or block on other locks.
    pub fn with_authenticated_publication<T>(
        &self,
        lease: &NativeWindowLease,
        scope: &NativeAuthenticatedScope,
        publish: impl FnOnce() -> T,
    ) -> Result<T, Error> {
        let windows = self.shared.window.lock().map_err(|_| Error::Internal)?;
        if lease.cancel.is_cancelled()
            || windows
                .as_ref()
                .is_none_or(|w| w.context != lease.context || w.owner != lease.owner)
        {
            return Err(Error::Stale);
        }
        let intent = self.shared.intent.load(Ordering::Acquire);
        if scope.client_binding != Some((lease.context, intent)) {
            return Err(Error::Denied);
        }
        if intent == 0 || self.shared.ready.load(Ordering::Acquire) != intent {
            return Err(Error::Stale);
        }
        self.shared
            .broker
            .with_authenticated_publication(lease.owner, scope, publish)
    }
    pub async fn request(
        &self,
        lease: &NativeWindowLease,
        request: NativeRequest,
    ) -> Result<NativePublication, Error> {
        self.request_with_id(lease, Uuid::new_v4(), request).await
    }
    pub async fn request_with_id(
        &self,
        lease: &NativeWindowLease,
        request_id: Uuid,
        request: NativeRequest,
    ) -> Result<NativePublication, Error> {
        self.request_inner(lease, request_id, None, None, None, request, || {})
            .await
    }
    /// Renderer-originated IPC must provide its captured native profile selector.
    /// Outer None is native-only; Some(None) means first/unselected Connect.
    pub async fn request_with_profile_id(
        &self,
        lease: &NativeWindowLease,
        request_id: Uuid,
        profile: Option<Uuid>,
        request: NativeRequest,
    ) -> Result<NativePublication, Error> {
        self.request_inner(lease, request_id, Some(profile), None, None, request, || {})
            .await
    }
    /// Bounded native custody hook runs only after exact admission, under the
    /// lifecycle lock. It must not reenter this client or perform I/O.
    pub async fn request_with_profile_admission(
        &self,
        lease: &NativeWindowLease,
        request_id: Uuid,
        profile: Option<Uuid>,
        request: NativeRequest,
        admit: impl FnOnce(),
    ) -> Result<NativePublication, Error> {
        self.request_inner(lease, request_id, Some(profile), None, None, request, admit)
            .await
    }
    #[allow(clippy::too_many_arguments)]
    pub async fn request_with_authentication_admission(
        &self,
        lease: &NativeWindowLease,
        request_id: Uuid,
        profile: Option<Uuid>,
        authentication: Option<Uuid>,
        request: NativeRequest,
        admit: impl FnOnce(),
    ) -> Result<NativePublication, Error> {
        self.request_inner(
            lease,
            request_id,
            Some(profile),
            Some(authentication),
            None,
            request,
            admit,
        )
        .await
    }
    /// Native Core metadata jobs retain the exact admitted allocation through
    /// deep prepare, rather than sampling a replacement grant after queueing.
    pub async fn request_retained_authenticated(
        &self,
        lease: &NativeWindowLease,
        scope: Arc<NativeAuthenticatedScope>,
        request_id: Uuid,
        request: NativeRequest,
    ) -> Result<NativePublication, Error> {
        if !matches!(&request, NativeRequest::Me | NativeRequest::Metadata { .. }) {
            return Err(Error::Denied);
        }
        self.request_inner(
            lease,
            request_id,
            Some(Some(scope.native_profile_identity())),
            Some(Some(scope.authentication_intent())),
            Some(scope),
            request,
            || {},
        )
        .await
    }
    pub fn selected_intents(
        &self,
        lease: &NativeWindowLease,
    ) -> Result<(Option<Uuid>, Option<Uuid>), Error> {
        let windows = self.shared.window.lock().map_err(|_| Error::Internal)?;
        if lease.cancel.is_cancelled()
            || windows
                .as_ref()
                .is_none_or(|window| window.context != lease.context || window.owner != lease.owner)
        {
            return Err(Error::Stale);
        }
        let intent = self.shared.intent.load(Ordering::Acquire);
        if intent == 0 || self.shared.ready.load(Ordering::Acquire) != intent {
            return Ok((None, None));
        }
        self.shared.broker.selected_intents(lease.owner)
    }
    pub fn authentication_intent(&self, lease: &NativeWindowLease) -> Result<Option<Uuid>, Error> {
        let windows = self.shared.window.lock().map_err(|_| Error::Internal)?;
        if lease.cancel.is_cancelled()
            || windows
                .as_ref()
                .is_none_or(|w| w.context != lease.context || w.owner != lease.owner)
        {
            return Err(Error::Stale);
        }
        self.shared.broker.authentication_intent(lease.owner)
    }
    pub fn authenticated_scope_with_intent(
        &self,
        lease: &NativeWindowLease,
        authentication: Uuid,
    ) -> Result<NativeAuthenticatedScope, Error> {
        let scope = self.authenticated_scope(lease)?;
        if scope.authentication_intent() != authentication {
            return Err(Error::Stale);
        }
        Ok(scope)
    }
    pub fn profile_intent(&self, lease: &NativeWindowLease) -> Result<Option<Uuid>, Error> {
        let window = self.shared.window.lock().map_err(|_| Error::Internal)?;
        if lease.cancel.is_cancelled()
            || window
                .as_ref()
                .is_none_or(|w| w.context != lease.context || w.owner != lease.owner)
        {
            return Err(Error::Stale);
        }
        let intent = self.shared.intent.load(Ordering::Acquire);
        if intent == 0 || self.shared.ready.load(Ordering::Acquire) != intent {
            return Ok(None);
        }
        self.shared
            .broker
            .selected_profile_identity(lease.owner)
            .map(Some)
    }
    pub fn check_profile_intent(
        &self,
        lease: &NativeWindowLease,
        profile: Uuid,
    ) -> Result<(), Error> {
        let window = self.shared.window.lock().map_err(|_| Error::Internal)?;
        if lease.cancel.is_cancelled()
            || window
                .as_ref()
                .is_none_or(|w| w.context != lease.context || w.owner != lease.owner)
        {
            return Err(Error::Stale);
        }
        let intent = self.shared.intent.load(Ordering::Acquire);
        if intent == 0
            || self.shared.ready.load(Ordering::Acquire) != intent
            || self.shared.broker.selected_profile_identity(lease.owner)? != profile
        {
            return Err(Error::Stale);
        }
        Ok(())
    }
    #[allow(clippy::too_many_arguments)]
    async fn request_inner(
        &self,
        lease: &NativeWindowLease,
        request_id: Uuid,
        profile_fence: Option<Option<Uuid>>,
        authentication_fence: Option<Option<Uuid>>,
        retained_scope: Option<Arc<NativeAuthenticatedScope>>,
        request: NativeRequest,
        admit: impl FnOnce(),
    ) -> Result<NativePublication, Error> {
        if request_id.is_nil() {
            return Err(Error::InvalidInput);
        }
        let (intent, bound_broker, cancel, _submission) = {
            // Window ownership validation and all synchronous Connect side effects
            // share the same native lifecycle lock. An old lease cannot clear a
            // recreated main window after a check/use gap.
            let window = self.shared.window.lock().map_err(|_| Error::Internal)?;
            if lease.cancel.is_cancelled()
                || window
                    .as_ref()
                    .is_none_or(|w| w.context != lease.context || w.owner != lease.owner)
            {
                return Err(Error::Stale);
            }
            if let Some(scope) = retained_scope.as_ref()
                && scope.client_binding
                    != Some((lease.context, self.shared.intent.load(Ordering::Acquire)))
            {
                return Err(Error::Denied);
            }
            if let Some(expected) = profile_fence {
                let intent = self.shared.intent.load(Ordering::Acquire);
                let ready = intent != 0 && self.shared.ready.load(Ordering::Acquire) == intent;
                match expected {
                    Some(profile)
                        if ready
                            && self.shared.broker.selected_profile_identity(lease.owner)?
                                == profile => {}
                    None if !ready && matches!(&request, NativeRequest::Connect { .. }) => {}
                    _ => return Err(Error::Stale),
                }
            }
            #[cfg(test)]
            if matches!(&request, NativeRequest::Connect { .. })
                && let Some((started, barrier)) =
                    self.shared.connect_before_mutation.lock().unwrap().take()
            {
                let _ = started.send(());
                barrier.wait();
            }
            let mut requests = self.shared.requests.lock().map_err(|_| Error::Internal)?;
            let key = (lease.context, request_id);
            if requests.len() >= 16 || requests.contains_key(&key) {
                return Err(Error::Busy);
            }
            let connecting = matches!(&request, NativeRequest::Connect { .. });
            let bound_broker = if let Some(expected) = authentication_fence {
                if connecting {
                    self.shared.broker.invalidate_with_authentication(
                        lease.owner,
                        expected,
                        admit,
                    )?;
                    None
                } else {
                    let kind = match &request {
                        NativeRequest::Login { .. } => InvocationKind::Login,
                        NativeRequest::Logout => InvocationKind::Logout,
                        NativeRequest::Password { .. } => InvocationKind::Password,
                        NativeRequest::Register { .. } => InvocationKind::Register,
                        _ => InvocationKind::Read,
                    };
                    let bound = if retained_scope.is_some() {
                        self.shared.broker.bind_invocation_retained(
                            lease.owner,
                            expected,
                            request_id,
                            kind,
                            retained_scope.clone(),
                        )?
                    } else {
                        self.shared.broker.bind_invocation(
                            lease.owner,
                            expected,
                            request_id,
                            kind,
                        )?
                    };
                    // Credential admission owns this native window lock. Seal
                    // only the admitted old socket now, never from delayed worker
                    // execution after its ticket could already be replaced.
                    if matches!(
                        kind,
                        InvocationKind::Login
                            | InvocationKind::Logout
                            | InvocationKind::Password
                            | InvocationKind::Register
                    ) {
                        actor_socket::cancel_sockets(&self.shared, lease.context);
                    }
                    admit();
                    Some(Arc::new(bound))
                }
            } else {
                admit();
                None
            };
            let intent = if connecting {
                self.shared.ready.store(0, Ordering::Release);
                if authentication_fence.is_none() {
                    self.shared.broker.invalidate_native_context()?;
                }
                self.shared
                    .intent
                    .try_update(Ordering::AcqRel, Ordering::Acquire, |n| n.checked_add(1))
                    .map_err(|_| Error::Exhausted)?
                    + 1
            } else {
                let n = self.shared.intent.load(Ordering::Acquire);
                if n == 0 || self.shared.ready.load(Ordering::Acquire) != n {
                    return Err(Error::NoProfile);
                }
                n
            };
            let cancel = CancellationToken::new();
            requests.insert(
                key,
                SubmittedRequest {
                    cancel: cancel.clone(),
                    profile: profile_fence,
                    authentication: authentication_fence,
                },
            );
            (
                intent,
                bound_broker,
                cancel,
                Submission {
                    shared: self.shared.clone(),
                    key,
                },
            )
        };
        let (answer, receive) = oneshot::channel();
        self.sender
            .try_send(Job {
                request_id,
                broker: bound_broker,
                lease: lease.clone(),
                intent,
                request,
                answer,
                cancel: cancel.clone(),
            })
            .map_err(|e| match e {
                mpsc::error::TrySendError::Full(_) => Error::Busy,
                mpsc::error::TrySendError::Closed(_) => Error::Internal,
            })?;
        tokio::select! {biased;_=lease.cancel.cancelled()=>Err(Error::Stale),_=cancel.cancelled()=>Err(Error::Cancelled),r=receive=>r.map_err(|_|Error::Internal)?}
    }
    pub fn cancel_request(&self, lease: &NativeWindowLease, request_id: Uuid) -> Result<(), Error> {
        let w = self.shared.window.lock().map_err(|_| Error::Internal)?;
        if lease.cancel.is_cancelled()
            || w.as_ref()
                .is_none_or(|w| w.context != lease.context || w.owner != lease.owner)
        {
            return Err(Error::Stale);
        }
        if let Some(cancel) = self
            .shared
            .requests
            .lock()
            .map_err(|_| Error::Internal)?
            .get(&(lease.context, request_id))
        {
            cancel.cancel.cancel();
        }
        Ok(())
    }
    pub fn cancel_request_with_intents(
        &self,
        lease: &NativeWindowLease,
        profile: Option<Uuid>,
        authentication: Option<Uuid>,
        request_id: Uuid,
    ) -> Result<(), Error> {
        let windows = self.shared.window.lock().map_err(|_| Error::Internal)?;
        if lease.cancel.is_cancelled()
            || windows
                .as_ref()
                .is_none_or(|w| w.context != lease.context || w.owner != lease.owner)
        {
            return Err(Error::Stale);
        }
        let requests = self.shared.requests.lock().map_err(|_| Error::Internal)?;
        if let Some(entry) = requests.get(&(lease.context, request_id)) {
            // Cancel the admitted original operation, including its pending
            // transition; never recapture the current replacement family.
            if entry.profile != Some(profile) || entry.authentication != Some(authentication) {
                return Err(Error::Stale);
            }
            entry.cancel.cancel();
        }
        Ok(())
    }
    pub fn with_selected_authentication<T>(
        &self,
        lease: &NativeWindowLease,
        profile: Uuid,
        authentication: Option<Uuid>,
        publish: impl FnOnce() -> T,
    ) -> Result<T, Error> {
        let windows = self.shared.window.lock().map_err(|_| Error::Internal)?;
        if lease.cancel.is_cancelled()
            || windows
                .as_ref()
                .is_none_or(|w| w.context != lease.context || w.owner != lease.owner)
        {
            return Err(Error::Stale);
        }
        let intent = self.shared.intent.load(Ordering::Acquire);
        if intent == 0
            || self.shared.ready.load(Ordering::Acquire) != intent
            || self.shared.broker.selected_profile_identity(lease.owner)? != profile
        {
            return Err(Error::Stale);
        }
        self.shared
            .broker
            .with_expected_authentication(lease.owner, authentication, publish)
    }
    pub fn disconnect_with_authentication_admission(
        &self,
        lease: &NativeWindowLease,
        profile: Option<Uuid>,
        authentication: Option<Uuid>,
        admit: impl FnOnce(),
    ) -> Result<(), Error> {
        self.disconnect_inner(lease, Some(profile), Some(authentication), admit)
    }
    pub fn disconnect(&self, lease: &NativeWindowLease) -> Result<(), Error> {
        self.disconnect_inner(lease, None, None, || {})
    }
    pub fn disconnect_with_profile_admission(
        &self,
        lease: &NativeWindowLease,
        profile: Option<Uuid>,
        admit: impl FnOnce(),
    ) -> Result<(), Error> {
        self.disconnect_inner(lease, Some(profile), None, admit)
    }
    fn disconnect_inner(
        &self,
        lease: &NativeWindowLease,
        profile_fence: Option<Option<Uuid>>,
        authentication_fence: Option<Option<Uuid>>,
        admit: impl FnOnce(),
    ) -> Result<(), Error> {
        let w = self.shared.window.lock().map_err(|_| Error::Internal)?;
        if lease.cancel.is_cancelled()
            || w.as_ref()
                .is_none_or(|w| w.context != lease.context || w.owner != lease.owner)
        {
            return Err(Error::Stale);
        }
        if let Some(expected) = profile_fence {
            let intent = self.shared.intent.load(Ordering::Acquire);
            let ready = intent != 0 && self.shared.ready.load(Ordering::Acquire) == intent;
            match expected {
                Some(profile)
                    if ready
                        && self.shared.broker.selected_profile_identity(lease.owner)?
                            == profile => {}
                None if !ready => {}
                _ => return Err(Error::Stale),
            }
        }
        if let Some(authentication) = authentication_fence {
            self.shared.broker.invalidate_with_authentication(
                lease.owner,
                authentication,
                admit,
            )?;
        } else {
            admit();
            self.shared.broker.invalidate_native_context()?;
        }
        self.shared.ready.store(0, Ordering::Release);
        self.shared
            .intent
            .try_update(Ordering::AcqRel, Ordering::Acquire, |n| n.checked_add(1))
            .map_err(|_| Error::Exhausted)?;
        Ok(())
    }
    pub fn commit(
        &self,
        lease: &NativeWindowLease,
        publication: NativePublication,
    ) -> Result<NativeReply, Error> {
        self.shared.ready(lease, publication.intent)?;
        if publication.lease.context != lease.context || publication.lease.owner != lease.owner {
            return Err(Error::Denied);
        }
        let b = &self.shared.broker;
        let (status, body) = match publication.value {
            Value::Connected(scope, value) => {
                b.check_publication_scope(lease.owner, scope)?;
                (
                    200,
                    serde_json::to_value(value).map_err(|_| Error::Internal)?,
                )
            }
            Value::Login(delivery) => {
                let authentication = delivery.authentication_identity;
                let user = b.commit_login(lease.owner, delivery)?;
                (
                    200,
                    serde_json::json!({"user":user_value(user),"authentication_intent":authentication.to_string()}),
                )
            }
            Value::Register(delivery) => (
                201,
                serde_json::json!({"user":user_value(b.commit_registration(lease.owner,delivery)?)}),
            ),
            Value::Password(delivery) => {
                let authentication = delivery.authentication_identity;
                b.commit_login(lease.owner, delivery)?;
                (
                    200,
                    serde_json::json!({"authentication_intent":authentication.to_string()}),
                )
            }
            Value::Admin(delivery) => {
                let response = b.commit_admin(lease.owner, delivery)?;
                (response.status, response.body)
            }
            Value::Me(delivery) => (200, user_value(b.commit_me(lease.owner, delivery)?)),
            Value::Metadata(delivery) => (200, b.commit_metadata(lease.owner, delivery)?),
            // Protected transport can never be serialized as a generic UI reply.
            Value::OpaqueRelay(_) => return Err(Error::QualificationRequired),
            Value::Refresh(scope) => {
                b.check_publication_scope(lease.owner, scope)?;
                (204, serde_json::Value::Null)
            }
            Value::SocketOpened(mut delivery) => {
                b.check_publication_scope(lease.owner, delivery.scope)?;
                self.shared.ready(lease, publication.intent)?;
                delivery.armed = false;
                (
                    200,
                    serde_json::json!({"handle":delivery.handle.hyphenated().to_string()}),
                )
            }
            Value::Logout(delivery) => {
                b.commit_logout(lease.owner, delivery)?;
                (204, serde_json::Value::Null)
            }
        };
        self.shared.ready(lease, publication.intent)?;
        Ok(NativeReply {
            context: lease.context_nonce(),
            status,
            body,
        })
    }
}
pub(super) fn user_value(user: User) -> serde_json::Value {
    let mut value = serde_json::json!({"id":user.account_id().hyphenated().to_string(),"username":user.username,"display_name":user.display_name,"bio":user.bio,"role":user.role,"avatar_url":"","status_text":user.status_text,"voice_seconds":user.voice_seconds,"message_count":user.message_count,"locale":user.locale,"created_at":user.created_at.to_rfc3339()});
    // Go's public member projection omits presence when it is not populated.
    // Preserve that absence; an empty presence is not a valid client enum.
    if !user.presence.is_empty() {
        value.as_object_mut().expect("user object").insert(
            "presence".to_owned(),
            serde_json::Value::String(user.presence),
        );
    }
    value
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct Descriptor {
    protocol: String,
    community_id: String,
    api_versions: Vec<u16>,
    e2ee_required: bool,
    native_api: NativeDescriptor,
}
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
struct NativeDescriptor {
    protocol: String,
    api_version: u16,
    prefix: String,
    compatibility: String,
    authentication: String,
    capabilities: Vec<String>,
    content_authorization: String,
}
async fn execute(
    shared: &Arc<Shared>,
    lease: NativeWindowLease,
    intent: u64,
    request_id: Uuid,
    bound_broker: Option<Arc<Broker>>,
    request: NativeRequest,
) -> Result<NativePublication, Error> {
    shared.current(&lease)?;
    if shared.intent.load(Ordering::Acquire) != intent {
        return Err(Error::Stale);
    }
    let b = bound_broker.as_ref().unwrap_or(&shared.broker);
    b.check_invocation_job(request_id)?;
    #[cfg(test)]
    {
        let gate = shared.before_prepare.lock().unwrap().take();
        if let Some((started, wait)) = gate {
            let _ = started.send(());
            wait.await.map_err(|_| Error::Stale)?;
        }
    }
    let value = match request {
        NativeRequest::Connect { address } => {
            let origin = mnema_desktop_probe::discovery::normalize_address(&address)
                .map_err(|_| Error::InvalidInput)?;
            #[cfg(feature = "synthetic-transport-fixture")]
            if b.fixture_scope
                .as_ref()
                .is_some_and(|(fixed, _, _)| origin.as_str() != fixed.as_str())
            {
                return Err(Error::Denied);
            }
            let http = {
                #[cfg(any(test, feature = "synthetic-transport-fixture"))]
                {
                    match &b.root {
                        Some(root) => Http::local_fixture(root)?,
                        None => Http::new()?,
                    }
                }
                #[cfg(not(any(test, feature = "synthetic-transport-fixture")))]
                {
                    Http::new()?
                }
            };
            let body = http
                .request(Endpoint::Discovery, &origin, None, None)
                .await?;
            let doc: Descriptor =
                serde_json::from_slice(body.bytes()).map_err(|_| Error::Protocol)?;
            #[cfg(feature = "synthetic-transport-fixture")]
            if b.fixture_scope
                .as_ref()
                .is_some_and(|(_, community, _)| doc.community_id != *community)
            {
                return Err(Error::Denied);
            }
            let d = &doc.native_api;
            if d.protocol != "mnema-native-preview-v1"
                || d.api_version != 1
                || d.prefix != "/api/native/v1"
                || !["supported", "deprecated"].contains(&d.compatibility.as_str())
                || d.authentication != "opaque-bearer-v1"
                || d.content_authorization != "unavailable"
                || d.capabilities.len() > 16
                || !d.capabilities.iter().any(|c| c == "authentication")
                || d.capabilities.iter().any(|c| {
                    c.len() > 64
                        || !c
                            .bytes()
                            .all(|x| x.is_ascii_alphanumeric() || b"_-".contains(&x))
                })
            {
                return Err(Error::QualificationRequired);
            }
            let base=serde_json::to_vec(&serde_json::json!({"protocol":doc.protocol,"community_id":doc.community_id,"api_versions":doc.api_versions,"e2ee_required":doc.e2ee_required})).map_err(|_|Error::Protocol)?;
            let verified = mnema_desktop_probe::discovery::parse_document(&origin, &base)
                .map_err(|_| Error::QualificationRequired)?;
            shared.current(&lease)?;
            if shared.intent.load(Ordering::Acquire) != intent {
                return Err(Error::Stale);
            }
            b.select_confirmed_profile(lease.owner, &verified.origin, &verified.community_id)
                .await?;
            let scope = b.selected_publication_scope(lease.owner)?;
            let window = shared.window.lock().map_err(|_| Error::Internal)?;
            if lease.cancel.is_cancelled()
                || window
                    .as_ref()
                    .is_none_or(|w| w.context != lease.context || w.owner != lease.owner)
            {
                return Err(Error::Stale);
            }
            if shared.intent.load(Ordering::Acquire) != intent {
                return Err(Error::Stale);
            }
            b.check_publication_scope(lease.owner, scope)?;
            shared.ready.store(intent, Ordering::Release);
            Value::Connected(
                scope,
                ConnectedPreview {
                    profile_intent: b.selected_profile_identity(lease.owner)?.to_string(),
                    origin: verified.origin,
                    community_id: verified.community_id,
                    compatibility: d.compatibility.clone(),
                    transport_preview: true,
                    content_authorization: "unavailable".to_owned(),
                },
            )
        }
        NativeRequest::OpenSocket { observer } => {
            actor_socket::open(shared.clone(), b.clone(), lease.clone(), intent, observer).await?
        }
        NativeRequest::Login { username, password } => {
            if bound_broker.is_none() {
                actor_socket::cancel_sockets(shared, lease.context);
            }
            Value::Login(b.login_delivery(lease.owner, &username, password).await?)
        }
        NativeRequest::Register {
            username,
            display_name,
            password,
            invite_code,
        } => Value::Register(
            b.registration_delivery(lease.owner, username, display_name, password, invite_code)
                .await?,
        ),
        NativeRequest::Password {
            current_password,
            new_password,
        } => {
            if bound_broker.is_none() {
                actor_socket::cancel_sockets(shared, lease.context);
            }
            Value::Password(
                b.password_delivery(lease.owner, current_password, new_password)
                    .await?,
            )
        }
        NativeRequest::Me => Value::Me(b.execute_me(b.prepare_me(lease.owner)?).await?),
        NativeRequest::Metadata { resource } => {
            Value::Metadata(b.metadata(lease.owner, resource).await?)
        }
        NativeRequest::PersonalMetadata { operation } => {
            Value::Me(b.personal_metadata(lease.owner, operation).await?)
        }
        NativeRequest::AdminMetadata { operation } => {
            Value::Admin(b.admin_metadata(lease.owner, operation).await?)
        }
        NativeRequest::OpaqueRelay { scope, operation } => {
            if scope.client_binding != Some((lease.context, intent)) {
                return Err(Error::Denied);
            }
            Value::OpaqueRelay(
                b.opaque_relay_retained(lease.owner, scope, operation)
                    .await?,
            )
        }
        NativeRequest::PublicMetadata { resource } => {
            Value::Metadata(b.public_metadata(lease.owner, resource).await?)
        }
        NativeRequest::Refresh => {
            b.refresh(lease.owner).await?;
            Value::Refresh(b.selected_publication_scope(lease.owner)?)
        }
        NativeRequest::Logout => {
            if bound_broker.is_none() {
                actor_socket::cancel_sockets(shared, lease.context);
            }
            Value::Logout(b.logout_delivery(lease.owner).await?)
        }
    };
    shared.ready(&lease, intent)?;
    Ok(NativePublication {
        lease,
        intent,
        value,
    })
}

#[cfg(test)]
mod lifecycle_tests {
    use super::*;
    #[test]
    fn actual_native_publication_closure_holds_window_and_profile_until_enqueue_finishes() {
        let client = NativeClient::ephemeral().unwrap();
        let lease = client.attach_main_window().unwrap();
        tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .unwrap()
            .block_on(client.shared.broker.select_confirmed_profile(
                lease.owner,
                "https://fixture.example",
                "fixture-community",
            ))
            .unwrap();
        let scope = client
            .shared
            .broker
            .selected_publication_scope(lease.owner)
            .unwrap();
        let worker = client.clone();
        let owner = lease.clone();
        let (start, started) = std::sync::mpsc::channel();
        let gate = Arc::new(std::sync::Barrier::new(2));
        let hold = gate.clone();
        let enqueue = std::thread::spawn(move || {
            worker.with_socket_publication(&owner, scope, || {
                start.send(()).unwrap();
                hold.wait();
            })
        });
        started
            .recv_timeout(std::time::Duration::from_secs(2))
            .unwrap();
        assert!(matches!(
            client.shared.window.try_lock(),
            Err(std::sync::TryLockError::WouldBlock)
        ));
        assert!(matches!(
            client.shared.broker.inner.state.try_lock(),
            Err(std::sync::TryLockError::WouldBlock)
        ));
        gate.wait();
        enqueue.join().unwrap().unwrap();
        client.disconnect(&lease).unwrap();
        assert!(matches!(
            client.with_socket_publication(&lease, scope, || panic!("stale enqueue")),
            Err(Error::Stale)
        ));
        client.detach_main_window(&lease).unwrap();
    }
    #[test]
    fn connect_holds_native_lifecycle_ownership_through_synchronous_mutation() {
        let client = NativeClient::ephemeral().unwrap();
        let old = client.attach_main_window().unwrap();
        let (started, receive) = std::sync::mpsc::channel();
        let gate = Arc::new(std::sync::Barrier::new(2));
        *client.shared.connect_before_mutation.lock().unwrap() = Some((started, gate.clone()));
        let worker = client.clone();
        let request = std::thread::spawn(move || {
            tokio::runtime::Builder::new_current_thread()
                .enable_all()
                .build()
                .unwrap()
                .block_on(worker.request(
                    &old,
                    NativeRequest::Connect {
                        address: String::new(),
                    },
                ))
        });
        receive
            .recv_timeout(std::time::Duration::from_secs(2))
            .unwrap();
        // Exact validation→mutation boundary holds the same lock used by native
        // detach/attach. Recreating a window cannot fit in that old TOCTOU gap.
        assert!(matches!(
            client.shared.window.try_lock(),
            Err(std::sync::TryLockError::WouldBlock)
        ));
        let recreated = client.clone();
        let replacement = std::thread::spawn(move || recreated.attach_main_window());
        gate.wait();
        assert!(request.join().unwrap().is_err());
        let new = replacement.join().unwrap().unwrap();
        client.check_window(&new).unwrap();
        assert_eq!(client.shared.broker.status().unwrap(), Status::Empty);
        client.detach_main_window(&new).unwrap();
    }
}

#[path = "actor_socket.rs"]
mod actor_socket;
pub use actor_socket::{NativeSocketNotice, NativeSocketObserver};
use actor_socket::{SocketControl, SocketOpened};
