//! Actual local-thread Core/OS-modal host; no serialized trust capability.
use super::*;
use mnema_private_native_client_broker::NativeAuthenticatedScope;
use mnema_private_native_crypto_owner::{
    NativeRootPreview, PendingFirstRoot, PendingRootCancellation,
};
use mnema_private_native_trust_dialog::{
    DialogError, NativeRegistryValidator, NativeTrustDialog, TauriNativeTrustDialog,
};
use std::{
    path::PathBuf,
    sync::atomic::{AtomicBool, AtomicUsize, Ordering},
    time::{Duration, Instant},
};
use tokio::sync::{mpsc, oneshot};
#[path = "crypto_custody.rs"]
mod custody;

static SETUPS: AtomicUsize = AtomicUsize::new(0);
#[derive(Clone, Serialize)]
pub(super) struct Status {
    operation_id: String,
    state: &'static str,
}
#[derive(Serialize)]
pub(super) struct ChatDisplay {
    id: Uuid,
    number: i64,
    channel_id: Uuid,
    client_event_id: Uuid,
    account_id: Uuid,
    device_id: String,
    body: String,
}
enum Job {
    Confirm(Arc<TauriNativeTrustDialog>),
    Chat {
        event: Option<Uuid>,
        body: zeroize::Zeroizing<String>,
        output: Channel<Vec<ChatDisplay>>,
        reply: oneshot::Sender<Result<(), Error>>,
    },
    Stop,
}
struct Control {
    channel: Uuid,
    lease: NativeWindowLease,
    scope: Arc<NativeAuthenticatedScope>,
    operation: Mutex<Option<Uuid>>,
    status: Mutex<Option<Status>>,
    cancel: Mutex<Option<PendingRootCancellation>>,
    dialog: Mutex<Option<Arc<TauriNativeTrustDialog>>>,
    stop: AtomicBool,
    jobs: mpsc::Sender<Job>,
}
impl Control {
    fn same_family(&self, scope: &NativeAuthenticatedScope) -> bool {
        scope.native_context() == self.scope.native_context()
            && scope.origin() == self.scope.origin()
            && scope.community_id() == self.scope.community_id()
            && scope.account_id() == self.scope.account_id()
            && scope.family_id() == self.scope.family_id()
            && scope.client_instance_id() == self.scope.client_instance_id()
            && scope.native_profile_identity() == self.scope.native_profile_identity()
            && scope.native_window_identity() == self.scope.native_window_identity()
    }
    fn stop(&self) {
        self.stop.store(true, Ordering::Release);
        if let Ok(cancel) = self.cancel.lock()
            && let Some(cancel) = cancel.as_ref()
        {
            let _ = cancel.cancel();
        }
        if let (Ok(dialog), Ok(operation)) = (self.dialog.lock(), self.operation.lock())
            && let (Some(dialog), Some(op)) = (dialog.as_ref(), *operation)
        {
            let _ = dialog.cancel_first_root(op);
        }
        let _ = self.jobs.try_send(Job::Stop);
    }
    fn value(&self, state: &'static str) -> Option<Status> {
        let operation = *self.operation.lock().ok()?;
        Some(Status {
            operation_id: operation?.to_string(),
            state,
        })
    }
    fn update(&self, state: &'static str) -> Option<Status> {
        let value = self.value(state)?;
        *self.status.lock().ok()? = Some(value.clone());
        Some(value)
    }
}
fn commit_after_enqueue(
    current: &Mutex<Option<Status>>,
    value: Status,
    enqueue: impl FnOnce(&Status) -> bool,
) -> bool {
    let Ok(mut current) = current.lock() else {
        return false;
    };
    if !enqueue(&value) {
        return false;
    }
    *current = Some(value);
    true
}
fn expire_after_enqueue(
    current: &Mutex<Option<Status>>,
    terminal: &'static str,
    enqueue: impl FnOnce(&Status) -> bool,
) -> bool {
    let Ok(mut current) = current.lock() else {
        return false;
    };
    let Some(status) = current.as_mut() else {
        return false;
    };
    if status.state != "pending" {
        return false;
    }
    let expired = Status {
        operation_id: status.operation_id.clone(),
        state: terminal,
    };
    let delivered = enqueue(&expired);
    *status = expired;
    delivered
}
fn negative_notice(
    app: &tauri::AppHandle,
    client: &NativeClient,
    lease: &NativeWindowLease,
    control: &Arc<Control>,
    output: &Channel<Status>,
    terminal: &'static str,
) {
    // Public operation failure is not an authentication/cryptographic grant.
    // Native registry + exact installed actor allocation prohibit an old
    // profile/window/operation from projecting into its successor.
    let runtime = app.state::<RuntimeState>();
    if let Ok(registry) = runtime.registry.lock() {
        let allowed = registry.as_ref().is_some_and(|entry| {
            entry.document == NativeDocument::App
                && entry.lease.context_nonce() == lease.context_nonce()
        }) && client.check_window(lease).is_ok();
        let installed = runtime.core.current.lock().ok();
        let allowed = allowed
            && installed.as_ref().is_some_and(|current| {
                current
                    .as_ref()
                    .is_some_and(|current| Arc::ptr_eq(current, control))
            });
        expire_after_enqueue(&control.status, terminal, |value| {
            allowed && output.send(value.clone()).is_ok()
        });
    } else {
        expire_after_enqueue(&control.status, terminal, |_| false);
    }
}
struct ThreadExit(Arc<Control>);
impl Drop for ThreadExit {
    fn drop(&mut self) {
        if self
            .0
            .status
            .lock()
            .ok()
            .and_then(|v| v.clone())
            .is_some_and(|v| matches!(v.state, "pending" | "root_saved"))
        {
            self.0.update("failed");
        }
        self.0.stop();
    }
}
#[derive(Default)]
pub(super) struct Host {
    current: Mutex<Option<Arc<Control>>>,
}
impl Host {
    pub(super) fn retire_profile(&self, context: &str, profile: Uuid) {
        if let Ok(mut current) = self.current.lock()
            && current.as_ref().is_some_and(|control| {
                control.lease.context_nonce() == context
                    && control.scope.native_profile_identity() == profile
            })
            && let Some(control) = current.take()
        {
            control.stop()
        }
    }
    pub(super) fn retire_context(&self, context: &str) {
        if let Ok(mut current) = self.current.lock()
            && current
                .as_ref()
                .is_some_and(|c| c.lease.context_nonce() == context)
            && let Some(control) = current.take()
        {
            control.stop();
        }
    }
    fn current(
        &self,
        lease: &NativeWindowLease,
        operation: Option<Uuid>,
    ) -> Result<Arc<Control>, Error> {
        let current = self.current.lock().map_err(|_| Error::Internal)?;
        let control = current
            .as_ref()
            .filter(|c| {
                c.lease.context_nonce() == lease.context_nonce() && !c.stop.load(Ordering::Acquire)
            })
            .ok_or(Error::Stale)?;
        if let Some(op) = operation
            && *control.operation.lock().map_err(|_| Error::Internal)? != Some(op)
        {
            return Err(Error::Stale);
        }
        Ok(control.clone())
    }
    fn current_profile(
        &self,
        lease: &NativeWindowLease,
        operation: Option<Uuid>,
        profile: Uuid,
    ) -> Result<Arc<Control>, Error> {
        let control = self.current(lease, operation)?;
        if control.scope.native_profile_identity() != profile {
            return Err(Error::Stale);
        }
        Ok(control)
    }
    #[allow(clippy::too_many_arguments)]
    pub(super) async fn begin(
        &self,
        app: tauri::AppHandle,
        window: WebviewWindow,
        client: NativeClient,
        lease: NativeWindowLease,
        channel: Uuid,
        profile: Uuid,
        on_status: Channel<Status>,
    ) -> Result<serde_json::Value, Error> {
        let scope = Arc::new(client.authenticated_scope(&lease)?);
        if scope.native_profile_identity() != profile {
            return Err(Error::Stale);
        }
        if scope.role() != "admin" {
            return Err(Error::Denied);
        }
        let (jobs, receive) = mpsc::channel(4);
        let control = Arc::new(Control {
            channel,
            lease: lease.clone(),
            scope: scope.clone(),
            operation: Mutex::new(None),
            status: Mutex::new(None),
            cancel: Mutex::new(None),
            dialog: Mutex::new(None),
            stop: AtomicBool::new(false),
            jobs,
        });
        // Tauri's dispatcher may synchronously wait for the UI thread. Never
        // perform this native getter while its lifecycle registry is locked.
        let native_identity = identity(&window)?;
        {
            // Same native registry mutex as reload/destroy, through actual scope
            // check and host reservation. An old queued begin cannot install a
            // new active owner after the actual window was replaced.
            let state = app.state::<RuntimeState>();
            let registry = state.registry.lock().map_err(|_| Error::Internal)?;
            let entry = registry.as_ref().ok_or(Error::Stale)?;
            if entry.document != NativeDocument::App
                || entry.identity != native_identity
                || entry.lease.context_nonce() != lease.context_nonce()
            {
                return Err(Error::Stale);
            }
            client.with_authenticated_publication(&lease, &scope, || {
                let mut current = self.current.lock().map_err(|_| Error::Internal)?;
                if current
                    .as_ref()
                    .is_some_and(|current| !current.stop.load(Ordering::Acquire))
                {
                    return Err(Error::Busy);
                }
                SETUPS
                    .try_update(Ordering::AcqRel, Ordering::Acquire, |count| {
                        (count < 16).then_some(count + 1)
                    })
                    .map_err(|_| Error::Busy)?;
                *current = Some(control.clone());
                Ok::<_, Error>(())
            })??;
        }
        let dir = fresh_directory(&app).inspect_err(|_| control.stop())?;
        let (reply, answer) = oneshot::channel();
        let thread_control = control.clone();
        let actor_app = app.clone();
        std::thread::Builder::new()
            .name("mnema-native-core".into())
            .spawn(move || {
                let _exit = ThreadExit(thread_control.clone());
                let Ok(runtime) = tokio::runtime::Builder::new_current_thread()
                    .enable_all()
                    .build()
                else {
                    let _ = reply.send(Err(Error::Internal));
                    return;
                };
                let local = tokio::task::LocalSet::new();
                runtime.block_on(local.run_until(run_core(
                    thread_control,
                    client,
                    lease,
                    scope,
                    dir,
                    actor_app,
                    receive,
                    reply,
                    on_status,
                )));
                drop(window);
            })
            .map_err(|_| {
                control.stop();
                Error::Internal
            })?;
        answer.await.map_err(|_| Error::Internal)?
    }
    fn status(&self, lease: &NativeWindowLease, profile: Uuid) -> Result<Option<Status>, Error> {
        let current = self.current.lock().map_err(|_| Error::Internal)?;
        let Some(c) = current.as_ref() else {
            return Ok(None);
        };
        if c.lease.context_nonce() != lease.context_nonce()
            || c.scope.native_profile_identity() != profile
        {
            return Err(Error::Stale);
        }
        let status = c.status.lock().map_err(|_| Error::Internal)?.clone();
        Ok(status)
    }
}
#[allow(clippy::too_many_arguments)]
async fn run_core(
    control: Arc<Control>,
    client: NativeClient,
    lease: NativeWindowLease,
    scope: Arc<NativeAuthenticatedScope>,
    dir: PathBuf,
    app: tauri::AppHandle,
    mut receive: mpsc::Receiver<Job>,
    reply: oneshot::Sender<Result<serde_json::Value, Error>>,
    on_status: Channel<Status>,
) {
    if control.stop.load(Ordering::Acquire)
        || client.check_authenticated_scope(&lease, &scope).is_err()
    {
        let _ = reply.send(Err(Error::Stale));
        return;
    }
    let secrets = match custody::Secrets::create(&scope, &dir) {
        Ok(s) => s,
        Err(_) => {
            let _ = reply.send(Err(Error::QualificationRequired));
            return;
        }
    };
    let (pending, cancel) = match PendingFirstRoot::begin(
        client.clone(),
        lease.clone(),
        &dir.join("root.sqlite"),
        &secrets,
        control.channel,
    )
    .await
    {
        Ok(v) => v,
        Err(_) => {
            let _ = reply.send(Err(Error::Denied));
            return;
        }
    };
    if control.stop.load(Ordering::Acquire)
        || client
            .check_authenticated_scope(&lease, pending.authenticated_scope())
            .is_err()
        || client.check_authenticated_scope(&lease, &scope).is_err()
    {
        let _ = reply.send(Err(Error::Stale));
        return;
    }
    let operation = cancel.operation_id();
    if let Ok(mut op) = control.operation.lock() {
        *op = Some(operation)
    }
    if let Ok(mut cancellation) = control.cancel.lock() {
        *cancellation = Some(cancel)
    }
    control.update("pending");
    let preview = preview(pending.public_preview(), pending.native_deadline());
    if reply.send(Ok(preview)).is_err() {
        return;
    }
    let mut pending = Some(pending);
    let mut owner = None;
    let mut access_deadline = scope.monotonic_access_deadline();
    let mut heartbeat = tokio::time::interval(Duration::from_millis(100));
    loop {
        let job = tokio::select! {
            job=receive.recv()=>job,
            _=heartbeat.tick()=>{
                if pending.as_ref().is_some_and(|value| Instant::now() >= value.native_deadline()) {
                    // This terminal failure conveys no grant or protected data.
                    // Access expiry may prevent an authenticated publication;
                    // exact native document ownership still gates the notice.
                    negative_notice(&app,&client,&lease,&control,&on_status,"expired");
                    control.stop();
                    None
                } else {
                let retained = match client.authenticated_scope(&lease) {
                    Ok(current) if control.same_family(&current) => {
                        access_deadline = current.monotonic_access_deadline(); true
                    },
                    // Retain custody only, with no publication or SDK grant,
                    // while the actual broker is single-flight rotating. A
                    // completed uncertain/revoked rotation retires on next tick.
                    Err(Error::Busy) => Instant::now() < access_deadline && client.check_window(&lease).is_ok(),
                    _ => false,
                };
                if control.stop.load(Ordering::Acquire) || !retained {None}else{continue}
                }
            }
        };
        let Some(job) = job else { break };
        match job {
            Job::Stop => break,
            Job::Confirm(dialog) => {
                let Some(pending) = pending.take() else {
                    continue;
                };
                let result = pending.confirm(dialog.as_ref()).await;
                if control.stop.load(Ordering::Acquire) {
                    if let Ok(mut value) = result {
                        value.retire()
                    }
                    break;
                }
                let state = match result {
                    Ok(value) => {
                        owner = Some(value);
                        "root_saved"
                    }
                    Err(_) => "failed",
                };
                if let Some(status) = control.value(state) {
                    let runtime = app.state::<RuntimeState>();
                    let delivered = if let Ok(registry) = runtime.registry.lock()
                        && let Some(entry) = registry.as_ref()
                        && entry.document == NativeDocument::App
                        && entry.lease.context_nonce() == lease.context_nonce()
                    {
                        let final_scope =
                            owner.as_ref().map(|v| v.adoption_scope()).unwrap_or(&scope);
                        let final_deadline = owner.as_ref().map(|v| v.adoption_deadline());
                        client
                            .with_authenticated_publication(&lease, final_scope, || {
                                if control.stop.load(Ordering::Acquire) {
                                    return false;
                                }
                                // Public terminal status becomes observable
                                // only after the actual fenced enqueue won.
                                commit_after_enqueue(&control.status, status, |value| {
                                    final_deadline.is_none_or(|deadline| Instant::now() < deadline)
                                        && on_status.send(value.clone()).is_ok()
                                })
                            })
                            .unwrap_or(false)
                    } else {
                        false
                    };
                    if !delivered {
                        negative_notice(&app, &client, &lease, &control, &on_status, "failed");
                        if let Some(mut value) = owner.take() {
                            value.retire();
                        }
                        break;
                    }
                    if owner.is_some() {
                        secrets.adopted();
                    } else {
                        break;
                    }
                }
            }
            Job::Chat {
                event,
                body,
                output,
                reply,
            } => {
                let mut terminal = false;
                let result = if control.stop.load(Ordering::Acquire) {
                    terminal = true;
                    Err(Error::Stale)
                } else if let Some(owner) = owner.as_mut() {
                    let sink = |rows: Vec<ChatDisplay>| {
                        // Owner invokes this while the actual broker publication
                        // fence is held. Never wait for the inverse registry→broker
                        // lock order: contention or stale native document denies.
                        let state = app.state::<RuntimeState>();
                        let registry = state
                            .registry
                            .try_lock()
                            .map_err(|_| mnema_private_native_crypto_owner::Error::Retired)?;
                        let entry = registry
                            .as_ref()
                            .ok_or(mnema_private_native_crypto_owner::Error::Retired)?;
                        if control.stop.load(Ordering::Acquire)
                            || entry.document != NativeDocument::App
                            || entry.lease.context_nonce() != lease.context_nonce()
                        {
                            return Err(mnema_private_native_crypto_owner::Error::Retired);
                        }
                        output
                            .send(rows)
                            .map_err(|_| mnema_private_native_crypto_owner::Error::Retired)
                    };
                    let chat = owner.chat_mut();
                    let result = if let Some(event) = event {
                        match chat.prepare_chat(event, &body) {
                            Ok(prepared) => {
                                chat.publish_chat(prepared, |row| {
                                    sink(vec![ChatDisplay {
                                        id: row.id,
                                        number: row.number,
                                        channel_id: row.channel_id,
                                        client_event_id: row.client_event_id,
                                        account_id: row.account_id,
                                        device_id: row.device_id.clone(),
                                        body: row.body.clone(),
                                    }])
                                })
                                .await
                            }
                            Err(error) => Err(error),
                        }
                    } else {
                        chat.receive_page(|rows| {
                            sink(
                                rows.iter()
                                    .map(|row| ChatDisplay {
                                        id: row.id,
                                        number: row.number,
                                        channel_id: row.channel_id,
                                        client_event_id: row.client_event_id,
                                        account_id: row.account_id,
                                        device_id: row.device_id.clone(),
                                        body: row.body.clone(),
                                    })
                                    .collect(),
                            )
                        })
                        .await
                    };
                    result.map_err(|error| {
                        use mnema_private_native_crypto_owner::Error as C;
                        match error {
                            C::Auth => match client.authenticated_scope(&lease) {
                                // A relay transport failure may have committed
                                // the exact durable event. Keep the outbox for a
                                // manual identical retry; never auto-send again.
                                Ok(current) if control.same_family(&current) => Error::Network,
                                _ => {
                                    terminal = true;
                                    Error::Stale
                                }
                            },
                            C::Binding | C::Crypto | C::Retired => {
                                terminal = true;
                                crypto_error(error)
                            }
                            _ => crypto_error(error),
                        }
                    })
                } else {
                    Err(Error::QualificationRequired)
                };
                if terminal {
                    control.update("failed");
                    control.stop();
                    if let Some(mut retired) = owner.take() {
                        retired.retire();
                    }
                }
                let _ = reply.send(result);
                if terminal {
                    break;
                }
            }
        }
    }
    if let Some(mut owner) = owner {
        owner.retire();
    }
    negative_notice(&app, &client, &lease, &control, &on_status, "failed");
}
fn fresh_directory(app: &tauri::AppHandle) -> Result<PathBuf, Error> {
    let base = app
        .path()
        .app_data_dir()
        .map_err(|_| Error::Internal)?
        .join("native-crypto-dev");
    std::fs::create_dir_all(&base).map_err(|_| Error::Internal)?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::{MetadataExt, PermissionsExt};
        let m = std::fs::symlink_metadata(&base).map_err(|_| Error::Internal)?;
        if m.file_type().is_symlink() || m.uid() != unsafe { libc::geteuid() } {
            return Err(Error::Denied);
        }
        std::fs::set_permissions(&base, std::fs::Permissions::from_mode(0o700))
            .map_err(|_| Error::Internal)?;
    }
    let path = base.join(Uuid::new_v4().to_string());
    std::fs::create_dir(&path).map_err(|_| Error::Internal)?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o700))
            .map_err(|_| Error::Internal)?;
    }
    Ok(path)
}
fn preview(p: NativeRootPreview, deadline: Instant) -> serde_json::Value {
    let expires = chrono::Utc::now()
        + chrono::Duration::from_std(deadline.saturating_duration_since(Instant::now()))
            .unwrap_or_default();
    serde_json::json!({"version":1,"operation_id":p.operation_id,"operation_kind":"first_root","expires_at":expires.to_rfc3339_opts(chrono::SecondsFormat::Millis,true),"scope":{"origin":p.origin,"community_id":p.community_id,"channel_id":p.channel_id,"account_id":p.account_id,"device_id":p.device_id,"group_id":p.group_id},"root_fingerprint_hex":p.root_fingerprint,"root_public_key_hex":p.root_public_key,"device_public_key_hex":p.device_public_key})
}
fn crypto_error(error: mnema_private_native_crypto_owner::Error) -> Error {
    use mnema_private_native_crypto_owner::Error as C;
    match error {
        C::Invalid => Error::InvalidInput,
        C::Limit => Error::Busy,
        C::Auth | C::Retired => Error::Stale,
        C::Binding | C::Crypto | C::Conflict => Error::Denied,
    }
}
fn validate_body(body: &str, publishing: bool) -> Result<(), Error> {
    if body.len() > 24 * 1024 || (publishing && body.is_empty()) {
        return Err(Error::InvalidInput);
    }
    Ok(())
}
#[allow(clippy::too_many_arguments)]
async fn chat_command(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    context: String,
    profile_intent: String,
    channel: String,
    event: Option<String>,
    body: zeroize::Zeroizing<String>,
    output: Channel<Vec<ChatDisplay>>,
) -> NativeReply {
    let result = async {
        let lease = require(&window, &state, Some(&context))?;
        state
            .client
            .check_profile_intent(&lease, canonical(&profile_intent)?)?;
        let channel = canonical(&channel)?;
        let event = event.map(|value| canonical(&value)).transpose()?;
        validate_body(&body, event.is_some())?;
        let control = state
            .core
            .current_profile(&lease, None, canonical(&profile_intent)?)?;
        let scope = state.client.authenticated_scope(&lease)?;
        if !control.same_family(&scope) || control.channel != channel {
            return Err(Error::Denied);
        }
        if control
            .status
            .lock()
            .map_err(|_| Error::Internal)?
            .as_ref()
            .is_none_or(|status| status.state != "root_saved")
        {
            return Err(Error::QualificationRequired);
        }
        let (reply, answer) = oneshot::channel();
        control
            .jobs
            .try_send(Job::Chat {
                event,
                body,
                output,
                reply,
            })
            .map_err(|_| Error::Busy)?;
        answer.await.map_err(|_| Error::Stale)??;
        require(&window, &state, Some(&context))?;
        let scope = state.client.authenticated_scope(&lease)?;
        if !control.same_family(&scope) || control.stop.load(Ordering::Acquire) {
            return Err(Error::Stale);
        }
        Ok::<_, Error>(serde_json::json!({"state":"completed"}))
    }
    .await;
    match result {
        Ok(body) => reply(context, body),
        Err(error) => rejected(context, error),
    }
}
#[tauri::command]
#[allow(clippy::too_many_arguments)]
pub(super) async fn native_chat_publish(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    context: String,
    profile_intent: String,
    channel_id: String,
    client_event_id: String,
    body: String,
    on_messages: Channel<Vec<ChatDisplay>>,
) -> Result<NativeReply, ()> {
    Ok(chat_command(
        window,
        state,
        context,
        profile_intent,
        channel_id,
        Some(client_event_id),
        zeroize::Zeroizing::new(body),
        on_messages,
    )
    .await)
}
#[tauri::command]
pub(super) async fn native_chat_receive(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    context: String,
    profile_intent: String,
    channel_id: String,
    on_messages: Channel<Vec<ChatDisplay>>,
) -> Result<NativeReply, ()> {
    Ok(chat_command(
        window,
        state,
        context,
        profile_intent,
        channel_id,
        None,
        zeroize::Zeroizing::new(String::new()),
        on_messages,
    )
    .await)
}
fn reply(context: String, body: serde_json::Value) -> NativeReply {
    NativeReply {
        context,
        status: 200,
        body,
    }
}
#[tauri::command]
pub(super) async fn native_trust_begin_first_root(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    context: String,
    profile_intent: String,
    channel_id: String,
    on_status: Channel<Status>,
) -> Result<NativeReply, ()> {
    let result = async {
        let lease = require(&window, &state, Some(&context))?;
        state
            .client
            .check_profile_intent(&lease, canonical(&profile_intent)?)?;
        let body = state
            .core
            .begin(
                window.app_handle().clone(),
                window.clone(),
                state.client.clone(),
                lease.clone(),
                canonical(&channel_id)?,
                canonical(&profile_intent)?,
                on_status,
            )
            .await?;
        require(&window, &state, Some(&context))?;
        state
            .client
            .check_profile_intent(&lease, canonical(&profile_intent)?)?;
        Ok::<_, Error>(body)
    }
    .await;
    Ok(match result {
        Ok(body) => reply(context, body),
        Err(e) => rejected(context, e),
    })
}
#[tauri::command]
pub(super) fn native_trust_request_confirmation(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    context: String,
    profile_intent: String,
    operation_id: String,
) -> NativeReply {
    let result = (|| {
        let lease = require(&window, &state, Some(&context))?;
        state
            .client
            .check_profile_intent(&lease, canonical(&profile_intent)?)?;
        let op = canonical(&operation_id)?;
        let control = state
            .core
            .current_profile(&lease, Some(op), canonical(&profile_intent)?)?;
        state
            .client
            .check_authenticated_scope(&lease, &control.scope)?;
        let app = window.app_handle().clone();
        let validator: NativeRegistryValidator = Arc::new(move |window, lease| {
            let state = app.state::<RuntimeState>();
            let current = require(window, &state, Some(&lease.context_nonce()))
                .map_err(|_| DialogError::Stale)?;
            if current.context_nonce() != lease.context_nonce() {
                return Err(DialogError::Stale);
            }
            Ok(())
        });
        let dialog = Arc::new(
            TauriNativeTrustDialog::from_native_window(
                window,
                state.client.clone(),
                lease,
                validator,
            )
            .map_err(|_| Error::Denied)?,
        );
        let mut existing = control.dialog.lock().map_err(|_| Error::Internal)?;
        if existing.is_some() {
            return Err(Error::Busy);
        }
        *existing = Some(dialog.clone());
        control
            .jobs
            .try_send(Job::Confirm(dialog))
            .map_err(|_| Error::Busy)?;
        Ok::<_, Error>(serde_json::json!({"operation_id":op.to_string(),"state":"pending"}))
    })();
    match result {
        Ok(body) => reply(context, body),
        Err(e) => rejected(context, e),
    }
}
#[tauri::command]
pub(super) fn native_trust_cancel(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    context: String,
    profile_intent: String,
    operation_id: String,
) -> NativeReply {
    let result = (|| {
        let lease = require(&window, &state, Some(&context))?;
        state
            .client
            .check_profile_intent(&lease, canonical(&profile_intent)?)?;
        let op = canonical(&operation_id)?;
        let control = state
            .core
            .current_profile(&lease, Some(op), canonical(&profile_intent)?)?;
        let cancel = control.cancel.lock().map_err(|_| Error::Internal)?;
        if let Some(cancel) = cancel.as_ref()
            && cancel.cancel().is_err()
        {
            // Adoption/failed operation won; do not falsely report cancellation
            // or destroy a live adopted root when presentation closes.
            let status = control
                .status
                .lock()
                .map_err(|_| Error::Internal)?
                .clone()
                .ok_or(Error::Stale)?;
            if status.state == "pending" {
                // Adoption won but its final publication is not committed yet.
                // Closing presentation cannot cancel the adopted native owner.
                return Ok(serde_json::Value::Null);
            }
            return serde_json::to_value(status).map_err(|_| Error::Internal);
        }
        drop(cancel);
        if let Ok(dialog) = control.dialog.lock()
            && let Some(dialog) = dialog.as_ref()
        {
            let _ = dialog.cancel_first_root(op);
        }
        let status = control.update("cancelled").ok_or(Error::Stale)?;
        control.stop();
        serde_json::to_value(status).map_err(|_| Error::Internal)
    })();
    match result {
        Ok(body) if body.is_null() => NativeReply {
            context,
            status: 204,
            body,
        },
        Ok(body) => reply(context, body),
        Err(e) => rejected(context, e),
    }
}
#[tauri::command]
pub(super) fn native_trust_read_status(
    window: WebviewWindow,
    state: tauri::State<'_, RuntimeState>,
    context: String,
    profile_intent: String,
) -> NativeReply {
    let result = require(&window, &state, Some(&context)).and_then(|lease| {
        state
            .client
            .check_profile_intent(&lease, canonical(&profile_intent)?)?;
        state.core.status(&lease, canonical(&profile_intent)?)
    });
    match result {
        Ok(Some(status)) => reply(
            context,
            serde_json::to_value(status).unwrap_or(serde_json::Value::Null),
        ),
        Ok(None) => NativeReply {
            context,
            status: 204,
            body: serde_json::Value::Null,
        },
        Err(e) => rejected(context, e),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn shared_terminal_read_cannot_outrun_actual_successful_enqueue() {
        let statuses = Arc::new(Mutex::new(Some(Status {
            operation_id: Uuid::new_v4().to_string(),
            state: "pending",
        })));
        let entered = Arc::new(std::sync::Barrier::new(2));
        let release = Arc::new(std::sync::Barrier::new(2));
        let worker_status = statuses.clone();
        let worker_entered = entered.clone();
        let worker_release = release.clone();
        let worker = std::thread::spawn(move || {
            commit_after_enqueue(
                &worker_status,
                Status {
                    operation_id: Uuid::new_v4().to_string(),
                    state: "root_saved",
                },
                |_| {
                    worker_entered.wait();
                    worker_release.wait();
                    true
                },
            )
        });
        entered.wait();
        assert!(matches!(
            statuses.try_lock(),
            Err(std::sync::TryLockError::WouldBlock)
        ));
        release.wait();
        assert!(worker.join().unwrap());
        assert_eq!(
            statuses.lock().unwrap().as_ref().unwrap().state,
            "root_saved"
        );
    }
    #[test]
    fn refused_native_enqueue_never_commits_shared_root_saved() {
        let current = Mutex::new(Some(Status {
            operation_id: Uuid::new_v4().to_string(),
            state: "pending",
        }));
        assert!(!commit_after_enqueue(
            &current,
            Status {
                operation_id: Uuid::new_v4().to_string(),
                state: "root_saved"
            },
            |_| false
        ));
        assert_eq!(current.lock().unwrap().as_ref().unwrap().state, "pending");
    }
    #[test]
    fn expired_pending_notifies_registered_channel_and_never_overwrites_terminal() {
        let messages = Arc::new(Mutex::new(Vec::new()));
        let capture = messages.clone();
        let channel: Channel<Status> = Channel::new(move |body| {
            let tauri::ipc::InvokeResponseBody::Json(raw) = body else {
                panic!("fixed status must be JSON")
            };
            capture
                .lock()
                .unwrap()
                .push(serde_json::from_str::<serde_json::Value>(&raw).unwrap());
            Ok(())
        });
        let op = Uuid::new_v4().to_string();
        let current = Mutex::new(Some(Status {
            operation_id: op.clone(),
            state: "pending",
        }));
        assert!(expire_after_enqueue(&current, "expired", |value| channel
            .send(value.clone())
            .is_ok()));
        assert_eq!(current.lock().unwrap().as_ref().unwrap().state, "expired");
        assert_eq!(
            messages.lock().unwrap().as_slice(),
            &[serde_json::json!({"operation_id":op,"state":"expired"})]
        );
        assert!(!expire_after_enqueue(&current, "expired", |_| panic!(
            "expired terminal must not emit twice"
        )));
        *current.lock().unwrap() = Some(Status {
            operation_id: Uuid::new_v4().to_string(),
            state: "root_saved",
        });
        assert!(!expire_after_enqueue(&current, "expired", |_| panic!(
            "saved owner cannot expire as pending"
        )));
    }
    #[test]
    fn actual_public_preview_has_canonical_utc_and_only_contract_fields() {
        let raw = preview(
            NativeRootPreview {
                operation_id: Uuid::new_v4().to_string(),
                origin: "https://example.invalid".into(),
                community_id: "fixture".into(),
                account_id: Uuid::new_v4().to_string(),
                channel_id: Uuid::new_v4().to_string(),
                device_id: Uuid::new_v4().to_string(),
                group_id: "AAE=".into(),
                root_public_key: "a".repeat(64),
                root_fingerprint: "b".repeat(64),
                device_public_key: "c".repeat(64),
            },
            Instant::now() + Duration::from_secs(90),
        );
        assert_eq!(raw.as_object().unwrap().len(), 8);
        let expires = raw["expires_at"].as_str().unwrap();
        assert!(expires.ends_with('Z'));
        let remaining = chrono::DateTime::parse_from_rfc3339(expires)
            .unwrap()
            .signed_duration_since(chrono::Utc::now());
        assert!(remaining.num_seconds() > 85 && remaining.num_seconds() <= 90);
        assert_eq!(raw["scope"].as_object().unwrap().len(), 6);
    }
    #[test]
    fn actual_tauri_channel_sends_one_closed_array_even_for_empty_receive() {
        let recorded = Arc::new(Mutex::new(Vec::new()));
        let capture = recorded.clone();
        let channel: Channel<Vec<ChatDisplay>> = Channel::new(move |body| {
            let tauri::ipc::InvokeResponseBody::Json(json) = body else {
                panic!("projection must be JSON")
            };
            capture
                .lock()
                .unwrap()
                .push(serde_json::from_str::<serde_json::Value>(&json).unwrap());
            Ok(())
        });
        channel.send(vec![]).unwrap();
        channel
            .send(vec![ChatDisplay {
                id: Uuid::new_v4(),
                number: 1,
                channel_id: Uuid::new_v4(),
                client_event_id: Uuid::new_v4(),
                account_id: Uuid::new_v4(),
                device_id: Uuid::new_v4().to_string(),
                body: "dummy native projection 🦀".into(),
            }])
            .unwrap();
        let messages = recorded.lock().unwrap();
        assert_eq!(messages.len(), 2);
        assert_eq!(messages[0], serde_json::json!([]));
        assert_eq!(messages[1].as_array().unwrap().len(), 1);
        let fields = messages[1][0].as_object().unwrap();
        assert_eq!(fields.len(), 7);
        assert!(
            !fields.contains_key("ciphertext")
                && !fields.contains_key("group_id")
                && !fields.contains_key("access_token")
        );
    }
    #[test]
    fn chat_body_budget_counts_exact_utf8_bytes_and_rejects_empty_publish() {
        assert!(validate_body("", false).is_ok());
        assert_eq!(validate_body("", true), Err(Error::InvalidInput));
        assert!(validate_body(&"🦀".repeat(6144), true).is_ok());
        assert_eq!(
            validate_body(&"🦀".repeat(6145), true),
            Err(Error::InvalidInput)
        );
        assert!(validate_body("\t\nexact bytes", true).is_ok());
    }
}
