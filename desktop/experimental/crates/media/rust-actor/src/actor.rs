use mnema_private_native_client_broker::{
    Broker, Error, NativeSocket, Password, WindowOwner, WindowRole,
};
use mnema_private_synthetic_media_policy::{
    Action, BoundMedia, EventCursor, FixtureCapability, NativeBinding, NativeMediaEvent,
    OwnedFixture, Role,
};
use std::{
    collections::HashMap,
    fmt,
    sync::{
        Arc, Mutex,
        atomic::{AtomicU64, Ordering},
    },
    time::{Duration, Instant},
};
use tokio::sync::{mpsc, oneshot};
use tokio_util::sync::CancellationToken;
use uuid::Uuid;

/// Minted only by the Tauri native-window registry, never deserialized from IPC.
#[derive(Clone)]
pub struct NativeMediaWindow {
    context: Uuid,
    cancelled: CancellationToken,
}
impl fmt::Debug for NativeMediaWindow {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("NativeMediaWindow(REDACTED)")
    }
}
impl NativeMediaWindow {
    /// Native registry comparison only; this does not replace actor current-window checks.
    pub fn same_native_context(&self, other: &Self) -> bool {
        self.context == other.context
            && !self.cancelled.is_cancelled()
            && !other.cancelled.is_cancelled()
    }
}
pub type EventSink = Arc<dyn Fn(NativeMediaEvent) -> Result<(), ()> + Send + Sync>;
struct SessionControl {
    window: Uuid,
    generation: u64,
    capability: Arc<FixtureCapability>,
    sender: mpsc::Sender<Control>,
    stop: CancellationToken,
}
struct State {
    window: Option<NativeMediaWindow>,
    reservations: Vec<(Uuid, Role)>,
    sessions: HashMap<String, SessionControl>,
}
struct Shared {
    state: Mutex<State>,
    generation: AtomicU64,
    #[cfg(test)]
    after_login: Mutex<Option<(mpsc::UnboundedSender<()>, Arc<tokio::sync::Notify>)>>,
    #[cfg(test)]
    before_message_enqueue: Mutex<Option<(mpsc::UnboundedSender<()>, Arc<std::sync::Barrier>)>>,
    #[cfg(test)]
    worker_before_final_seal: Mutex<Option<(std::sync::mpsc::Sender<()>, Arc<std::sync::Barrier>)>>,
}
struct OwnerLifetime {
    shared: Arc<Shared>,
}
impl Drop for OwnerLifetime {
    fn drop(&mut self) {
        self.shared.seal_all();
    }
}
impl Shared {
    fn seal_all(&self) {
        if let Ok(mut state) = self.state.lock() {
            if let Some(window) = state.window.take() {
                window.cancelled.cancel();
            }
            for session in state.sessions.values() {
                session.capability.seal();
                session.stop.cancel();
            }
        }
    }
    fn current(&self, window: &NativeMediaWindow) -> Result<(), Error> {
        let s = self.state.lock().map_err(|_| Error::Internal)?;
        if window.cancelled.is_cancelled()
            || s.window
                .as_ref()
                .is_none_or(|w| w.context != window.context)
        {
            Err(Error::Stale)
        } else {
            Ok(())
        }
    }
    fn publish(
        &self,
        window: &NativeMediaWindow,
        sink: &EventSink,
        event: NativeMediaEvent,
        authorization: Option<(&Arc<FixtureCapability>, &NativeBinding)>,
    ) -> Result<(), Error> {
        // Serializes the check and enqueue with native detach; no post-detach enqueue.
        let s = self.state.lock().map_err(|_| Error::Internal)?;
        if window.cancelled.is_cancelled()
            || s.window
                .as_ref()
                .is_none_or(|w| w.context != window.context)
        {
            return Err(Error::Stale);
        }
        if let NativeMediaEvent::Message {
            handle, generation, ..
        } = &event
        {
            let (cap, binding) = authorization.ok_or(Error::Denied)?;
            let session = s.sessions.get(handle).ok_or(Error::Stale)?;
            if session.window != window.context
                || session.generation != *generation
                || !Arc::ptr_eq(&session.capability, cap)
                || session.stop.is_cancelled()
            {
                return Err(Error::Stale);
            }
            // Same mutex as close/detach seal: validation and actual enqueue are
            // indivisible with respect to native capability revocation.
            cap.check(Some(binding), handle, *generation, Instant::now())
                .map_err(|_| Error::Denied)?;
        } else if authorization.is_some() {
            return Err(Error::Denied);
        }
        sink(event).map_err(|_| Error::Cancelled)
    }
    fn release(&self, window: Uuid, role: Role, handle: Option<&str>) {
        if let Ok(mut s) = self.state.lock() {
            s.reservations.retain(|r| *r != (window, role));
            if let Some(handle) = handle {
                s.sessions.remove(handle);
            }
        }
    }
}
enum Control {
    Send {
        action: Action,
        answer: oneshot::Sender<Result<(), Error>>,
    },
    Close {
        answer: oneshot::Sender<Result<(), Error>>,
    },
}
struct Open {
    window: NativeMediaWindow,
    role: Role,
    sink: EventSink,
    answer: oneshot::Sender<Result<BoundMedia, Error>>,
}
#[derive(Clone)]
pub struct MediaActor {
    shared: Arc<Shared>,
    sender: mpsc::Sender<Open>,
    // Only public actor clones hold this Arc. Its final destructor is elected
    // by Arc's atomic refcount, independent of approximate Sender counts.
    _owner: Arc<OwnerLifetime>,
}
impl fmt::Debug for MediaActor {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("MediaActor(REDACTED)")
    }
}
impl MediaActor {
    pub fn check_window(&self, window: &NativeMediaWindow) -> Result<(), Error> {
        self.shared.current(window)
    }
    pub fn fixture(fixture: OwnedFixture) -> Result<Self, Error> {
        let shared = Arc::new(Shared {
            state: Mutex::new(State {
                window: None,
                reservations: vec![],
                sessions: HashMap::new(),
            }),
            generation: AtomicU64::new(0),
            #[cfg(test)]
            after_login: Mutex::new(None),
            #[cfg(test)]
            before_message_enqueue: Mutex::new(None),
            #[cfg(test)]
            worker_before_final_seal: Mutex::new(None),
        });
        let worker = shared.clone();
        let (sender, mut receive) = mpsc::channel::<Open>(2);
        std::thread::Builder::new()
            .name("mnema-synthetic-media-actor".into())
            .spawn(move || {
                let Ok(runtime) = tokio::runtime::Builder::new_current_thread()
                    .enable_all()
                    .build()
                else {
                    return;
                };
                tokio::task::LocalSet::new().block_on(&runtime, async move {
                    let fixture = Arc::new(fixture);
                    let mut tasks = Vec::new();
                    while let Some(job) = receive.recv().await {
                        tasks.retain(|task: &tokio::task::JoinHandle<()>| !task.is_finished());
                        let owner = worker.clone();
                        let fixture = fixture.clone();
                        tasks.push(tokio::task::spawn_local(async move {
                            run(owner, fixture, job).await;
                        }));
                    }
                    // Last public actor owner dropped: keep the LocalSet alive until
                    // native socket destruction and committed logout cleanup finish.
                    #[cfg(test)]
                    {
                        let hook = worker.worker_before_final_seal.lock().unwrap().take();
                        if let Some((entered, release)) = hook {
                            let _ = entered.send(());
                            release.wait();
                        }
                    }
                    worker.seal_all();
                    for task in tasks {
                        let _ = task.await;
                    }
                });
            })
            .map_err(|_| Error::Internal)?;
        let owner = Arc::new(OwnerLifetime {
            shared: shared.clone(),
        });
        Ok(Self {
            shared,
            sender,
            _owner: owner,
        })
    }
    /// Native OS lifecycle only. The caller must validate the actual main-window incarnation.
    pub fn native_attach_window(&self) -> Result<NativeMediaWindow, Error> {
        let mut s = self.shared.state.lock().map_err(|_| Error::Internal)?;
        if let Some(old) = s.window.take() {
            old.cancelled.cancel();
            for c in s.sessions.values() {
                c.capability.seal();
            }
        }
        let w = NativeMediaWindow {
            context: Uuid::new_v4(),
            cancelled: CancellationToken::new(),
        };
        s.window = Some(w.clone());
        Ok(w)
    }
    pub fn native_detach_window(&self, window: &NativeMediaWindow) -> Result<(), Error> {
        let mut s = self.shared.state.lock().map_err(|_| Error::Internal)?;
        if s.window
            .as_ref()
            .is_none_or(|w| w.context != window.context)
        {
            return Err(Error::Stale);
        }
        window.cancelled.cancel();
        s.window = None;
        for c in s.sessions.values() {
            if c.window == window.context {
                c.capability.seal();
            }
        }
        Ok(())
    }
    pub async fn open(
        &self,
        window: &NativeMediaWindow,
        role: Role,
        sink: EventSink,
    ) -> Result<BoundMedia, Error> {
        self.shared.current(window)?;
        {
            let mut s = self.shared.state.lock().map_err(|_| Error::Internal)?;
            // One role at a time even during teardown, including across window replacement.
            if s.reservations.iter().any(|(_, r)| *r == role) {
                return Err(Error::Busy);
            }
            s.reservations.push((window.context, role));
        }
        let (answer, receive) = oneshot::channel();
        if self
            .sender
            .try_send(Open {
                window: window.clone(),
                role,
                sink,
                answer,
            })
            .is_err()
        {
            self.shared.release(window.context, role, None);
            return Err(Error::Busy);
        }
        let bound = receive.await.map_err(|_| Error::Internal)??;
        self.shared.current(window)?;
        Ok(bound)
    }
    fn control(
        &self,
        window: &NativeMediaWindow,
        handle: &str,
        generation: u64,
    ) -> Result<mpsc::Sender<Control>, Error> {
        self.shared.current(window)?;
        let s = self.shared.state.lock().map_err(|_| Error::Internal)?;
        let c = s.sessions.get(handle).ok_or(Error::Denied)?;
        if c.window != window.context || c.generation != generation {
            return Err(Error::Denied);
        }
        Ok(c.sender.clone())
    }
    pub async fn send(
        &self,
        window: &NativeMediaWindow,
        handle: &str,
        generation: u64,
        action: Action,
    ) -> Result<(), Error> {
        let control = self.control(window, handle, generation)?;
        let (answer, receive) = oneshot::channel();
        control
            .try_send(Control::Send { action, answer })
            .map_err(|_| Error::Busy)?;
        receive.await.map_err(|_| Error::Cancelled)??;
        self.shared.current(window)
    }
    pub async fn close(
        &self,
        window: &NativeMediaWindow,
        handle: &str,
        generation: u64,
    ) -> Result<(), Error> {
        let control = self.control(window, handle, generation)?;
        // Seal synchronously before any queue/await; media cannot resume on queued work.
        let (answer, receive) = oneshot::channel();
        let queued = {
            let s = self.shared.state.lock().map_err(|_| Error::Internal)?;
            let session = s.sessions.get(handle).ok_or(Error::Denied)?;
            session.capability.seal();
            let queued = control.try_send(Control::Close { answer });
            session.stop.cancel();
            queued
        };
        queued.map_err(|_| Error::Busy)?;
        receive.await.map_err(|_| Error::Cancelled)?
    }
}
struct Resources {
    broker: Broker,
    owner: WindowOwner,
    socket: Option<NativeSocket>,
}
impl Resources {
    async fn cleanup(&mut self) -> Result<(), Error> {
        // Physical socket is destroyed before logout, responses or bookkeeping.
        self.socket.take();
        let result = self.broker.logout(self.owner).await;
        let _ = self.broker.destroy_native_window(self.owner);
        result
    }
}
async fn setup(
    shared: &Shared,
    fixture: &OwnedFixture,
    job: &mut Open,
) -> Result<(Resources, NativeBinding, Instant), Error> {
    shared.current(&job.window)?;
    let profile = fixture.native_profile(job.role);
    let broker = Broker::qualification_fixture(
        profile.origin().as_str().trim_end_matches('/').into(),
        profile.community_id().uuid().to_string(),
        profile.expected_account().uuid(),
        profile.authority_pem().to_vec(),
    )?;
    let owner = broker.register_native_window("main", WindowRole::Main)?;
    let mut r = Resources {
        broker,
        owner,
        socket: None,
    };
    let result = tokio::select! {biased;
        _=job.window.cancelled.cancelled()=>Err(Error::Stale),
        _=job.answer.closed()=>Err(Error::Cancelled),
        result=async{
        r.broker.select_confirmed_profile(owner,profile.origin().as_str(),&profile.community_id().uuid().to_string()).await?;
        shared.current(&job.window)?;
        r.broker.login(owner,profile.username(),Password::from_native_input(profile.password().to_owned())?).await.inspect_err(|_error|{#[cfg(test)] eprintln!("native fixture stage=login error={_error:?}");})?;
        #[cfg(test)]
        {
            let hook=shared.after_login.lock().map_err(|_|Error::Internal)?.take();
            if let Some((entered,release))=hook {let _=entered.send(());release.notified().await;}
        }
        shared.current(&job.window)?;
        r.socket=Some(r.broker.open_native_socket(owner).await.inspect_err(|_error|{#[cfg(test)] eprintln!("native fixture stage=socket error={_error:?}");})?);
        shared.current(&job.window)?;
        let generation=shared.generation.try_update(Ordering::AcqRel,Ordering::Acquire,|n|n.checked_add(1).filter(|n|*n<=9_007_199_254_740_991)).map_err(|_|Error::Exhausted)?+1;
        let(binding,lease)=r.broker.native_fixture_binding(owner,r.socket.as_ref().ok_or(Error::Internal)?,job.window.context,Uuid::new_v4(),generation)?;
        // This qualification does not implement refresh; hard stop within120s is deliberate.
        Ok((binding,lease.min(Instant::now()+Duration::from_secs(120))))
    }=>result};
    match result {
        Ok((binding, deadline)) => Ok((r, binding, deadline)),
        Err(error) => {
            let _ = r.cleanup().await;
            Err(error)
        }
    }
}
async fn run(shared: Arc<Shared>, fixture: Arc<OwnedFixture>, mut job: Open) {
    let prepared = setup(&shared, &fixture, &mut job).await;
    let (mut r, binding, deadline) = match prepared {
        Ok(v) => v,
        Err(e) => {
            shared.release(job.window.context, job.role, None);
            let _ = job.answer.send(Err(e));
            return;
        }
    };
    let cap = match FixtureCapability::bind(&fixture, job.role, binding, deadline) {
        Ok(c) => Arc::new(c),
        Err(_) => {
            let _ = r.cleanup().await;
            shared.release(job.window.context, job.role, None);
            let _ = job.answer.send(Err(Error::Denied));
            return;
        }
    };
    let bound = cap.bound();
    let handle = bound.handle.clone();
    let (sender, mut commands) = mpsc::channel(16);
    let stop = CancellationToken::new();
    if let Ok(mut s) = shared.state.lock() {
        s.sessions.insert(
            handle.clone(),
            SessionControl {
                window: job.window.context,
                generation: bound.generation,
                capability: cap.clone(),
                sender,
                stop: stop.clone(),
            },
        );
    } else {
        cap.seal();
        let _ = r.cleanup().await;
        shared.release(job.window.context, job.role, None);
        let _ = job.answer.send(Err(Error::Internal));
        return;
    }
    let mut cursor = EventCursor::default();
    if shared.current(&job.window).is_err() || job.answer.send(Ok(bound)).is_err() {
        cap.seal();
        let _ = r.cleanup().await;
        shared.release(job.window.context, job.role, Some(&handle));
        return;
    }
    let mut close_answers = vec![];
    let mut failed = false;
    loop {
        enum Next {
            Stop,
            Expired,
            Control(Option<Control>),
            Poll(Result<Option<mnema_private_synthetic_media_policy::MediaMessage>, Error>),
        }
        let next = {
            let socket = r
                .socket
                .as_mut()
                .expect("native actor owns socket while active");
            if job.window.cancelled.is_cancelled() || stop.is_cancelled() {
                Next::Stop
            } else if Instant::now() >= deadline {
                Next::Expired
            } else if let Ok(command) = commands.try_recv() {
                Next::Control(Some(command))
            } else {
                // Broker poll already bounds the read wait to100ms. A queued command
                // cannot cancel a partially written Pong/renewal control. Only a
                // terminal close/window/deadline may interrupt; that drops socket.
                tokio::select! {biased;
                    _=job.window.cancelled.cancelled()=>Next::Stop,
                    _=stop.cancelled()=>Next::Stop,
                    _=tokio::time::sleep_until(tokio::time::Instant::from_std(deadline))=>Next::Expired,
                    message=r.broker.native_fixture_poll(socket)=>Next::Poll(message),
                }
            }
        };
        match next {
            Next::Stop => break,
            Next::Expired => {
                failed = true;
                break;
            }
            Next::Control(Some(Control::Close { answer })) => {
                close_answers.push(answer);
                break;
            }
            Next::Control(None) => break,
            Next::Control(Some(Control::Send { action, answer })) => {
                if answer.is_closed() {
                    continue;
                }
                let result = match shared.current(&job.window) {
                    Err(e) => Err(e),
                    Ok(()) => {
                        tokio::select! {biased;_=job.window.cancelled.cancelled()=>Err(Error::Stale),_=stop.cancelled()=>Err(Error::Cancelled),_=tokio::time::sleep_until(tokio::time::Instant::from_std(deadline))=>Err(Error::Expired),result=r.broker.native_fixture_send(r.owner,r.socket.as_mut().expect("active socket"),&cap,&binding,action)=>result}
                    }
                };
                let result = result.and_then(|()| shared.current(&job.window));
                if result.is_err() {
                    cap.seal();
                    r.socket.take();
                    failed = true;
                    let _ = answer.send(result);
                    break;
                }
                let _ = answer.send(Ok(()));
            }
            Next::Poll(Ok(None)) => {}
            Next::Poll(Ok(Some(message))) => {
                let current = r
                    .broker
                    .native_fixture_binding(
                        r.owner,
                        r.socket.as_ref().expect("active socket"),
                        binding.window_context.uuid(),
                        binding.profile_context.uuid(),
                        binding.socket_generation,
                    )
                    .map(|v| v.0);
                let event = current.and_then(|current| {
                    cursor
                        .message(&cap, Some(&current), Instant::now(), message)
                        .map(|event| (event, current))
                        .map_err(|_| Error::Denied)
                });
                #[cfg(test)]
                {
                    let hook = shared.before_message_enqueue.lock().unwrap().take();
                    if let Some((entered, release)) = hook {
                        let _ = entered.send(());
                        release.wait();
                    }
                }
                if event
                    .and_then(|(event, current)| {
                        shared.publish(&job.window, &job.sink, event, Some((&cap, &current)))
                    })
                    .is_err()
                {
                    failed = !stop.is_cancelled() && !job.window.cancelled.is_cancelled();
                    break;
                }
            }
            Next::Poll(Err(_)) => {
                failed = true;
                break;
            }
        }
    }
    cap.seal();
    r.socket.take();
    // Drop queued actions before cleanup; close waiters receive the same committed result.
    while let Ok(command) = commands.try_recv() {
        match command {
            Control::Close { answer } => close_answers.push(answer),
            Control::Send { answer, .. } => {
                let _ = answer.send(Err(Error::Stale));
            }
        }
    }
    let cleanup = r.cleanup().await;
    if cleanup.is_err() {
        failed = true
    }
    if shared.current(&job.window).is_ok() {
        let event = if failed {
            cursor.error(&cap)
        } else {
            cursor.closed(&cap)
        };
        if let Ok(event) = event {
            let _ = shared.publish(&job.window, &job.sink, event, None);
        }
    }
    shared.release(job.window.context, job.role, Some(&handle));
    for answer in close_answers {
        let _ = answer.send(cleanup);
    }
}

#[cfg(all(test, unix))]
mod tests;
