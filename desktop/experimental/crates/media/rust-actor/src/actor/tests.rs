use super::*;
use mnema_private_synthetic_media_policy::{Id, load_owned_bootstrap};
use std::{
    fs,
    os::unix::fs::PermissionsExt,
    path::{Path, PathBuf},
    process::{Child, Command, Stdio},
};
struct Directory(PathBuf);
impl Directory {
    fn new() -> Self {
        let p = std::env::temp_dir().join(format!("mnema-native-actor-{}", Uuid::new_v4()));
        fs::create_dir(&p).unwrap();
        fs::set_permissions(&p, fs::Permissions::from_mode(0o700)).unwrap();
        Self(p)
    }
    fn bootstrap(&self) -> PathBuf {
        self.0.join("bootstrap.json")
    }
}
impl Drop for Directory {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}
struct Daemon(Child);
impl Drop for Daemon {
    fn drop(&mut self) {
        unsafe {
            libc::kill(self.0.id() as i32, libc::SIGINT);
        };
        for _ in 0..100 {
            if self.0.try_wait().ok().flatten().is_some() {
                return;
            }
            std::thread::sleep(Duration::from_millis(50));
        }
        let _ = self.0.kill();
        let _ = self.0.wait();
    }
}
fn static_fixture() -> (Directory, OwnedFixture) {
    let d = Directory::new();
    let v = serde_json::json!({"schema":1,"scope":"synthetic native authenticated SFU qualification, no MLS grant","origin":"https://127.0.0.1:8443","ca_pem":include_str!("../../../rust/src/test-ca.fixture.txt"),"community_id":Uuid::new_v4().to_string(),"channel_id":Uuid::new_v4().to_string(),"publisher":{"user_id":Uuid::new_v4().to_string(),"username":"nws-public-fixture","password":"native-fixture-password"},"viewer":{"user_id":Uuid::new_v4().to_string(),"username":"media-public-fixture","password":"native-fixture-password"}});
    fs::write(d.bootstrap(), serde_json::to_vec(&v).unwrap()).unwrap();
    fs::set_permissions(d.bootstrap(), fs::Permissions::from_mode(0o600)).unwrap();
    let fixture = load_owned_bootstrap(&d.bootstrap()).unwrap();
    (d, fixture)
}
#[test]
fn actual_native_window_replacement_denies_old_incarnation() {
    let (_d, f) = static_fixture();
    let actor = MediaActor::fixture(f).unwrap();
    let old = actor.native_attach_window().unwrap();
    assert!(old.same_native_context(&old.clone()));
    let new = actor.native_attach_window().unwrap();
    assert!(!old.same_native_context(&new));
    assert!(!old.same_native_context(&old.clone()));
    assert_eq!(actor.check_window(&old), Err(Error::Stale));
    assert!(actor.check_window(&new).is_ok());
    assert_eq!(actor.shared.current(&old), Err(Error::Stale));
    assert!(actor.shared.current(&new).is_ok());
    assert_eq!(actor.native_detach_window(&old), Err(Error::Stale));
    actor.native_detach_window(&new).unwrap();
    assert_eq!(actor.shared.current(&new), Err(Error::Stale));
}

#[test]
fn concurrent_last_public_owner_drops_seal_before_worker_can_react() {
    let (_d, f) = static_fixture();
    let actor = MediaActor::fixture(f).unwrap();
    let window = actor.native_attach_window().unwrap();
    let shared = actor.shared.clone();
    let other = actor.clone();
    let (entered, held) = std::sync::mpsc::channel();
    let release = Arc::new(std::sync::Barrier::new(2));
    *shared.worker_before_final_seal.lock().unwrap() = Some((entered, release.clone()));
    let simultaneous = Arc::new(std::sync::Barrier::new(3));
    let first = simultaneous.clone();
    let second = simultaneous.clone();
    let a = std::thread::spawn(move || {
        first.wait();
        drop(actor)
    });
    let b = std::thread::spawn(move || {
        second.wait();
        drop(other)
    });
    simultaneous.wait();
    a.join().unwrap();
    b.join().unwrap();
    held.recv_timeout(Duration::from_secs(3)).unwrap();
    // Worker fallback is deliberately suspended before its seal; this result
    // must come synchronously from the atomic last public owner destructor.
    assert!(window.cancelled.is_cancelled());
    assert!(shared.state.lock().unwrap().window.is_none());
    release.wait();
}
#[test]
fn registered_sink_is_not_called_after_native_detach() {
    let (_d, f) = static_fixture();
    let actor = MediaActor::fixture(f).unwrap();
    let window = actor.native_attach_window().unwrap();
    let calls = Arc::new(AtomicU64::new(0));
    let count = calls.clone();
    let sink: EventSink = Arc::new(move |_| {
        count.fetch_add(1, Ordering::AcqRel);
        Ok(())
    });
    let event = || NativeMediaEvent::Closed {
        handle: "a".repeat(64),
        generation: 1,
        sequence: 1,
    };
    actor.shared.publish(&window, &sink, event(), None).unwrap();
    actor.native_detach_window(&window).unwrap();
    assert_eq!(
        actor.shared.publish(&window, &sink, event(), None),
        Err(Error::Stale)
    );
    assert_eq!(calls.load(Ordering::Acquire), 1);
}
#[tokio::test(flavor = "current_thread")]
async fn forged_or_old_handles_cannot_send_or_close() {
    let (_d, f) = static_fixture();
    let actor = MediaActor::fixture(f).unwrap();
    let window = actor.native_attach_window().unwrap();
    assert_eq!(
        actor.send(&window, &"0".repeat(64), 1, Action::Leave).await,
        Err(Error::Denied)
    );
    assert_eq!(
        actor.close(&window, &"0".repeat(64), 1).await,
        Err(Error::Denied)
    );
    actor.native_detach_window(&window).unwrap();
    assert_eq!(
        actor.send(&window, &"0".repeat(64), 1, Action::Leave).await,
        Err(Error::Stale)
    );
}
#[tokio::test(flavor = "current_thread")]
async fn actual_verified_native_tls_wss_two_roles_offer_and_committed_logout() {
    let d = Directory::new();
    let binary = Path::new(env!("CARGO_MANIFEST_DIR")).join("../native-media-backend-fixture");
    assert!(
        binary.is_file(),
        "actual backend fixture must be built before this qualification"
    );
    let child = Command::new(binary)
        .env("MNEMA_SYNTHETIC_NATIVE_MEDIA_FIXTURE", "1")
        .env("MNEMA_SYNTHETIC_NATIVE_MEDIA_BOOTSTRAP", d.bootstrap())
        .args([
            "-test.run",
            "^TestNativeMediaOperationalFixture$",
            "-test.timeout",
            "60s",
        ])
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .unwrap();
    let mut daemon = Daemon(child);
    let begin = Instant::now();
    while !d.bootstrap().exists() {
        assert!(
            begin.elapsed() < Duration::from_secs(30),
            "real backend startup deadline"
        );
        assert!(
            daemon.0.try_wait().unwrap().is_none(),
            "real fixture exited before readiness"
        );
        tokio::time::sleep(Duration::from_millis(50)).await;
    }
    let fixture = load_owned_bootstrap(&d.bootstrap()).unwrap();
    let channel = fixture.channel_id();
    let actor = MediaActor::fixture(fixture).unwrap();
    let mut window = actor.native_attach_window().unwrap();
    let (tx, mut rx) = tokio::sync::mpsc::unbounded_channel();
    let sink: EventSink = Arc::new(move |event| tx.send(event).map_err(|_| ()));
    let publisher = tokio::time::timeout(
        Duration::from_secs(10),
        actor.open(&window, Role::Publisher, sink.clone()),
    )
    .await
    .unwrap()
    .unwrap();
    let viewer = tokio::time::timeout(
        Duration::from_secs(10),
        actor.open(&window, Role::Viewer, sink.clone()),
    )
    .await
    .unwrap()
    .unwrap();
    assert_ne!(publisher.handle, viewer.handle);
    assert_ne!(publisher.self_user_id, viewer.self_user_id);
    assert_eq!(publisher.channel_id, channel);
    assert_eq!(viewer.publisher_user_id, publisher.self_user_id);
    actor
        .send(
            &window,
            &publisher.handle,
            publisher.generation,
            Action::Join {
                channel_id: channel,
            },
        )
        .await
        .unwrap();
    actor
        .send(
            &window,
            &viewer.handle,
            viewer.generation,
            Action::Join {
                channel_id: channel,
            },
        )
        .await
        .unwrap();
    let mut offers = vec![];
    let deadline = tokio::time::Instant::now() + Duration::from_secs(8);
    while offers.len() < 2 {
        let event = tokio::time::timeout_at(deadline, rx.recv())
            .await
            .unwrap()
            .unwrap();
        match event {
            NativeMediaEvent::Message {
                handle,
                message: mnema_private_synthetic_media_policy::MediaMessage::Offer(offer),
                ..
            } => {
                assert!(offer.sdp.starts_with("v=0"));
                if !offers.contains(&handle) {
                    offers.push(handle)
                }
            }
            NativeMediaEvent::Error { .. } => panic!("actual native media actor failed"),
            _ => {}
        }
    }
    // Backend generated real Pion offers through two real, separately authenticated native actors.
    let wrong = Id::native(Uuid::new_v4()).unwrap();
    assert_eq!(
        actor
            .send(
                &window,
                &viewer.handle,
                viewer.generation,
                Action::Join { channel_id: wrong }
            )
            .await,
        Err(Error::Denied)
    );
    // Rejected action is fail-closed and destroys that socket before returning; fresh roles can be reopened after cleanup.
    let wait = Instant::now();
    while actor
        .shared
        .state
        .lock()
        .unwrap()
        .reservations
        .iter()
        .any(|(_, r)| *r == Role::Viewer)
    {
        assert!(wait.elapsed() < Duration::from_secs(8));
        tokio::time::sleep(Duration::from_millis(20)).await;
    }
    actor
        .close(&window, &publisher.handle, publisher.generation)
        .await
        .unwrap();
    assert!(actor.shared.state.lock().unwrap().sessions.is_empty());
    assert!(actor.shared.state.lock().unwrap().reservations.is_empty());
    // Pause after a genuine committed native HTTP login, then replace actual
    // native window while setup awaits. Cancellation must revoke/drop resources,
    // deny the old publication and never create the late WSS socket.
    let (entered, mut held) = tokio::sync::mpsc::unbounded_channel();
    let release = Arc::new(tokio::sync::Notify::new());
    *actor.shared.after_login.lock().unwrap() = Some((entered, release.clone()));
    let old_window = window.clone();
    let opening_actor = actor.clone();
    let opening_sink = sink.clone();
    let pending = tokio::spawn(async move {
        opening_actor
            .open(&old_window, Role::Publisher, opening_sink)
            .await
    });
    tokio::time::timeout(Duration::from_secs(10), held.recv())
        .await
        .unwrap()
        .unwrap();
    window = actor.native_attach_window().unwrap();
    assert!(matches!(
        tokio::time::timeout(Duration::from_secs(8), pending)
            .await
            .unwrap()
            .unwrap(),
        Err(Error::Stale)
    ));
    release.notify_waiters();
    assert!(actor.shared.state.lock().unwrap().sessions.is_empty());
    assert!(actor.shared.state.lock().unwrap().reservations.is_empty());
    // Genuine callback race: validate an actual Pion offer, suspend immediately
    // before enqueue, then seal via real actor close on another thread. Enqueue
    // must recheck the capability under the exact same revocation mutex.
    let (entered, mut held) = tokio::sync::mpsc::unbounded_channel();
    let release = Arc::new(std::sync::Barrier::new(2));
    *actor.shared.before_message_enqueue.lock().unwrap() = Some((entered, release.clone()));
    let enqueued = Arc::new(AtomicU64::new(0));
    let count = enqueued.clone();
    let race_sink: EventSink = Arc::new(move |event| {
        if matches!(event, NativeMediaEvent::Message { .. }) {
            count.fetch_add(1, Ordering::AcqRel);
        }
        Ok(())
    });
    let raced = actor
        .open(&window, Role::Publisher, race_sink)
        .await
        .unwrap();
    actor
        .send(
            &window,
            &raced.handle,
            raced.generation,
            Action::Join {
                channel_id: channel,
            },
        )
        .await
        .unwrap();
    tokio::time::timeout(Duration::from_secs(8), held.recv())
        .await
        .unwrap()
        .unwrap();
    let closing_actor = actor.clone();
    let closing_window = window.clone();
    let closing_handle = raced.handle.clone();
    let closing = tokio::spawn(async move {
        closing_actor
            .close(&closing_window, &closing_handle, raced.generation)
            .await
    });
    let wait = Instant::now();
    while !actor
        .shared
        .state
        .lock()
        .unwrap()
        .sessions
        .get(&raced.handle)
        .unwrap()
        .stop
        .is_cancelled()
    {
        assert!(wait.elapsed() < Duration::from_secs(2));
        tokio::time::sleep(Duration::from_millis(5)).await;
    }
    release.wait();
    tokio::time::timeout(Duration::from_secs(8), closing)
        .await
        .unwrap()
        .unwrap()
        .unwrap();
    assert_eq!(
        enqueued.load(Ordering::Acquire),
        0,
        "no actual offer/ICE enqueue may cross close seal"
    );
    let refusing: EventSink = Arc::new(|_| Err(()));
    let refused = actor.open(&window, Role::Viewer, refusing).await.unwrap();
    actor
        .send(
            &window,
            &refused.handle,
            refused.generation,
            Action::Join {
                channel_id: channel,
            },
        )
        .await
        .unwrap();
    let wait = Instant::now();
    while actor
        .shared
        .state
        .lock()
        .unwrap()
        .reservations
        .iter()
        .any(|(_, r)| *r == Role::Viewer)
    {
        assert!(
            wait.elapsed() < Duration::from_secs(8),
            "refused actual Pion offer must seal/socketdrop/logout"
        );
        tokio::time::sleep(Duration::from_millis(20)).await;
    }
    let reopened = actor.open(&window, Role::Viewer, sink).await.unwrap();
    actor
        .send(
            &window,
            &reopened.handle,
            reopened.generation,
            Action::Join {
                channel_id: channel,
            },
        )
        .await
        .unwrap();
    let shared = actor.shared.clone();
    drop(actor);
    // Last native actor owner synchronously seals/cancels, while its LocalSet
    // remains alive to finish genuine logout and remove the session reservation.
    assert!(window.cancelled.is_cancelled());
    assert!(shared.state.lock().unwrap().window.is_none());
    let wait = Instant::now();
    while !shared.state.lock().unwrap().reservations.is_empty() {
        assert!(wait.elapsed() < Duration::from_secs(8));
        tokio::time::sleep(Duration::from_millis(20)).await;
    }
    assert!(shared.state.lock().unwrap().sessions.is_empty());
    unsafe {
        libc::kill(daemon.0.id() as i32, libc::SIGINT);
    };
    let end = Instant::now();
    loop {
        if let Some(status) = daemon.0.try_wait().unwrap() {
            assert!(status.success());
            break;
        }
        assert!(end.elapsed() < Duration::from_secs(8));
        tokio::time::sleep(Duration::from_millis(50)).await;
    }
    assert!(!d.bootstrap().exists());
}

#[test]
fn registered_sink_refusal_fails_closed_without_retry_or_state_mutation() {
    let (_d, f) = static_fixture();
    let actor = MediaActor::fixture(f).unwrap();
    let window = actor.native_attach_window().unwrap();
    let calls = Arc::new(AtomicU64::new(0));
    let c = calls.clone();
    let sink: EventSink = Arc::new(move |_| {
        c.fetch_add(1, Ordering::AcqRel);
        Err(())
    });
    let event = NativeMediaEvent::Closed {
        handle: "a".repeat(64),
        generation: 1,
        sequence: 1,
    };
    assert_eq!(
        actor.shared.publish(&window, &sink, event, None),
        Err(Error::Cancelled)
    );
    assert_eq!(calls.load(Ordering::Acquire), 1);
    assert!(actor.shared.current(&window).is_ok());
}
