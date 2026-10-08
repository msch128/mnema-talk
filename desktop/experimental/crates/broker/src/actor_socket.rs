//! Only the LocalSet owns a socket and its Running operation. Every terminal path
//! drops transport custody before reporting closure or allowing auth drains.
use super::*;
use std::time::Duration;
#[derive(Serialize)]
pub struct NativeSocketNotice {
    pub context: String,
    pub handle: String,
    pub sequence: u64,
    pub kind: &'static str,
    pub payload: serde_json::Value,
}
impl fmt::Debug for NativeSocketNotice {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("NativeSocketNotice(REDACTED)")
    }
}
pub type NativeSocketObserver =
    Arc<dyn Fn(PublicationScope, NativeSocketNotice) -> bool + Send + Sync>;
pub(super) struct SocketOpened {
    pub scope: PublicationScope,
    pub handle: Uuid,
    pub cancel: CancellationToken,
    pub armed: bool,
}
impl Drop for SocketOpened {
    fn drop(&mut self) {
        if self.armed {
            self.cancel.cancel();
        }
    }
}
pub(super) struct SocketControl {
    pub lease: NativeWindowLease,
    pub intent: u64,
    pub cancel: CancellationToken,
    pub sender: mpsc::Sender<SocketCommand>,
}
pub(super) struct SocketCommand {
    action: NativeSocketAction,
    answer: oneshot::Sender<Result<(), Error>>,
}
impl NativeClient {
    /// Native enqueue closure must not call back into client/broker. Window and
    /// profile mutation remain locked through the actual Channel enqueue.
    pub fn with_socket_publication<T>(
        &self,
        lease: &NativeWindowLease,
        scope: PublicationScope,
        publish: impl FnOnce() -> T,
    ) -> Result<T, Error> {
        let window = self.shared.window.lock().map_err(|_| Error::Internal)?;
        if lease.cancel.is_cancelled()
            || window
                .as_ref()
                .is_none_or(|w| w.context != lease.context || w.owner != lease.owner)
        {
            return Err(Error::Stale);
        }
        self.shared
            .broker
            .with_publication(lease.owner, scope, publish)
    }
    pub fn check_socket_publication(
        &self,
        lease: &NativeWindowLease,
        scope: PublicationScope,
    ) -> Result<(), Error> {
        self.shared.current(lease)?;
        self.shared
            .broker
            .check_publication_scope(lease.owner, scope)
    }
    pub async fn socket_send(
        &self,
        lease: &NativeWindowLease,
        handle: Uuid,
        action: NativeSocketAction,
    ) -> Result<(), Error> {
        let (sender, cancel) = {
            self.shared.current(lease)?;
            let sockets = self.shared.sockets.lock().map_err(|_| Error::Internal)?;
            let entry = sockets.get(&handle).ok_or(Error::Stale)?;
            if entry.lease.context != lease.context || entry.lease.owner != lease.owner {
                return Err(Error::Denied);
            }
            self.shared.ready(lease, entry.intent)?;
            (entry.sender.clone(), entry.cancel.clone())
        };
        let (answer, receive) = oneshot::channel();
        sender
            .try_send(SocketCommand { action, answer })
            .map_err(|_| Error::Busy)?;
        tokio::select! {biased;_=lease.cancel.cancelled()=>Err(Error::Stale),_=cancel.cancelled()=>Err(Error::Cancelled),r=receive=>r.map_err(|_|Error::Cancelled)?}
    }
    pub fn socket_close(&self, lease: &NativeWindowLease, handle: Uuid) -> Result<(), Error> {
        self.shared.current(lease)?;
        let sockets = self.shared.sockets.lock().map_err(|_| Error::Internal)?;
        let entry = sockets.get(&handle).ok_or(Error::Stale)?;
        if entry.lease.context != lease.context || entry.lease.owner != lease.owner {
            return Err(Error::Denied);
        }
        entry.cancel.cancel();
        Ok(())
    }
}
pub(super) fn cancel_sockets(shared: &Shared, context: Uuid) {
    if let Ok(sockets) = shared.sockets.lock() {
        for entry in sockets.values().filter(|e| e.lease.context == context) {
            entry.cancel.cancel();
        }
    }
}
pub(super) async fn open(
    shared: Arc<Shared>,
    lease: NativeWindowLease,
    intent: u64,
    observer: NativeSocketObserver,
) -> Result<Value, Error> {
    shared.ready(&lease, intent)?;
    // One metadata socket for this exact main incarnation; never create hidden
    // reconnect sockets or replay an ambiguous handshake.
    if !shared
        .sockets
        .lock()
        .map_err(|_| Error::Internal)?
        .is_empty()
    {
        return Err(Error::Busy);
    }
    let socket = shared.broker.open_native_socket(lease.owner).await?;
    let scope = shared.broker.selected_publication_scope(lease.owner)?;
    shared.ready(&lease, intent)?;
    shared.broker.check_publication_scope(lease.owner, scope)?;
    let handle = Uuid::new_v4();
    let cancel = CancellationToken::new();
    let (sender, receive) = mpsc::channel(16);
    {
        let mut sockets = shared.sockets.lock().map_err(|_| Error::Internal)?;
        if !sockets.is_empty() {
            return Err(Error::Busy);
        }
        sockets.insert(
            handle,
            SocketControl {
                lease: lease.clone(),
                intent,
                cancel: cancel.clone(),
                sender,
            },
        );
    }
    let publication_cancel = cancel.clone();
    tokio::task::spawn_local(drive(
        SocketOwner {
            shared,
            lease,
            intent,
            handle,
            cancel,
            observer,
            initial: scope,
        },
        socket,
        receive,
    ));
    Ok(Value::SocketOpened(SocketOpened {
        scope,
        handle,
        cancel: publication_cancel,
        armed: true,
    }))
}
struct Custody {
    socket: Option<NativeSocket>,
    answer: Option<oneshot::Sender<Result<(), Error>>>,
}
impl Drop for Custody {
    fn drop(&mut self) {
        drop(self.socket.take());
        if let Some(answer) = self.answer.take() {
            let _ = answer.send(Err(Error::Cancelled));
        }
    }
}
struct SocketOwner {
    shared: Arc<Shared>,
    lease: NativeWindowLease,
    intent: u64,
    handle: Uuid,
    cancel: CancellationToken,
    observer: NativeSocketObserver,
    initial: PublicationScope,
}
async fn drive(
    owner: SocketOwner,
    socket: NativeSocket,
    mut receive: mpsc::Receiver<SocketCommand>,
) {
    let SocketOwner {
        shared,
        lease,
        intent,
        handle,
        cancel,
        observer,
        initial,
    } = owner;
    let mut custody = Custody {
        socket: Some(socket),
        answer: None,
    };
    let mut seq = 0u64;
    let mut last_scope = initial;
    let notify = |scope: PublicationScope, seq: &mut u64, kind, payload| -> Result<(), Error> {
        *seq = seq
            .checked_add(1)
            .filter(|n| *n <= 9_007_199_254_740_991)
            .ok_or(Error::Exhausted)?;
        shared.ready(&lease, intent)?;
        shared.broker.check_publication_scope(lease.owner, scope)?;
        if !(observer)(
            scope,
            NativeSocketNotice {
                context: lease.context_nonce(),
                handle: handle.hyphenated().to_string(),
                sequence: *seq,
                kind,
                payload,
            },
        ) {
            return Err(Error::Stale);
        }
        Ok(())
    };
    let result:Result<(),Error>=async {
        notify(initial,&mut seq,"opened",serde_json::Value::Null)?;
        loop {
            shared.ready(&lease,intent)?;
            if cancel.is_cancelled(){return Err(Error::Cancelled)}
            if shared.broker.socket_needs_refresh(custody.socket.as_ref().unwrap())? {
                // Refresh ownership remains native and single-flight. Cancelling
                // this actor drops the rotation future and quarantines lost ACK.
                tokio::select!{biased;_=cancel.cancelled()=>return Err(Error::Cancelled),_=lease.cancel.cancelled()=>return Err(Error::Stale),r=shared.broker.refresh(lease.owner)=>r?};
            }
            let scope=shared.broker.selected_publication_scope(lease.owner)?;last_scope=scope;
            tokio::select!{biased;
                _=cancel.cancelled()=>return Err(Error::Cancelled),
                _=lease.cancel.cancelled()=>return Err(Error::Stale),

                event=shared.broker.socket_poll(custody.socket.as_mut().unwrap())=>{if let Some(event)=event?{
                    shared.broker.commit_socket_event(lease.owner,custody.socket.as_ref().unwrap(),event.clone())?;
                    let payload=serde_json::to_value(event).map_err(|_|Error::Internal)?;
                    notify(scope,&mut seq,"message",payload)?;
                }}
            }
            if let Ok(command)=receive.try_recv(){
                custody.answer=Some(command.answer);
                let result=tokio::select!{biased;_=cancel.cancelled()=>return Err(Error::Cancelled),_=lease.cancel.cancelled()=>return Err(Error::Stale),r=shared.broker.socket_send(lease.owner,custody.socket.as_mut().unwrap(),command.action)=>r};
                result?;
                if let Some(answer)=custody.answer.take(){let _=answer.send(result);}
            }
            // This yield also gives lifecycle/auth jobs a chance to cancel and
            // release the one owned socket before any request drain proceeds.
            tokio::time::sleep(Duration::from_millis(1)).await;
        }
    }.await;
    #[cfg(test)]
    if let Some((started, barrier)) = shared.custody_before_drop.lock().unwrap().take() {
        let _ = started.send(());
        barrier.wait();
    }
    drop(custody.socket.take()); // Running operation gone BEFORE any error reply/publication.
    if let Ok(mut sockets) = shared.sockets.lock() {
        sockets.remove(&handle);
    }
    if let Some(answer) = custody.answer.take() {
        let _ = answer.send(result);
    }
    if result.is_err() {
        let _ = notify(
            last_scope,
            &mut seq,
            "closed",
            serde_json::json!({"code":"NATIVE_SOCKET_CLOSED"}),
        );
    }
}
