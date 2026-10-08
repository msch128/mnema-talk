use super::*;
use crate::{broker::Broker, journal::Fault, secret::Secret};
use serde_json::{Value, json};
use std::{
    fs,
    io::{BufRead, BufReader},
    path::PathBuf,
    process::{Child, Command, Stdio},
    sync::Arc,
    time::Duration,
};
use tokio::task::LocalSet;
use uuid::Uuid;
const LOGIN: &str = "/api/native/v1/auth/login";
const REFRESH: &str = "/api/native/v1/auth/refresh";
const ME: &str = "/api/native/v1/auth/me";
const LOGOUT: &str = "/api/native/v1/auth/logout";
struct Fixture {
    child: Child,
    path: PathBuf,
    port: u16,
    port2: u16,
    root: Vec<u8>,
}
impl Fixture {
    fn start() -> Self {
        let base = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
        let parent = base.join("test-runtime");
        fs::create_dir_all(&parent).unwrap();
        let path = parent.join(Uuid::new_v4().hyphenated().to_string());
        let child = Command::new("python3")
            .arg(base.join("tests/https_fixture.py"))
            .arg(&path)
            .stdout(Stdio::piped())
            .stderr(Stdio::null())
            .spawn()
            .unwrap();
        let mut fixture = Self {
            child,
            path,
            port: 0,
            port2: 0,
            root: Vec::new(),
        };
        let stdout = fixture.child.stdout.take().unwrap();
        let mut line = String::new();
        BufReader::new(stdout).read_line(&mut line).unwrap();
        let info: Value = serde_json::from_str(&line).expect("local fixture startup failed");
        fixture.port = info["port"].as_u64().unwrap() as u16;
        fixture.port2 = info["port2"].as_u64().unwrap() as u16;
        fixture.root = fs::read(fixture.path.join("ca.pem")).unwrap();
        fixture
    }
    fn origin(&self) -> String {
        format!("https://127.0.0.1:{}", self.port)
    }
    fn second_origin(&self) -> String {
        format!("https://127.0.0.1:{}", self.port2)
    }
    fn control(&self, path: &str, mode: &str) {
        fs::write(
            self.path.join("control.tmp"),
            serde_json::to_vec(&json!({path:{"mode":mode}})).unwrap(),
        )
        .unwrap();
        fs::rename(
            self.path.join("control.tmp"),
            self.path.join("control.json"),
        )
        .unwrap();
    }
    fn redirect_to_second(&self) {
        fs::write(self.path.join("control.json"),serde_json::to_vec(&json!({LOGIN:{"mode":"redirect","location":format!("{}{}",self.second_origin(),LOGIN)}})).unwrap()).unwrap();
    }
    fn release(&self) {
        fs::write(self.path.join("release"), b"owned fixture gate").unwrap();
    }
    fn state(&self) -> Value {
        serde_json::from_slice(&fs::read(self.path.join("state.json")).unwrap()).unwrap()
    }
    fn calls(&self, path: &str) -> Vec<Value> {
        self.state()["calls"]
            .as_array()
            .unwrap()
            .iter()
            .filter(|call| call["path"] == path)
            .cloned()
            .collect()
    }
    async fn wait(&self, path: &str, count: usize) {
        tokio::time::timeout(Duration::from_secs(5), async {
            while self.calls(path).len() < count {
                tokio::time::sleep(Duration::from_millis(5)).await;
            }
        })
        .await
        .expect("fixture request did not arrive");
    }
}
impl Drop for Fixture {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
        let _ = fs::remove_dir_all(&self.path);
    }
}
fn password() -> Password {
    Password::from_native_input("native-fixture-password".to_owned()).unwrap()
}
async fn selected(f: &Fixture) -> (Arc<Broker>, WindowOwner) {
    let b = Arc::new(Broker::fixture(f.root.clone()));
    let owner = b.register_native_window("main", WindowRole::Main).unwrap();
    b.select_confirmed_profile(owner, &f.origin(), "fixture-community")
        .await
        .unwrap();
    (b, owner)
}
async fn logged(f: &Fixture) -> (Arc<Broker>, WindowOwner) {
    let (b, w) = selected(f).await;
    b.login(w, "fixture", password()).await.unwrap();
    (b, w)
}

#[tokio::test(flavor = "current_thread")]
async fn real_https_login_me_refresh_logout_uses_fixed_routes_and_native_authority() {
    let f = Fixture::start();
    let (b, w) = selected(&f).await;
    assert!(!b.remembered_login_supported());
    assert_eq!(b.status().unwrap(), Status::Selected);
    let user = b.login(w, "fixture", password()).await.unwrap();
    assert_eq!(user.username, "fixture");
    assert_eq!(b.me(w).await.unwrap().account_id(), user.account_id());
    b.refresh(w).await.unwrap();
    assert_eq!(b.status().unwrap(), Status::Authenticated);
    assert_eq!(b.me(w).await.unwrap().username, "fixture");
    b.logout(w).await.unwrap();
    assert_eq!(b.status().unwrap(), Status::ReauthRequired);
    assert_eq!(b.me(w).await.unwrap_err(), Error::ReauthRequired);
    let calls = f.state()["calls"].as_array().unwrap().clone();
    assert_eq!(calls.len(), 5);
    for call in &calls {
        let h = call["headers"].as_object().unwrap();
        assert!(!h.keys().any(|k|["cookie","origin","referer"].contains(&k.to_ascii_lowercase().as_str())));
        let path = call["path"].as_str().unwrap();
        let auth = h
            .iter()
            .find(|(k, _)| k.eq_ignore_ascii_case("authorization"));
        assert_eq!(auth.is_some(), [ME, LOGOUT].contains(&path));
        if let Some((_, value)) = auth {
            assert!(value.as_str().unwrap().starts_with("Bearer "));
            assert_eq!(value.as_str().unwrap().len(), 50)
        }
        assert!(!path.contains('?'));
    }
    assert_eq!(f.calls(REFRESH).len(), 1);
    assert_eq!(f.state()["sequence"], 1);
    assert!(b.memory.phases().is_empty());
}

#[tokio::test(flavor = "current_thread")]
async fn untrusted_tls_root_and_hostname_mismatch_never_reach_http_handler() {
    let f = Fixture::start();
    let b = Broker::ephemeral().unwrap();
    let w = b.register_native_window("main", WindowRole::Main).unwrap();
    b.select_confirmed_profile(w, &f.origin(), "fixture-community")
        .await
        .unwrap();
    assert_eq!(
        b.login(w, "fixture", password()).await.unwrap_err(),
        Error::Network
    );
    assert!(f.calls(LOGIN).is_empty());
    let b = Broker::fixture(f.root.clone());
    let w = b.register_native_window("main", WindowRole::Main).unwrap();
    let address = format!("https://localhost:{}", f.port);
    b.select_confirmed_profile(w, &address, "fixture-community")
        .await
        .unwrap();
    assert_eq!(
        b.login(w, "fixture", password()).await.unwrap_err(),
        Error::Network
    );
    assert!(f.calls(LOGIN).is_empty());
}

#[tokio::test(flavor = "current_thread")]
async fn redirect_post_is_rejected_without_replay_or_cross_origin_forwarding() {
    let f = Fixture::start();
    f.redirect_to_second();
    let (b, w) = selected(&f).await;
    assert_eq!(
        b.login(w, "fixture", password()).await.unwrap_err(),
        Error::Redirect
    );
    let calls = f.calls(LOGIN);
    assert_eq!(calls.len(), 1);
    assert_eq!(calls[0]["port"], f.port);
    assert_eq!(b.status().unwrap(), Status::ReauthRequired);
}

#[tokio::test(flavor = "current_thread")]
async fn declared_and_chunked_response_limits_abort_before_json_decoding() {
    let f = Fixture::start();
    let (b, w) = selected(&f).await;
    f.control(LOGIN, "oversized");
    assert_eq!(
        b.login(w, "fixture", password()).await.unwrap_err(),
        Error::BodyLimit
    );
    f.control(LOGIN, "chunked_oversized");
    assert_eq!(
        b.login(w, "fixture", password()).await.unwrap_err(),
        Error::BodyLimit
    );
    assert_eq!(f.calls(LOGIN).len(), 2);
    assert_eq!(b.status().unwrap(), Status::ReauthRequired);
}

#[tokio::test(flavor = "current_thread")]
async fn cookies_remote_html_compression_and_legacy_grants_fail_closed() {
    let f = Fixture::start();
    let (b, w) = selected(&f).await;
    for mode in ["cookie", "html", "gzip", "legacy_grant"] {
        f.control(LOGIN, mode);
        assert_eq!(
            b.login(w, "fixture", password()).await.unwrap_err(),
            Error::Protocol
        );
        assert_eq!(b.status().unwrap(), Status::ReauthRequired);
    }
    assert_eq!(f.calls(LOGIN).len(), 4);
}

#[tokio::test(flavor = "current_thread")]
async fn profile_switch_cancels_running_http_and_waits_for_worker_to_exit() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    f.control(ME, "hold");
    LocalSet::new()
        .run_until(async {
            let worker = b.clone();
            let me = tokio::task::spawn_local(async move { worker.me(w).await });
            f.wait(ME, 1).await;
            b.select_confirmed_profile(w, &f.second_origin(), "fixture-community")
                .await
                .unwrap();
            assert!(matches!(
                me.await.unwrap(),
                Err(Error::Stale) | Err(Error::Cancelled)
            ));
            assert_eq!(b.status().unwrap(), Status::Selected);
            assert!(b.inner.state.lock().unwrap().requests.is_empty());
            assert!(b.memory.phases().is_empty());
            f.release();
        })
        .await;
}

#[tokio::test(flavor = "current_thread")]
async fn completed_delivery_is_fenced_again_before_owner_publication() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    let op = b.prepare_me(w).unwrap();
    let delivery = b.execute_me(op).await.unwrap();
    b.select_confirmed_profile(w, &f.second_origin(), "fixture-community")
        .await
        .unwrap();
    assert!(matches!(
        b.commit_me(w, delivery),
        Err(Error::Stale) | Err(Error::Cancelled)
    ));
    assert_eq!(b.status().unwrap(), Status::Selected);
}

#[tokio::test(flavor = "current_thread")]
async fn old_profile_handle_never_dispatches_credentials_to_new_origin() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    let old = b.prepare_me(w).unwrap();
    b.select_confirmed_profile(w, &f.second_origin(), "fixture-community")
        .await
        .unwrap();
    assert!(matches!(
        b.execute_me(old).await,
        Err(Error::Stale) | Err(Error::Cancelled)
    ));
    assert!(f.calls(ME).is_empty());
    f.control(ME, "normal");
    b.login(w, "fixture", password()).await.unwrap();
    b.me(w).await.unwrap();
    let logins = f.calls(LOGIN);
    assert_eq!(logins.len(), 2);
    assert_ne!(
        serde_json::from_str::<Value>(logins[0]["body"].as_str().unwrap()).unwrap()["client_instance_id"],
        serde_json::from_str::<Value>(logins[1]["body"].as_str().unwrap()).unwrap()["client_instance_id"]
    );
    assert_eq!(f.calls(ME)[0]["port"], f.port2);
}

#[tokio::test(flavor = "current_thread")]
async fn foreign_broker_or_window_cannot_cancel_or_publish_another_owners_operation() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    let second = b
        .register_native_window("second", WindowRole::Main)
        .unwrap();
    let op = b.prepare_me(w).unwrap();
    assert_eq!(b.cancel(second, op.cancel_handle()), Err(Error::Denied));
    let foreign = Broker::fixture(f.root.clone());
    let foreign_window = foreign
        .register_native_window("main", WindowRole::Main)
        .unwrap();
    assert_eq!(
        foreign.cancel(foreign_window, op.cancel_handle()),
        Err(Error::Denied)
    );
    let delivery = b.execute_me(op).await.unwrap();
    assert_eq!(b.commit_me(second, delivery).unwrap_err(), Error::Denied);
    let widget = b
        .register_native_window("widget", WindowRole::Widget)
        .unwrap();
    assert_eq!(b.prepare_me(widget).unwrap_err(), Error::Denied);
    assert_eq!(
        b.login(widget, "fixture", password()).await.unwrap_err(),
        Error::Denied
    );
}

#[tokio::test(flavor = "current_thread")]
async fn explicit_cancel_is_owner_bound_and_label_reuse_cannot_resurrect_a_window() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    f.control(ME, "hold");
    LocalSet::new()
        .run_until(async {
            let op = b.prepare_me(w).unwrap();
            let handle = op.cancel_handle();
            let worker = b.clone();
            let request = tokio::task::spawn_local(async move { worker.execute_me(op).await });
            f.wait(ME, 1).await;
            b.cancel(w, handle).unwrap();
            assert_eq!(request.await.unwrap().unwrap_err(), Error::Cancelled);
            let old = b.prepare_me(w).unwrap();
            let replacement = b.register_native_window("main", WindowRole::Main).unwrap();
            assert_ne!(w, replacement);
            assert!(matches!(
                b.execute_me(old).await,
                Err(Error::Stale) | Err(Error::Cancelled)
            ));
            assert_eq!(b.me(w).await.unwrap_err(), Error::Stale);
            f.release();
        })
        .await;
}

#[tokio::test(flavor = "current_thread")]
async fn eight_concurrent_refresh_callers_share_exactly_one_https_dispatch() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    f.control(REFRESH, "hold");
    LocalSet::new()
        .run_until(async {
            let leader_b = b.clone();
            let leader = tokio::task::spawn_local(async move { leader_b.refresh(w).await });
            f.wait(REFRESH, 1).await;
            let mut followers = Vec::new();
            for _ in 0..7 {
                let follower = b.clone();
                followers.push(tokio::task::spawn_local(async move {
                    follower.refresh(w).await
                }));
            }
            tokio::task::yield_now().await;
            assert_eq!(b.inner.state.lock().unwrap().requests.len(), 8);
            assert_eq!(b.memory.phases(), vec![1]);
            f.release();
            leader.await.unwrap().unwrap();
            for follower in followers {
                follower.await.unwrap().unwrap();
            }
            assert_eq!(f.calls(REFRESH).len(), 1);
            assert_eq!(b.status().unwrap(), Status::Authenticated);
            assert_eq!(b.memory.phases(), vec![0]);
        })
        .await;
}

#[tokio::test(flavor = "current_thread")]
async fn lost_ack_after_server_consumes_refresh_quarantines_session_without_retry() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    f.control(REFRESH, "drop_after_rotate");
    assert_eq!(b.refresh(w).await.unwrap_err(), Error::UncertainRotation);
    assert_eq!(f.state()["sequence"], 1);
    assert_eq!(b.status().unwrap(), Status::ReauthRequired);
    assert_eq!(b.memory.phases(), vec![1]);
    assert_eq!(b.refresh(w).await.unwrap_err(), Error::ReauthRequired);
    assert_eq!(b.me(w).await.unwrap_err(), Error::ReauthRequired);
    assert_eq!(f.calls(REFRESH).len(), 1);
}

#[tokio::test(flavor = "current_thread")]
async fn journal_failure_prevents_dispatch_and_successor_failure_never_marks_ready() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    b.memory.fault(Fault::WriteBefore);
    assert_eq!(b.refresh(w).await.unwrap_err(), Error::Vault);
    assert!(f.calls(REFRESH).is_empty());
    assert_eq!(b.status().unwrap(), Status::ReauthRequired);
    assert_eq!(b.memory.phases(), vec![0]);
    f.control(LOGIN, "normal");
    b.login(w, "fixture", password()).await.unwrap();
    f.control(REFRESH, "hold");
    LocalSet::new()
        .run_until(async {
            let worker = b.clone();
            let refresh = tokio::task::spawn_local(async move { worker.refresh(w).await });
            f.wait(REFRESH, 1).await;
            b.memory.fault(Fault::WriteAfter);
            f.release();
            assert_eq!(
                refresh.await.unwrap().unwrap_err(),
                Error::UncertainRotation
            );
            assert_eq!(b.status().unwrap(), Status::ReauthRequired);
            assert_eq!(b.memory.phases(), vec![0]);
            assert_eq!(b.refresh(w).await.unwrap_err(), Error::ReauthRequired);
        })
        .await;
    assert_eq!(f.calls(REFRESH).len(), 1);
}

#[tokio::test(flavor = "current_thread")]
async fn dropping_refresh_task_after_dispatch_quarantines_and_releases_native_ownership() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    f.control(REFRESH, "hold");
    LocalSet::new()
        .run_until(async {
            let worker = b.clone();
            let refresh = tokio::task::spawn_local(async move { worker.refresh(w).await });
            f.wait(REFRESH, 1).await;
            refresh.abort();
            assert!(refresh.await.unwrap_err().is_cancelled());
            assert_eq!(b.status().unwrap(), Status::ReauthRequired);
            assert_eq!(b.memory.phases(), vec![1]);
            assert!(b.inner.state.lock().unwrap().requests.is_empty());
            assert_eq!(b.refresh(w).await.unwrap_err(), Error::ReauthRequired);
            f.release();
        })
        .await;
    assert_eq!(f.calls(REFRESH).len(), 1);
}

#[tokio::test(flavor = "current_thread")]
async fn cancelled_refresh_result_cannot_resurrect_profile_or_window() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    f.control(REFRESH, "hold");
    LocalSet::new()
        .run_until(async {
            let worker = b.clone();
            let refresh = tokio::task::spawn_local(async move { worker.refresh(w).await });
            f.wait(REFRESH, 1).await;
            b.select_confirmed_profile(w, &f.second_origin(), "fixture-community")
                .await
                .unwrap();
            assert!(matches!(
                refresh.await.unwrap(),
                Err(Error::Stale) | Err(Error::Cancelled)
            ));
            assert_eq!(b.status().unwrap(), Status::Selected);
            assert!(b.memory.phases().is_empty());
            f.release();
        })
        .await;
}

#[tokio::test(flavor = "current_thread")]
async fn wrong_successor_family_instance_or_sequence_is_never_committed() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    for mode in ["bad_family", "bad_instance", "bad_sequence"] {
        f.control(REFRESH, mode);
        assert_eq!(b.refresh(w).await.unwrap_err(), Error::UncertainRotation);
        assert_eq!(b.status().unwrap(), Status::ReauthRequired);
        assert_eq!(b.memory.phases(), vec![1]);
        f.control(LOGIN, "normal");
        b.login(w, "fixture", password()).await.unwrap();
    }
    assert_eq!(f.calls(REFRESH).len(), 3);
}

#[tokio::test(flavor = "current_thread")]
async fn current_unauthorized_response_clears_only_its_native_session() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    f.control(ME, "unauthorized");
    assert_eq!(b.me(w).await.unwrap_err(), Error::Unauthorized);
    assert_eq!(b.status().unwrap(), Status::ReauthRequired);
    assert_eq!(b.me(w).await.unwrap_err(), Error::ReauthRequired);
    assert_eq!(f.calls(ME).len(), 1);
}

#[tokio::test(flavor = "current_thread")]
async fn restart_has_no_remembered_auth_and_no_startup_network() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    drop(b);
    let b = Broker::fixture(f.root.clone());
    let new = b.register_native_window("main", WindowRole::Main).unwrap();
    assert_ne!(w, new);
    assert_eq!(b.status().unwrap(), Status::Empty);
    assert_eq!(b.me(new).await.unwrap_err(), Error::NoProfile);
    assert_eq!(f.calls(LOGIN).len(), 1);
    assert!(f.calls(ME).is_empty());
}

#[tokio::test(flavor = "current_thread")]
async fn bounded_request_budget_and_prepared_cancellation_allow_switch_to_drain() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    let mut operations = Vec::new();
    for _ in 0..16 {
        operations.push(b.prepare_me(w).unwrap());
    }
    assert_eq!(b.prepare_me(w).unwrap_err(), Error::Busy);
    b.select_confirmed_profile(w, &f.second_origin(), "fixture-community")
        .await
        .unwrap();
    for old in operations {
        assert!(matches!(
            b.execute_me(old).await,
            Err(Error::Stale) | Err(Error::Cancelled)
        ));
    }
    assert!(f.calls(ME).is_empty());
}

#[test]
fn secret_parser_is_canonical_and_debug_redacts_enclosing_types() {
    use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
    let wire = URL_SAFE_NO_PAD.encode([41u8; 32]);
    let secret = Secret::parse(&wire).unwrap();
    assert_eq!(secret.wire().as_str(), wire);
    for invalid in [
        format!("{wire}="),
        format!(" {wire}"),
        format!("{wire}\r\n"),
        "a".repeat(44),
        "é".repeat(43),
        "+".repeat(43),
    ] {
        assert!(Secret::parse(&invalid).is_err())
    }
    assert_eq!(format!("{secret:?}"), "Secret(REDACTED)");
    let password = Password::from_native_input("fixture\tpassword\n".into()).unwrap();
    assert_eq!(password.text(), "fixture\tpassword\n");
    assert_eq!(format!("{password:?}"), "Password(REDACTED)");
}

#[tokio::test(flavor = "current_thread")]
async fn real_native_wss_header_metadata_filter_and_rotation_renewal() {
    LocalSet::new()
        .run_until(async {
            let f = Fixture::start();
            let (b, w) = logged(&f).await;
            let mut socket = b.open_native_socket(w).await.unwrap();
            let calls = f.calls("/api/native/v1/ws");
            assert_eq!(calls.len(), 1);
            let headers = calls[0]["headers"].as_object().unwrap();
            assert!(
                headers
                    .keys()
                    .any(|k| k.eq_ignore_ascii_case("Authorization"))
            );
            for forbidden in ["Origin", "Cookie", "Sec-WebSocket-Protocol"] {
                assert!(!headers.keys().any(|k| k.eq_ignore_ascii_case(forbidden)));
            }
            let info = b.socket_next(&mut socket).await.unwrap();
            assert!(matches!(info, MetadataEvent::ServerInfo(_)));
            assert!(matches!(
                b.socket_next(&mut socket).await.unwrap(),
                MetadataEvent::PresenceSnapshot(_)
            ));
            b.refresh(w).await.unwrap();
            // Buffered legacy chat content is suppressed; renewal ACK is native-only.
            let next = b.socket_next(&mut socket).await.unwrap();
            let value = serde_json::to_value(&next).unwrap();
            assert_eq!(
                value,
                json!({"type":"server_info","payload":{"version":"renewed"}})
            );
            assert_eq!(f.calls("WS:native_access_renew").len(), 1);
            assert!(
                !serde_json::to_string(&next)
                    .unwrap()
                    .contains("access_token")
            );
            b.socket_send(w, &mut socket, NativeSocketAction::Ping { t: 123 })
                .await
                .unwrap();
            assert!(matches!(
                b.socket_next(&mut socket).await.unwrap(),
                MetadataEvent::Pong(_)
            ));
            drop(socket);
            b.logout(w).await.unwrap();
        })
        .await;
}

#[tokio::test(flavor = "current_thread")]
async fn real_native_wss_redirect_cookie_and_frame_bounds_fail_closed() {
    for mode in ["redirect", "cookie", "oversized"] {
        let f = Fixture::start();
        let (b, w) = logged(&f).await;
        f.control("/api/native/v1/ws", mode);
        let opened = b.open_native_socket(w).await;
        if mode == "oversized" {
            let mut socket = opened.unwrap();
            assert!(b.socket_next(&mut socket).await.is_err());
        } else {
            assert!(opened.is_err());
        }
        assert_eq!(f.calls("/api/native/v1/ws").len(), 1);
    }
}

#[tokio::test(flavor = "current_thread")]
async fn native_wss_profile_switch_drains_old_reader_and_suppresses_publication() {
    LocalSet::new()
        .run_until(async {
            let f = Fixture::start();
            let (b, w) = logged(&f).await;
            let mut socket = b.open_native_socket(w).await.unwrap();
            b.socket_next(&mut socket).await.unwrap();
            b.socket_next(&mut socket).await.unwrap();
            let worker = b.clone();
            let reading =
                tokio::task::spawn_local(async move { worker.socket_next(&mut socket).await });
            tokio::task::yield_now().await;
            tokio::time::timeout(
                Duration::from_secs(2),
                b.select_confirmed_profile(w, &f.second_origin(), "fixture-community"),
            )
            .await
            .unwrap()
            .unwrap();
            assert!(matches!(
                reading.await.unwrap(),
                Err(Error::Stale) | Err(Error::Cancelled)
            ));
            assert_eq!(b.status().unwrap(), Status::Selected);
        })
        .await;
}

#[tokio::test(flavor = "current_thread")]
async fn native_wss_bad_renewal_ack_never_publishes_new_lease() {
    LocalSet::new()
        .run_until(async {
            let f = Fixture::start();
            let (b, w) = logged(&f).await;
            f.control("/api/native/v1/ws", "bad_ack");
            let mut socket = b.open_native_socket(w).await.unwrap();
            b.socket_next(&mut socket).await.unwrap();
            b.socket_next(&mut socket).await.unwrap();
            b.refresh(w).await.unwrap();
            assert_eq!(b.socket_next(&mut socket).await.unwrap_err(), Error::Stale);
            assert_eq!(f.calls("WS:native_access_renew").len(), 1);
        })
        .await;
}

#[tokio::test(flavor = "current_thread")]
async fn actual_native_localset_actor_auth_replies_export_only_user_and_native_context() {
    let f = Fixture::start();
    let client = NativeClient::fixture(f.root.clone());
    let lease = client.attach_main_window().unwrap();
    let connect = client
        .request(
            &lease,
            NativeRequest::Connect {
                address: f.origin(),
            },
        )
        .await
        .unwrap();
    let preview = client.commit(&lease, connect).unwrap();
    assert_eq!(preview.body["content_authorization"], "unavailable");
    let login = client
        .request(
            &lease,
            NativeRequest::Login {
                username: "fixture".to_owned(),
                password: password(),
            },
        )
        .await
        .unwrap();
    let user = client.commit(&lease, login).unwrap();
    assert_eq!(user.status, 200);
    assert_eq!(user.body["user"]["username"], "fixture");
    assert_eq!(user.context, lease.context_nonce());
    let exported = serde_json::to_string(&user).unwrap();
    for forbidden in [
        "access_token",
        "refresh_token",
        "family_id",
        "client_instance_id",
        "native-fixture-password",
    ] {
        assert!(!exported.contains(forbidden));
    }
    let me = client.request(&lease, NativeRequest::Me).await.unwrap();
    assert_eq!(
        client.commit(&lease, me).unwrap().body["username"],
        "fixture"
    );
    let refresh = client
        .request(&lease, NativeRequest::Refresh)
        .await
        .unwrap();
    assert_eq!(client.commit(&lease, refresh).unwrap().status, 204);
    let logout = client.request(&lease, NativeRequest::Logout).await.unwrap();
    assert_eq!(client.commit(&lease, logout).unwrap().status, 204);
    client.detach_main_window(&lease).unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn native_actor_final_queued_login_me_logout_and_window_fences() {
    let f = Fixture::start();
    let client = NativeClient::fixture(f.root.clone());
    let lease = client.attach_main_window().unwrap();
    let c = client
        .request(
            &lease,
            NativeRequest::Connect {
                address: f.origin(),
            },
        )
        .await
        .unwrap();
    client.commit(&lease, c).unwrap();
    let login = client
        .request(
            &lease,
            NativeRequest::Login {
                username: "fixture".to_owned(),
                password: password(),
            },
        )
        .await
        .unwrap();
    client.commit(&lease, login).unwrap();
    let me = client.request(&lease, NativeRequest::Me).await.unwrap();
    let logout = client.request(&lease, NativeRequest::Logout).await.unwrap();
    assert!(matches!(
        client.commit(&lease, me),
        Err(Error::Stale) | Err(Error::Cancelled)
    ));
    let next = client
        .request(
            &lease,
            NativeRequest::Login {
                username: "fixture".to_owned(),
                password: password(),
            },
        )
        .await
        .unwrap();
    client.commit(&lease, next).unwrap();
    assert_eq!(client.commit(&lease, logout).unwrap_err(), Error::Stale);
    let pending = client.request(&lease, NativeRequest::Me).await.unwrap();
    let reloaded = client.attach_main_window().unwrap();
    assert_ne!(reloaded.context_nonce(), lease.context_nonce());
    assert_eq!(client.commit(&lease, pending).unwrap_err(), Error::Stale);
    assert!(client.request(&lease, NativeRequest::Me).await.is_err());
    assert!(client.request(&reloaded, NativeRequest::Me).await.is_err());
    client.detach_main_window(&reloaded).unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn stale_old_connect_cannot_clear_recreated_windows_real_authenticated_session() {
    let f = Fixture::start();
    let client = NativeClient::fixture(f.root.clone());
    let old = client.attach_main_window().unwrap();
    let new = client.attach_main_window().unwrap();
    let c = client
        .request(
            &new,
            NativeRequest::Connect {
                address: f.origin(),
            },
        )
        .await
        .unwrap();
    client.commit(&new, c).unwrap();
    let login = client
        .request(
            &new,
            NativeRequest::Login {
                username: "fixture".to_owned(),
                password: password(),
            },
        )
        .await
        .unwrap();
    client.commit(&new, login).unwrap();
    assert_eq!(
        client
            .request(
                &old,
                NativeRequest::Connect {
                    address: f.second_origin()
                }
            )
            .await
            .unwrap_err(),
        Error::Stale
    );
    let me = client.request(&new, NativeRequest::Me).await.unwrap();
    assert_eq!(client.commit(&new, me).unwrap().body["username"], "fixture");
    assert_eq!(f.calls("/.well-known/mnema").len(), 1);
    client.detach_main_window(&new).unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn actor_owned_request_cancel_aborts_actual_https_and_never_cancels_foreign_context() {
    let f = Fixture::start();
    let client = NativeClient::fixture(f.root.clone());
    let lease = client.attach_main_window().unwrap();
    let c = client
        .request(
            &lease,
            NativeRequest::Connect {
                address: f.origin(),
            },
        )
        .await
        .unwrap();
    client.commit(&lease, c).unwrap();
    let login = client
        .request(
            &lease,
            NativeRequest::Login {
                username: "fixture".to_owned(),
                password: password(),
            },
        )
        .await
        .unwrap();
    client.commit(&lease, login).unwrap();
    f.control(ME, "hold");
    let id = Uuid::new_v4();
    let worker = client.clone();
    let owned = lease.clone();
    let task =
        tokio::spawn(async move { worker.request_with_id(&owned, id, NativeRequest::Me).await });
    f.wait(ME, 1).await;
    let other = NativeClient::fixture(f.root.clone());
    let foreign = other.attach_main_window().unwrap();
    assert_eq!(
        client.cancel_request(&foreign, id).unwrap_err(),
        Error::Stale
    );
    client.cancel_request(&lease, id).unwrap();
    assert_eq!(task.await.unwrap().unwrap_err(), Error::Cancelled);
    f.release();
    assert_eq!(f.calls(ME).len(), 1);
    client.detach_main_window(&lease).unwrap();
    other.detach_main_window(&foreign).unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn login_reservation_failures_do_not_leave_an_unowned_signing_in_state() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    let mut operations = Vec::new();
    for _ in 0..16 {
        operations.push(b.prepare_me(w).unwrap());
    }
    {
        let mut state = b.inner.state.lock().unwrap();
        // A deterministic boundary fault: cancelled workers retain their running slots
        // until their guards drain. This is the quota case seen by login.
        for request in state.requests.values_mut() {
            request.phase = RequestPhase::Running;
        }
        state.active.as_mut().unwrap().status = Status::ReauthRequired;
    }
    assert_eq!(
        b.login(w, "fixture", password()).await.unwrap_err(),
        Error::Busy
    );
    assert_eq!(b.status().unwrap(), Status::ReauthRequired);
    assert_eq!(f.calls(LOGIN).len(), 1);
    drop(operations);
    {
        let mut state = b.inner.state.lock().unwrap();
        state.next_request = u64::MAX;
    }
    assert_eq!(
        b.login(w, "fixture", password()).await.unwrap_err(),
        Error::Exhausted
    );
    assert_eq!(b.status().unwrap(), Status::ReauthRequired);
    assert!(b.inner.state.lock().unwrap().requests.is_empty());
    assert_eq!(f.calls(LOGIN).len(), 1);
    // Restore only the injected counter fault; normal login remains usable.
    b.inner.state.lock().unwrap().next_request = 16;
    b.login(w, "fixture", password()).await.unwrap();
    assert_eq!(b.status().unwrap(), Status::Authenticated);
    assert_eq!(f.calls(LOGIN).len(), 2);
}

#[tokio::test(flavor = "current_thread")]
async fn journal_read_failure_is_closed_before_network_and_order_is_real() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    assert!(b.memory.events().ends_with(&["read", "write"]));
    b.memory.fault(Fault::Read);
    assert_eq!(b.refresh(w).await.unwrap_err(), Error::Vault);
    assert!(f.calls(REFRESH).is_empty());
    assert_eq!(b.status().unwrap(), Status::ReauthRequired);
    assert_eq!(b.refresh(w).await.unwrap_err(), Error::ReauthRequired);
}

#[tokio::test(flavor = "current_thread")]
async fn deleting_a_refresh_follower_window_does_not_cancel_the_other_owners_leader() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    let follower_window = b
        .register_native_window("follower", WindowRole::Main)
        .unwrap();
    f.control(REFRESH, "hold");
    LocalSet::new()
        .run_until(async {
            let worker = b.clone();
            let leader = tokio::task::spawn_local(async move { worker.refresh(w).await });
            f.wait(REFRESH, 1).await;
            let other = b.clone();
            let follower =
                tokio::task::spawn_local(async move { other.refresh(follower_window).await });
            tokio::task::yield_now().await;
            b.destroy_native_window(follower_window).unwrap();
            assert!(matches!(
                follower.await.unwrap(),
                Err(Error::Stale) | Err(Error::Cancelled)
            ));
            assert_eq!(b.status().unwrap(), Status::Rotating);
            f.release();
            leader.await.unwrap().unwrap();
            assert_eq!(b.status().unwrap(), Status::Authenticated);
        })
        .await;
    assert_eq!(f.calls(REFRESH).len(), 1);
}

#[tokio::test(flavor = "current_thread")]
async fn logout_epoch_exhaustion_clears_native_authorization_before_error() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    b.inner.state.lock().unwrap().active.as_mut().unwrap().epoch = u64::MAX;
    assert_eq!(b.logout(w).await.unwrap_err(), Error::Exhausted);
    assert_eq!(b.status().unwrap(), Status::ReauthRequired);
    assert_eq!(b.me(w).await.unwrap_err(), Error::ReauthRequired);
    assert!(f.calls(LOGOUT).is_empty());
}

#[tokio::test(flavor = "current_thread")]
async fn exact_password_control_bytes_are_json_escaped_and_preserved_over_https() {
    let f = Fixture::start();
    let (b, w) = selected(&f).await;
    let password = Password::from_native_input("fixture\tpassword\n".into()).unwrap();
    b.login(w, "fixture", password).await.unwrap();
    let body: Value = serde_json::from_str(f.calls(LOGIN)[0]["body"].as_str().unwrap()).unwrap();
    assert_eq!(body["password"], "fixture\tpassword\n");
}

#[tokio::test(flavor = "current_thread")]
async fn total_https_body_deadline_stops_a_server_that_keeps_trickling_bytes() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    f.control(ME, "slow_body");
    let start = std::time::Instant::now();
    let result = tokio::time::timeout(Duration::from_secs(10), b.me(w))
        .await
        .expect("HTTP total deadline did not fire");
    assert_eq!(result.unwrap_err(), Error::Network);
    assert!(start.elapsed() >= Duration::from_secs(7));
    assert!(start.elapsed() < Duration::from_secs(10));
    assert_eq!(f.calls(ME).len(), 1);
}

#[tokio::test(flavor = "current_thread")]
async fn malformed_native_username_does_not_become_a_header_path_or_network_request() {
    let f = Fixture::start();
    let (b, w) = selected(&f).await;
    for bad in [
        "../auth/refresh",
        "fixture\r\nAuthorization: x",
        "https://other.test",
    ] {
        assert_eq!(
            b.login(w, bad, password()).await.unwrap_err(),
            Error::InvalidInput
        );
    }
    assert!(f.calls(LOGIN).is_empty());
    assert_eq!(b.status().unwrap(), Status::Selected);
}

#[tokio::test(flavor = "current_thread")]
async fn actual_fixed_metadata_routes_reject_secret_fields_bounds_and_stale_publication() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    for r in [
        MetadataResource::Channels,
        MetadataResource::Members,
        MetadataResource::ReadState,
        MetadataResource::Health,
        MetadataResource::Legal,
    ] {
        let d = b.metadata(w, r).await.unwrap();
        let value = b.commit_metadata(w, d).unwrap();
        assert!(!value.to_string().contains("access_token"));
        if matches!(r, MetadataResource::Members) {
            assert!(value[0].get("presence").is_none());
        }
    }
    for call in f.calls("/api/native/v1/members") {
        assert_eq!(call["method"], "GET");
        assert_eq!(call["body"], "");
        assert!(
            call["headers"]
                .as_object()
                .unwrap()
                .keys()
                .any(|k| k.eq_ignore_ascii_case("authorization"))
        );
    }
    f.control("/api/native/v1/channels", "unknown_metadata_field");
    assert_eq!(
        b.metadata(w, MetadataResource::Channels).await.unwrap_err(),
        Error::Protocol
    );
    f.control("/api/native/v1/channels", "metadata_oversized");
    assert_eq!(
        b.metadata(w, MetadataResource::Channels).await.unwrap_err(),
        Error::BodyLimit
    );
    f.control("/api/native/v1/channels", "normal");
    let old = b.metadata(w, MetadataResource::Channels).await.unwrap();
    b.refresh(w).await.unwrap();
    assert!(matches!(
        b.commit_metadata(w, old),
        Err(Error::Stale | Error::Cancelled)
    ));
}
#[tokio::test(flavor = "current_thread")]
async fn actual_actor_owned_socket_channel_ping_refresh_and_logout_drain() {
    use crate::{NativeClient, NativeRequest};
    let f = Fixture::start();
    let c = NativeClient::fixture(f.root.clone());
    let w = c.attach_main_window().unwrap();
    let p = c
        .request(
            &w,
            NativeRequest::Connect {
                address: f.origin(),
            },
        )
        .await
        .unwrap();
    c.commit(&w, p).unwrap();
    let p = c
        .request(
            &w,
            NativeRequest::Login {
                username: "fixture".into(),
                password: password(),
            },
        )
        .await
        .unwrap();
    c.commit(&w, p).unwrap();
    let (tx, mut rx) = tokio::sync::mpsc::unbounded_channel();
    let p = c
        .request(
            &w,
            NativeRequest::OpenSocket {
                observer: Arc::new(move |scope, n| tx.send((scope, n)).is_ok()),
            },
        )
        .await
        .unwrap();
    let reply = c.commit(&w, p).unwrap();
    let handle = Uuid::parse_str(reply.body["handle"].as_str().unwrap()).unwrap();
    let wait = async {
        loop {
            let (scope, n) = rx.recv().await.unwrap();
            c.check_socket_publication(&w, scope).unwrap();
            assert!(!n.payload.to_string().contains("access_token"));
            if n.kind == "message" && n.payload["type"] == "server_info" {
                break;
            }
        }
    };
    tokio::time::timeout(Duration::from_secs(3), wait)
        .await
        .unwrap();
    c.socket_send(&w, handle, NativeSocketAction::Ping { t: 42 })
        .await
        .unwrap();
    f.wait("WS:ping", 1).await;
    let p = c.request(&w, NativeRequest::Refresh).await.unwrap();
    c.commit(&w, p).unwrap();
    f.wait("WS:native_access_renew", 1).await;
    tokio::time::timeout(Duration::from_secs(3), async {
        loop {
            let (_, n) = rx.recv().await.unwrap();
            assert!(!n.payload.to_string().contains("native_access_renewed"));
            if n.kind == "message" && n.payload["payload"]["version"] == "renewed" {
                break;
            }
        }
    })
    .await
    .unwrap();
    let p = tokio::time::timeout(Duration::from_secs(3), c.request(&w, NativeRequest::Logout))
        .await
        .unwrap()
        .unwrap();
    c.commit(&w, p).unwrap();
    assert_eq!(f.calls(LOGOUT).len(), 1);
    assert!(matches!(
        c.socket_send(&w, handle, NativeSocketAction::Ping { t: 43 })
            .await,
        Err(Error::Stale | Error::Cancelled)
    ));
    c.detach_main_window(&w).unwrap();
}
#[tokio::test(flavor = "current_thread")]
async fn actor_pending_renewal_window_recreation_drops_custody_and_fences_queued_events() {
    use crate::{NativeClient, NativeRequest};
    let f = Fixture::start();
    f.control("/api/native/v1/ws", "lost_ack");
    let c = NativeClient::fixture(f.root.clone());
    let w = c.attach_main_window().unwrap();
    let p = c
        .request(
            &w,
            NativeRequest::Connect {
                address: f.origin(),
            },
        )
        .await
        .unwrap();
    c.commit(&w, p).unwrap();
    let p = c
        .request(
            &w,
            NativeRequest::Login {
                username: "fixture".into(),
                password: password(),
            },
        )
        .await
        .unwrap();
    c.commit(&w, p).unwrap();
    let (tx, mut rx) = tokio::sync::mpsc::unbounded_channel();
    let p = c
        .request(
            &w,
            NativeRequest::OpenSocket {
                observer: Arc::new(move |s, n| tx.send((s, n)).is_ok()),
            },
        )
        .await
        .unwrap();
    c.commit(&w, p).unwrap();
    let old = rx.recv().await.unwrap();
    let p = c.request(&w, NativeRequest::Refresh).await.unwrap();
    c.commit(&w, p).unwrap();
    f.wait("WS:native_access_renew", 1).await;
    let new = c.attach_main_window().unwrap();
    assert_eq!(c.check_socket_publication(&w, old.0), Err(Error::Stale));
    let p = tokio::time::timeout(
        Duration::from_secs(3),
        c.request(
            &new,
            NativeRequest::Connect {
                address: f.origin(),
            },
        ),
    )
    .await
    .unwrap()
    .unwrap();
    c.commit(&new, p).unwrap();
    let p = c
        .request(
            &new,
            NativeRequest::Login {
                username: "fixture".into(),
                password: password(),
            },
        )
        .await
        .unwrap();
    c.commit(&new, p).unwrap();
    c.detach_main_window(&new).unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn actual_socket_error_reply_waits_until_transport_and_running_guard_are_dropped() {
    use crate::{NativeClient, NativeRequest};
    let f = Fixture::start();
    let c = NativeClient::fixture(f.root.clone());
    let w = c.attach_main_window().unwrap();
    let p = c
        .request(
            &w,
            NativeRequest::Connect {
                address: f.origin(),
            },
        )
        .await
        .unwrap();
    c.commit(&w, p).unwrap();
    let p = c
        .request(
            &w,
            NativeRequest::Login {
                username: "fixture".into(),
                password: password(),
            },
        )
        .await
        .unwrap();
    c.commit(&w, p).unwrap();
    let p = c
        .request(
            &w,
            NativeRequest::OpenSocket {
                observer: Arc::new(|_, _| true),
            },
        )
        .await
        .unwrap();
    let r = c.commit(&w, p).unwrap();
    let h = Uuid::parse_str(r.body["handle"].as_str().unwrap()).unwrap();
    let (start, started) = std::sync::mpsc::channel();
    let gate = Arc::new(std::sync::Barrier::new(2));
    c.install_custody_drop_barrier(start, gate.clone());
    let sender = c.clone();
    let owner = w.clone();
    let request = tokio::spawn(async move {
        sender
            .socket_send(&owner, h, NativeSocketAction::Ping { t: u64::MAX })
            .await
    });
    tokio::time::sleep(Duration::from_millis(150)).await;
    started.recv_timeout(Duration::from_secs(3)).unwrap();
    assert!(
        !request.is_finished(),
        "error must remain private while socket custody is held"
    );
    gate.wait();
    assert_eq!(request.await.unwrap(), Err(Error::InvalidInput));
    let p = tokio::time::timeout(Duration::from_secs(2), c.request(&w, NativeRequest::Logout))
        .await
        .unwrap()
        .unwrap();
    c.commit(&w, p).unwrap();
    c.detach_main_window(&w).unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn selected_public_terms_use_no_auth_and_keep_old_server_unauthorized_out_of_login_state() {
    let f = Fixture::start();
    let (b, w) = selected(&f).await;
    let d = b
        .public_metadata(w, PublicMetadataResource::Legal)
        .await
        .unwrap();
    let v = b.commit_metadata(w, d).unwrap();
    assert_eq!(v["media_retention_days"], 0);
    assert!(
        f.calls("/api/native/v1/public/legal")[0]["headers"]
            .as_object()
            .unwrap()
            .keys()
            .all(|k| !k.eq_ignore_ascii_case("authorization"))
    );
    f.control("/api/native/v1/public/legal", "unauthorized");
    assert_eq!(
        b.public_metadata(w, PublicMetadataResource::Legal)
            .await
            .unwrap_err(),
        Error::QualificationRequired
    );
    assert_eq!(b.status().unwrap(), Status::Selected);
    f.control("/api/native/v1/public/legal", "normal");
    let d = b
        .public_metadata(w, PublicMetadataResource::Health)
        .await
        .unwrap();
    b.select_confirmed_profile(w, &f.second_origin(), "fixture-community")
        .await
        .unwrap();
    assert!(matches!(
        b.commit_metadata(w, d),
        Err(Error::Stale | Error::Cancelled)
    ));
}
#[tokio::test(flavor = "current_thread")]
async fn native_metadata_socket_forwards_live_disable_and_channel_changes_as_closed_dtos() {
    LocalSet::new()
        .run_until(async {
            let f = Fixture::start();
            f.control("/api/native/v1/ws", "metadata_updates");
            let (b, w) = logged(&f).await;
            let mut socket = b.open_native_socket(w).await.unwrap();
            let mut types = Vec::new();
            for _ in 0..6 {
                let event = b.socket_next(&mut socket).await.unwrap();
                let value = serde_json::to_value(event).unwrap();
                types.push(value["type"].as_str().unwrap().to_owned());
                if value["type"] == "user_update" {
                    assert_eq!(value["payload"]["disabled"], true);
                    assert_eq!(value["payload"].as_object().unwrap().len(), 2);
                }
                if value["type"] == "member_joined" {
                    assert!(value["payload"].get("presence").is_none());
                }
                assert!(!value.to_string().contains("access_token"));
            }
            assert!(types.iter().any(|s| s == "channels_changed"));
            assert!(types.iter().any(|s| s == "member_joined"));
            assert!(types.iter().any(|s| s == "user_stats"));
            drop(socket);
            b.logout(w).await.unwrap();
        })
        .await
}

#[tokio::test(flavor = "current_thread")]
async fn authenticated_scope_requires_real_login_not_selected_profile_lifecycle() {
    let f = Fixture::start();
    let (b, w) = selected(&f).await;
    let lifecycle = b.selected_publication_scope(w).unwrap();
    b.check_publication_scope(w, lifecycle).unwrap();
    assert_eq!(b.authenticated_scope(w).unwrap_err(), Error::ReauthRequired);
    b.login(w, "fixture", password()).await.unwrap();
    let scope = b.authenticated_scope(w).unwrap();
    assert_eq!(scope.origin(), format!("{}/", f.origin()));
    assert_eq!(scope.community_id(), "fixture-community");
    assert_eq!(
        scope.account_id(),
        Uuid::parse_str("11111111-1111-1111-1111-111111111111").unwrap()
    );
    assert_eq!(
        scope.family_id(),
        Uuid::parse_str("33333333-3333-3333-3333-333333333333").unwrap()
    );
    assert!(!scope.client_instance_id().is_nil());
    assert_eq!(scope.refresh_sequence(), 0);
    assert_eq!(scope.role(), "user");
    assert!(scope.native_context().is_none());
    assert_eq!(format!("{scope:?}"), "NativeAuthenticatedScope(REDACTED)");
    for id in [
        scope.native_window_identity(),
        scope.native_profile_identity(),
        scope.native_session_identity(),
    ] {
        assert!(!id.is_nil());
    }
    b.check_authenticated_scope(w, &scope).unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn authenticated_scope_rotating_is_blocked_and_successor_retires_exact_old_identity() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    let old = b.authenticated_scope(w).unwrap();
    f.control(REFRESH, "hold");
    LocalSet::new()
        .run_until(async {
            let next = b.clone();
            let refresh = tokio::task::spawn_local(async move { next.refresh(w).await });
            f.wait(REFRESH, 1).await;
            assert_eq!(b.authenticated_scope(w).unwrap_err(), Error::Busy);
            assert_eq!(b.check_authenticated_scope(w, &old), Err(Error::Busy));
            f.release();
            refresh.await.unwrap().unwrap();
        })
        .await;
    assert_eq!(b.check_authenticated_scope(w, &old), Err(Error::Stale));
    let next = b.authenticated_scope(w).unwrap();
    assert_eq!(next.account_id(), old.account_id());
    assert_eq!(next.family_id(), old.family_id());
    assert_eq!(next.client_instance_id(), old.client_instance_id());
    assert_eq!(next.refresh_sequence(), 1);
    assert_eq!(
        next.native_profile_identity(),
        old.native_profile_identity()
    );
    assert_eq!(next.native_window_identity(), old.native_window_identity());
    assert_ne!(
        next.native_session_identity(),
        old.native_session_identity()
    );
    assert_eq!(f.calls(REFRESH).len(), 1);
}

#[tokio::test(flavor = "current_thread")]
async fn authenticated_scope_lost_refresh_ack_and_aborted_rotation_never_resurrect() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    let old = b.authenticated_scope(w).unwrap();
    f.control(REFRESH, "drop_after_rotate");
    assert_eq!(b.refresh(w).await, Err(Error::UncertainRotation));
    assert_eq!(
        b.check_authenticated_scope(w, &old),
        Err(Error::ReauthRequired)
    );
    assert_eq!(b.authenticated_scope(w).unwrap_err(), Error::ReauthRequired);
    assert_eq!(b.refresh(w).await, Err(Error::ReauthRequired));
    assert_eq!(f.calls(REFRESH).len(), 1);
    b.login(w, "fixture", password()).await.unwrap();
    f.control(REFRESH, "hold");
    LocalSet::new()
        .run_until(async {
            let scope = b.authenticated_scope(w).unwrap();
            let next = b.clone();
            let refresh = tokio::task::spawn_local(async move { next.refresh(w).await });
            f.wait(REFRESH, 2).await;
            refresh.abort();
            assert!(refresh.await.unwrap_err().is_cancelled());
            assert_eq!(
                b.check_authenticated_scope(w, &scope),
                Err(Error::ReauthRequired)
            );
            assert_eq!(b.authenticated_scope(w).unwrap_err(), Error::ReauthRequired);
            f.release();
        })
        .await;
    assert_eq!(f.calls(REFRESH).len(), 2);
}

#[tokio::test(flavor = "current_thread")]
async fn authenticated_scope_logout_profile_and_window_replacement_fence_native_authority() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    let scope = b.authenticated_scope(w).unwrap();
    let overlay = b
        .register_native_window("overlay", WindowRole::Overlay)
        .unwrap();
    assert_eq!(b.authenticated_scope(overlay).unwrap_err(), Error::Denied);
    assert_eq!(
        b.check_authenticated_scope(overlay, &scope),
        Err(Error::Denied)
    );
    let other = b.register_native_window("other", WindowRole::Main).unwrap();
    assert_eq!(
        b.check_authenticated_scope(other, &scope),
        Err(Error::Denied)
    );
    let foreign = Broker::ephemeral().unwrap();
    assert_eq!(
        foreign.check_authenticated_scope(w, &scope),
        Err(Error::Denied)
    );
    b.logout(w).await.unwrap();
    assert_eq!(
        b.check_authenticated_scope(w, &scope),
        Err(Error::ReauthRequired)
    );
    b.login(w, "fixture", password()).await.unwrap();
    let newer = b.authenticated_scope(w).unwrap();
    assert_ne!(
        newer.native_session_identity(),
        scope.native_session_identity()
    );
    b.select_confirmed_profile(w, &f.second_origin(), "fixture-community")
        .await
        .unwrap();
    assert_eq!(
        b.check_authenticated_scope(w, &newer),
        Err(Error::ReauthRequired)
    );
    b.login(w, "fixture", password()).await.unwrap();
    let switched = b.authenticated_scope(w).unwrap();
    assert_ne!(
        switched.native_profile_identity(),
        newer.native_profile_identity()
    );
    assert_ne!(switched.origin(), newer.origin());
    assert_eq!(b.check_authenticated_scope(w, &newer), Err(Error::Stale));
    let recreated = b.register_native_window("main", WindowRole::Main).unwrap();
    assert_eq!(b.check_authenticated_scope(w, &switched), Err(Error::Stale));
    assert_eq!(
        b.check_authenticated_scope(recreated, &switched),
        Err(Error::Denied)
    );
}

#[tokio::test(flavor = "current_thread")]
async fn authenticated_scope_uses_one_admission_monotonic_cap_even_with_wall_clock_future() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    let first = b.authenticated_scope(w).unwrap();
    tokio::time::sleep(Duration::from_millis(8)).await;
    let next = b.authenticated_scope(w).unwrap();
    assert_eq!(
        first.monotonic_access_deadline(),
        next.monotonic_access_deadline()
    );
    assert_eq!(
        first.native_session_identity(),
        next.native_session_identity()
    );
    {
        let mut state = b.inner.state.lock().unwrap();
        let session = state.active.as_mut().unwrap().session.as_mut().unwrap();
        assert!(session.grant.access_expires_at > chrono::Utc::now());
        session.access_deadline = std::time::Instant::now() - Duration::from_millis(1);
    }
    assert_eq!(b.authenticated_scope(w).unwrap_err(), Error::Expired);
    assert_eq!(b.check_authenticated_scope(w, &first), Err(Error::Expired));
    let mut called = false;
    assert_eq!(
        b.with_authenticated_publication(w, &first, || { called = true }),
        Err(Error::Expired)
    );
    assert!(!called);
}

#[tokio::test(flavor = "current_thread")]
async fn authenticated_scope_publication_lock_prevents_profile_switch_between_check_and_enqueue() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    let scope = b.authenticated_scope(w).unwrap();
    let (entered_tx, entered_rx) = std::sync::mpsc::channel();
    let release = Arc::new(std::sync::Barrier::new(2));
    let pub_b = b.clone();
    let pub_release = release.clone();
    let publish = std::thread::spawn(move || {
        pub_b.with_authenticated_publication(w, &scope, || {
            entered_tx.send(()).unwrap();
            pub_release.wait();
            17
        })
    });
    entered_rx.recv_timeout(Duration::from_secs(2)).unwrap();
    // This callback barrier intentionally blocks only this private fault test.
    assert!(matches!(
        b.inner.state.try_lock(),
        Err(std::sync::TryLockError::WouldBlock)
    ));
    let (changed_tx, changed_rx) = std::sync::mpsc::channel();
    let change_b = b.clone();
    let change = std::thread::spawn(move || {
        change_b.invalidate_native_context().unwrap();
        changed_tx.send(()).unwrap();
    });
    assert!(matches!(
        changed_rx.recv_timeout(Duration::from_millis(20)),
        Err(std::sync::mpsc::RecvTimeoutError::Timeout)
    ));
    release.wait();
    assert_eq!(publish.join().unwrap(), Ok(17));
    changed_rx.recv_timeout(Duration::from_secs(2)).unwrap();
    change.join().unwrap();
    assert_eq!(b.authenticated_scope(w).unwrap_err(), Error::NoProfile);
}

#[tokio::test(flavor = "current_thread")]
async fn authenticated_scope_real_native_actor_window_context_fences_without_renderer_grant() {
    let f = Fixture::start();
    let client = NativeClient::fixture(f.root.clone());
    let lease = client.attach_main_window().unwrap();
    assert_eq!(
        client.authenticated_scope(&lease).unwrap_err(),
        Error::Stale
    );
    let c = client
        .request(
            &lease,
            NativeRequest::Connect {
                address: f.origin(),
            },
        )
        .await
        .unwrap();
    client.commit(&lease, c).unwrap();
    assert_eq!(
        client.authenticated_scope(&lease).unwrap_err(),
        Error::ReauthRequired
    );
    let login = client
        .request(
            &lease,
            NativeRequest::Login {
                username: "fixture".to_owned(),
                password: password(),
            },
        )
        .await
        .unwrap();
    client.commit(&lease, login).unwrap();
    let scope = client.authenticated_scope(&lease).unwrap();
    assert_eq!(
        scope.native_context().unwrap().hyphenated().to_string(),
        lease.context_nonce()
    );
    client.check_authenticated_scope(&lease, &scope).unwrap();
    assert_eq!(
        client.with_authenticated_publication(&lease, &scope, || 23),
        Ok(23)
    );
    let refresh = client
        .request(&lease, NativeRequest::Refresh)
        .await
        .unwrap();
    client.commit(&lease, refresh).unwrap();
    assert_eq!(
        client.check_authenticated_scope(&lease, &scope),
        Err(Error::Stale)
    );
    let fresh = client.authenticated_scope(&lease).unwrap();
    client.disconnect(&lease).unwrap();
    assert_eq!(
        client.check_authenticated_scope(&lease, &fresh),
        Err(Error::Denied)
    );
    let replaced = client.attach_main_window().unwrap();
    assert_eq!(
        client.check_authenticated_scope(&lease, &fresh),
        Err(Error::Stale)
    );
    assert_eq!(
        client.check_authenticated_scope(&replaced, &fresh),
        Err(Error::Denied)
    );
    client.detach_main_window(&replaced).unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn authenticated_scope_current_me_unauthorized_retires_before_native_completion() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    let scope = b.authenticated_scope(w).unwrap();
    f.control(ME, "unauthorized");
    assert_eq!(b.me(w).await.unwrap_err(), Error::Unauthorized);
    assert_eq!(
        b.check_authenticated_scope(w, &scope),
        Err(Error::ReauthRequired)
    );
    let mut emitted = false;
    assert_eq!(
        b.with_authenticated_publication(w, &scope, || { emitted = true }),
        Err(Error::ReauthRequired)
    );
    assert!(!emitted);
    assert_eq!(f.calls(ME).len(), 1);
}

#[tokio::test(flavor = "current_thread")]
async fn authenticated_scope_verified_me_role_downgrade_retires_admin_before_delivery() {
    let f = Fixture::start();
    let (b, w) = selected(&f).await;
    f.control(LOGIN, "admin_login");
    b.login(w, "fixture", password()).await.unwrap();
    let admin = b.authenticated_scope(w).unwrap();
    assert_eq!(admin.role(), "admin");
    let operation = b.prepare_me(w).unwrap();
    let delivery = b.execute_me(operation).await.unwrap();
    // Real /me response on this fixture is role user, before UI commit.
    assert_eq!(b.check_authenticated_scope(w, &admin), Err(Error::Stale));
    let user = b.authenticated_scope(w).unwrap();
    assert_eq!(user.role(), "user");
    assert_ne!(
        user.native_session_identity(),
        admin.native_session_identity()
    );
    assert_eq!(user.family_id(), admin.family_id());
    assert_eq!(user.client_instance_id(), admin.client_instance_id());
    assert_eq!(user.refresh_sequence(), admin.refresh_sequence());
    assert_eq!(b.commit_me(w, delivery).unwrap().role, "user");
}

#[tokio::test(flavor = "current_thread")]
async fn authenticated_scope_older_held_admin_me_cannot_resurrect_after_newer_downgrade() {
    let f = Fixture::start();
    let (b, w) = selected(&f).await;
    f.control(LOGIN, "admin_login");
    b.login(w, "fixture", password()).await.unwrap();
    let old = b.prepare_me(w).unwrap();
    f.control(ME, "hold_admin_me");
    let pending = b.execute_me(old);
    tokio::pin!(pending);
    tokio::select! {r=&mut pending=>panic!("held reply unexpectedly completed: {}",r.is_ok()), _=f.wait(ME,1)=>{}}
    f.control(ME, "normal");
    let newer = b.me(w).await.unwrap();
    assert_eq!(newer.role, "user");
    let downgraded = b.authenticated_scope(w).unwrap();
    f.release();
    assert_eq!(pending.await.unwrap_err(), Error::Stale);
    assert_eq!(b.authenticated_scope(w).unwrap().role(), "user");
    b.check_authenticated_scope(w, &downgraded).unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn authenticated_scope_older_delivered_admin_me_is_rejected_after_newer_downgrade() {
    let f = Fixture::start();
    let (b, w) = selected(&f).await;
    f.control(LOGIN, "admin_login");
    b.login(w, "fixture", password()).await.unwrap();
    f.control(ME, "hold_admin_me");
    f.release();
    let old = b.execute_me(b.prepare_me(w).unwrap()).await.unwrap();
    assert_eq!(b.authenticated_scope(w).unwrap().role(), "admin");
    f.control(ME, "normal");
    assert_eq!(b.me(w).await.unwrap().role, "user");
    assert_eq!(b.commit_me(w, old).unwrap_err(), Error::Stale);
    assert_eq!(b.authenticated_scope(w).unwrap().role(), "user");
}

#[path = "personal_metadata_tests.rs"]
mod personal_metadata_tests;

#[tokio::test(flavor = "current_thread")]
async fn authenticated_scope_older_refresh_cannot_resurrect_role_after_pending_me_downgrade() {
    let f = Fixture::start();
    let (b, w) = selected(&f).await;
    f.control(LOGIN, "admin_login");
    b.login(w, "fixture", password()).await.unwrap();
    f.control(ME, "hold");
    let me = b.execute_me(b.prepare_me(w).unwrap());
    tokio::pin!(me);
    tokio::select! {r=&mut me=>panic!("held me completed {}",r.is_ok()),_=f.wait(ME,1)=>{}}
    f.control(REFRESH, "hold_admin_refresh");
    let refresh = b.refresh(w);
    tokio::pin!(refresh);
    tokio::select! {r=&mut refresh=>panic!("held refresh completed {}",r.is_ok()),_=f.wait(REFRESH,1)=>{}}
    f.release();
    let delivery = me.await.unwrap();
    assert_eq!(delivery.value.role, "user");
    assert_eq!(
        b.inner
            .state
            .lock()
            .unwrap()
            .active
            .as_ref()
            .unwrap()
            .session
            .as_ref()
            .unwrap()
            .accepted_role,
        "user"
    );
    std::fs::write(f.path.join("refresh-release"), b"1").unwrap();
    assert_eq!(refresh.await, Err(Error::UncertainRotation));
    assert_eq!(b.status().unwrap(), Status::ReauthRequired);
    assert_eq!(b.authenticated_scope(w).unwrap_err(), Error::ReauthRequired);
    assert_eq!(b.memory.phases(), vec![1]);
    assert_eq!(b.refresh(w).await, Err(Error::ReauthRequired));
    assert_eq!(f.calls(REFRESH).len(), 1);
}
#[path = "opaque_relay_tests.rs"]
mod opaque_relay_tests;

#[tokio::test(flavor = "current_thread")]
async fn delayed_old_profile_ipc_never_transmits_credentials_or_runs_custody_hook_on_new_origin() {
    use crate::NativeClient;
    use std::sync::atomic::{AtomicBool, Ordering};
    let fixture = Fixture::start();
    let client = NativeClient::fixture(fixture.root.clone());
    let lease = client.attach_main_window().unwrap();
    let first = client
        .request_with_profile_id(
            &lease,
            Uuid::new_v4(),
            None,
            crate::NativeRequest::Connect {
                address: fixture.origin(),
            },
        )
        .await
        .unwrap();
    let first = client.commit(&lease, first).unwrap();
    let a = Uuid::parse_str(first.body["profile_intent"].as_str().unwrap()).unwrap();
    let entered = Arc::new(AtomicBool::new(false));
    let marker = entered.clone();
    // Construct old IPC arguments before B, but deliberately do not poll native
    // admission until B's successful discovery/selection. JS reply fencing
    // cannot repair a credential transmission if this comparator is absent.
    let delayed = client.request_with_profile_admission(
        &lease,
        Uuid::new_v4(),
        Some(a),
        crate::NativeRequest::Login {
            username: "fixture".into(),
            password: crate::Password::from_native_input("dummy delayed profile A password".into())
                .unwrap(),
        },
        move || marker.store(true, Ordering::Release),
    );
    let second = client
        .request_with_profile_id(
            &lease,
            Uuid::new_v4(),
            Some(a),
            crate::NativeRequest::Connect {
                address: fixture.second_origin(),
            },
        )
        .await
        .unwrap();
    let second = client.commit(&lease, second).unwrap();
    let b = Uuid::parse_str(second.body["profile_intent"].as_str().unwrap()).unwrap();
    assert_ne!(a, b);
    assert!(matches!(delayed.await, Err(Error::Stale)));
    assert!(!entered.load(Ordering::Acquire));
    assert!(fixture.calls(LOGIN).is_empty());
    let disconnected = Arc::new(AtomicBool::new(false));
    let marker = disconnected.clone();
    assert_eq!(
        client.disconnect_with_profile_admission(&lease, Some(a), move || {
            marker.store(true, Ordering::Release)
        }),
        Err(Error::Stale)
    );
    assert!(!disconnected.load(Ordering::Acquire));
    assert_eq!(client.profile_intent(&lease).unwrap(), Some(b));
    let current = client
        .request_with_profile_id(
            &lease,
            Uuid::new_v4(),
            Some(b),
            crate::NativeRequest::Login {
                username: "fixture".into(),
                password: crate::Password::from_native_input(
                    "dummy current profile B password".into(),
                )
                .unwrap(),
            },
        )
        .await
        .unwrap();
    assert_eq!(client.commit(&lease, current).unwrap().status, 200);
    let calls = fixture.calls(LOGIN);
    assert_eq!(calls.len(), 1);
    assert_eq!(calls[0]["port"], fixture.port2);
    client.detach_main_window(&lease).unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn lost_connect_ack_rehydrates_only_current_native_profile_and_old_none_never_burns_it() {
    let fixture = Fixture::start();
    let client = crate::NativeClient::fixture(fixture.root.clone());
    let lease = client.attach_main_window().unwrap();
    assert_eq!(client.profile_intent(&lease).unwrap(), None);
    let lost = client
        .request_with_profile_id(
            &lease,
            Uuid::new_v4(),
            None,
            crate::NativeRequest::Connect {
                address: fixture.origin(),
            },
        )
        .await
        .unwrap();
    drop(lost); // Simulate discarded IPC completion after actual native selection.
    let a = client.profile_intent(&lease).unwrap().unwrap();
    assert!(matches!(
        client
            .request_with_profile_id(
                &lease,
                Uuid::new_v4(),
                None,
                crate::NativeRequest::Connect {
                    address: fixture.second_origin()
                }
            )
            .await,
        Err(Error::Stale)
    ));
    assert_eq!(client.profile_intent(&lease).unwrap(), Some(a));
    let selected = client
        .request_with_profile_id(
            &lease,
            Uuid::new_v4(),
            Some(a),
            crate::NativeRequest::Connect {
                address: fixture.second_origin(),
            },
        )
        .await
        .unwrap();
    let b = Uuid::parse_str(
        client.commit(&lease, selected).unwrap().body["profile_intent"]
            .as_str()
            .unwrap(),
    )
    .unwrap();
    assert_ne!(a, b);
    fixture.control("/.well-known/mnema", "oversized");
    assert!(
        client
            .request_with_profile_id(
                &lease,
                Uuid::new_v4(),
                Some(b),
                crate::NativeRequest::Connect {
                    address: fixture.origin()
                }
            )
            .await
            .is_err()
    );
    assert_eq!(client.profile_intent(&lease).unwrap(), None);
    assert!(matches!(
        client
            .request_with_profile_id(&lease, Uuid::new_v4(), Some(b), crate::NativeRequest::Me)
            .await,
        Err(Error::Stale)
    ));
    client.detach_main_window(&lease).unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn rejected_login_keeps_selected_profile_retryable_without_resurrecting_old_delivery() {
    let fixture = Fixture::start();
    let client = crate::NativeClient::fixture(fixture.root.clone());
    let lease = client.attach_main_window().unwrap();
    let old_selection = client
        .request_with_profile_id(
            &lease,
            Uuid::new_v4(),
            None,
            crate::NativeRequest::Connect {
                address: fixture.origin(),
            },
        )
        .await
        .unwrap();
    let profile = client.profile_intent(&lease).unwrap().unwrap();
    fixture.control(LOGIN, "unauthorized");
    let failed = client
        .request_with_profile_id(
            &lease,
            Uuid::new_v4(),
            Some(profile),
            crate::NativeRequest::Login {
                username: "fixture".into(),
                password: password(),
            },
        )
        .await;
    assert!(matches!(failed, Err(Error::Unauthorized)));
    assert_eq!(fixture.calls(LOGIN).len(), 1);
    assert_eq!(client.profile_intent(&lease).unwrap(), Some(profile));
    assert_eq!(
        client.commit(&lease, old_selection).unwrap_err(),
        Error::Stale
    );
    assert!(matches!(
        client
            .request_with_profile_id(
                &lease,
                Uuid::new_v4(),
                Some(profile),
                crate::NativeRequest::Me
            )
            .await,
        Err(Error::ReauthRequired)
    ));
    assert!(fixture.calls(ME).is_empty());
    fixture.control(LOGIN, "normal");
    let retry = client
        .request_with_profile_id(
            &lease,
            Uuid::new_v4(),
            Some(profile),
            crate::NativeRequest::Login {
                username: "fixture".into(),
                password: password(),
            },
        )
        .await
        .unwrap();
    assert_eq!(client.commit(&lease, retry).unwrap().status, 200);
    assert_eq!(fixture.calls(LOGIN).len(), 2);
    assert_eq!(client.profile_intent(&lease).unwrap(), Some(profile));
    let current = client
        .request_with_profile_id(
            &lease,
            Uuid::new_v4(),
            Some(profile),
            crate::NativeRequest::Me,
        )
        .await
        .unwrap();
    assert_eq!(client.commit(&lease, current).unwrap().status, 200);
    assert_eq!(fixture.calls(ME).len(), 1);
    client.detach_main_window(&lease).unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn rejected_login_can_disconnect_and_reconnect_without_reusing_old_profile_authority() {
    use std::sync::atomic::{AtomicBool, Ordering};
    let fixture = Fixture::start();
    let client = crate::NativeClient::fixture(fixture.root.clone());
    let lease = client.attach_main_window().unwrap();
    let selected = client
        .request_with_profile_id(
            &lease,
            Uuid::new_v4(),
            None,
            crate::NativeRequest::Connect {
                address: fixture.origin(),
            },
        )
        .await
        .unwrap();
    let old = Uuid::parse_str(
        client.commit(&lease, selected).unwrap().body["profile_intent"]
            .as_str()
            .unwrap(),
    )
    .unwrap();
    fixture.control(LOGIN, "unauthorized");
    assert!(matches!(
        client
            .request_with_profile_id(
                &lease,
                Uuid::new_v4(),
                Some(old),
                crate::NativeRequest::Login {
                    username: "fixture".into(),
                    password: password()
                }
            )
            .await,
        Err(Error::Unauthorized)
    ));
    let admitted = Arc::new(AtomicBool::new(false));
    let marker = admitted.clone();
    client
        .disconnect_with_profile_admission(&lease, Some(old), move || {
            marker.store(true, Ordering::Release)
        })
        .unwrap();
    assert!(admitted.load(Ordering::Acquire));
    assert_eq!(client.profile_intent(&lease).unwrap(), None);
    let selected = client
        .request_with_profile_id(
            &lease,
            Uuid::new_v4(),
            None,
            crate::NativeRequest::Connect {
                address: fixture.second_origin(),
            },
        )
        .await
        .unwrap();
    let new = Uuid::parse_str(
        client.commit(&lease, selected).unwrap().body["profile_intent"]
            .as_str()
            .unwrap(),
    )
    .unwrap();
    assert_ne!(old, new);
    assert!(matches!(
        client
            .request_with_profile_id(
                &lease,
                Uuid::new_v4(),
                Some(old),
                crate::NativeRequest::Login {
                    username: "fixture".into(),
                    password: password()
                }
            )
            .await,
        Err(Error::Stale)
    ));
    assert_eq!(fixture.calls(LOGIN).len(), 1);
    fixture.control(LOGIN, "normal");
    let current = client
        .request_with_profile_id(
            &lease,
            Uuid::new_v4(),
            Some(new),
            crate::NativeRequest::Login {
                username: "fixture".into(),
                password: password(),
            },
        )
        .await
        .unwrap();
    assert_eq!(client.commit(&lease, current).unwrap().status, 200);
    let calls = fixture.calls(LOGIN);
    assert_eq!(calls.len(), 2);
    assert_eq!(calls[1]["port"], fixture.port2);
    client.detach_main_window(&lease).unwrap();
}

async fn selected_authentication_client(
    fixture: &Fixture,
) -> (crate::NativeClient, crate::NativeWindowLease, Uuid) {
    let client = crate::NativeClient::fixture(fixture.root.clone());
    let lease = client.attach_main_window().unwrap();
    let selected = client
        .request_with_authentication_admission(
            &lease,
            Uuid::new_v4(),
            None,
            None,
            crate::NativeRequest::Connect {
                address: fixture.origin(),
            },
            || {},
        )
        .await
        .unwrap();
    let profile = Uuid::parse_str(
        client.commit(&lease, selected).unwrap().body["profile_intent"]
            .as_str()
            .unwrap(),
    )
    .unwrap();
    (client, lease, profile)
}
async fn admitted_authentication_login(
    client: &crate::NativeClient,
    lease: &crate::NativeWindowLease,
    profile: Uuid,
    expected: Option<Uuid>,
) -> Uuid {
    let delivery = client
        .request_with_authentication_admission(
            lease,
            Uuid::new_v4(),
            Some(profile),
            expected,
            crate::NativeRequest::Login {
                username: "fixture".into(),
                password: password(),
            },
            || {},
        )
        .await
        .unwrap();
    let body = client.commit(lease, delivery).unwrap().body;
    assert_eq!(body.as_object().unwrap().len(), 2);
    assert!(body.get("user").is_some());
    Uuid::parse_str(body["authentication_intent"].as_str().unwrap()).unwrap()
}

#[tokio::test(flavor = "current_thread")]
async fn authentication_intent_delayed_first_admission_never_targets_replacement_family_or_custody()
{
    use std::sync::atomic::{AtomicUsize, Ordering};
    let fixture = Fixture::start();
    let (client, lease, profile) = selected_authentication_client(&fixture).await;
    assert_eq!(client.authentication_intent(&lease).unwrap(), None);
    let old = admitted_authentication_login(&client, &lease, profile, None).await;
    let old_scope = client.authenticated_scope_with_intent(&lease, old).unwrap();
    let custody = Arc::new(AtomicUsize::new(0));
    let marker = custody.clone();
    let delayed_login = client.request_with_authentication_admission(
        &lease,
        Uuid::new_v4(),
        Some(profile),
        Some(old),
        crate::NativeRequest::Login {
            username: "fixture".into(),
            password: password(),
        },
        move || {
            marker.fetch_add(1, Ordering::AcqRel);
        },
    );
    let marker = custody.clone();
    let delayed_logout = client.request_with_authentication_admission(
        &lease,
        Uuid::new_v4(),
        Some(profile),
        Some(old),
        crate::NativeRequest::Logout,
        move || {
            marker.fetch_add(1, Ordering::AcqRel);
        },
    );
    let delayed_me = client.request_with_authentication_admission(
        &lease,
        Uuid::new_v4(),
        Some(profile),
        Some(old),
        crate::NativeRequest::Me,
        || {},
    );
    let current = admitted_authentication_login(&client, &lease, profile, Some(old)).await;
    assert_ne!(old, current);
    assert_eq!(client.authentication_intent(&lease).unwrap(), Some(current));
    assert_eq!(client.profile_intent(&lease).unwrap(), Some(profile));
    assert!(matches!(delayed_login.await, Err(Error::Stale)));
    assert!(matches!(delayed_logout.await, Err(Error::Stale)));
    assert!(matches!(delayed_me.await, Err(Error::Stale)));
    assert_eq!(custody.load(Ordering::Acquire), 0);
    assert_eq!(fixture.calls(LOGIN).len(), 2);
    assert!(fixture.calls(LOGOUT).is_empty());
    assert!(fixture.calls(ME).is_empty());
    assert_eq!(
        client.check_authenticated_scope(&lease, &old_scope),
        Err(Error::Stale)
    );
    assert_eq!(
        client
            .authenticated_scope_with_intent(&lease, old)
            .unwrap_err(),
        Error::Stale
    );
    let me = client
        .request_with_authentication_admission(
            &lease,
            Uuid::new_v4(),
            Some(profile),
            Some(current),
            crate::NativeRequest::Me,
            || {},
        )
        .await
        .unwrap();
    assert_eq!(client.commit(&lease, me).unwrap().status, 200);
    assert_eq!(fixture.calls(ME).len(), 1);
    client.detach_main_window(&lease).unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn authentication_intent_ticket_is_consumed_at_prepare_and_never_recaptures_new_grant() {
    let fixture = Fixture::start();
    let (broker, window) = logged(&fixture).await;
    let old = broker.authentication_intent(window).unwrap().unwrap();
    let bound = broker
        .bind_invocation(window, Some(old), Uuid::new_v4(), InvocationKind::Read)
        .unwrap();
    broker.logout(window).await.unwrap();
    broker.login(window, "fixture", password()).await.unwrap();
    let current = broker.authentication_intent(window).unwrap().unwrap();
    assert_ne!(old, current);
    assert_eq!(bound.prepare_me(window).unwrap_err(), Error::Stale);
    assert!(fixture.calls(ME).is_empty());
    assert_eq!(broker.me(window).await.unwrap().username, "fixture");
    assert_eq!(fixture.calls(ME).len(), 1);
}

#[tokio::test(flavor = "current_thread")]
async fn authentication_intent_pending_drop_retires_only_its_owned_transition_and_requires_fresh_login()
 {
    let fixture = Fixture::start();
    let (broker, window) = logged(&fixture).await;
    let old = broker.authentication_intent(window).unwrap().unwrap();
    let old_scope = broker.authenticated_scope(window).unwrap();
    let pending = broker
        .bind_invocation(window, Some(old), Uuid::new_v4(), InvocationKind::Login)
        .unwrap();
    assert_eq!(broker.authentication_intent(window), Err(Error::Busy));
    assert_eq!(broker.authenticated_scope(window).unwrap_err(), Error::Busy);
    assert!(matches!(
        broker.bind_invocation(window, None, Uuid::new_v4(), InvocationKind::Login),
        Err(Error::Busy)
    ));
    assert!(matches!(
        broker.bind_invocation(window, Some(old), Uuid::new_v4(), InvocationKind::Read),
        Err(Error::Busy)
    ));
    assert_eq!(fixture.calls(LOGIN).len(), 1);
    drop(pending);
    assert_eq!(broker.status().unwrap(), Status::ReauthRequired);
    assert_eq!(broker.authentication_intent(window).unwrap(), None);
    assert_eq!(
        broker.check_authenticated_scope(window, &old_scope),
        Err(Error::ReauthRequired)
    );
    assert!(matches!(
        broker.bind_invocation(window, Some(old), Uuid::new_v4(), InvocationKind::Read),
        Err(Error::ReauthRequired)
    ));
    broker.login(window, "fixture", password()).await.unwrap();
    let current = broker.authentication_intent(window).unwrap().unwrap();
    assert_ne!(old, current);
    assert!(matches!(
        broker.bind_invocation(window, Some(old), Uuid::new_v4(), InvocationKind::Read),
        Err(Error::Stale)
    ));
    assert_eq!(fixture.calls(LOGIN).len(), 2);
}

#[tokio::test(flavor = "current_thread")]
async fn authentication_intent_validated_refresh_preserves_family_selector_and_retires_scope_revision()
 {
    let fixture = Fixture::start();
    let (client, lease, profile) = selected_authentication_client(&fixture).await;
    let authentication = admitted_authentication_login(&client, &lease, profile, None).await;
    let old = client
        .authenticated_scope_with_intent(&lease, authentication)
        .unwrap();
    let refresh = client
        .request_with_authentication_admission(
            &lease,
            Uuid::new_v4(),
            Some(profile),
            Some(authentication),
            crate::NativeRequest::Refresh,
            || {},
        )
        .await
        .unwrap();
    assert_eq!(client.commit(&lease, refresh).unwrap().status, 204);
    assert_eq!(
        client.authentication_intent(&lease).unwrap(),
        Some(authentication)
    );
    let next = client
        .authenticated_scope_with_intent(&lease, authentication)
        .unwrap();
    assert_eq!(next.authentication_intent(), old.authentication_intent());
    assert_eq!(next.family_id(), old.family_id());
    assert_eq!(next.client_instance_id(), old.client_instance_id());
    assert_eq!(next.refresh_sequence(), old.refresh_sequence() + 1);
    assert_ne!(
        next.native_session_identity(),
        old.native_session_identity()
    );
    assert_eq!(
        client.check_authenticated_scope(&lease, &old),
        Err(Error::Stale)
    );
    assert_eq!(client.check_authenticated_scope(&lease, &next), Ok(()));
    assert_eq!(fixture.calls(REFRESH).len(), 1);
    client.detach_main_window(&lease).unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn authentication_intent_admitted_actor_ticket_cannot_prepare_replacement_family() {
    let fixture = Fixture::start();
    let (client, lease, profile) = selected_authentication_client(&fixture).await;
    let old = admitted_authentication_login(&client, &lease, profile, None).await;
    let (started, release) = client.hold_next_prepare();
    let old_client = client.clone();
    let old_lease = lease.clone();
    let old_request = tokio::spawn(async move {
        old_client
            .request_with_authentication_admission(
                &old_lease,
                Uuid::new_v4(),
                Some(profile),
                Some(old),
                crate::NativeRequest::Me,
                || {},
            )
            .await
    });
    tokio::time::timeout(Duration::from_secs(3), started)
        .await
        .unwrap()
        .unwrap();
    let current = admitted_authentication_login(&client, &lease, profile, Some(old)).await;
    assert_ne!(old, current);
    release.send(()).unwrap();
    assert!(matches!(old_request.await.unwrap(), Err(Error::Stale)));
    assert!(fixture.calls(ME).is_empty());
    let fresh = client
        .request_with_authentication_admission(
            &lease,
            Uuid::new_v4(),
            Some(profile),
            Some(current),
            crate::NativeRequest::Me,
            || {},
        )
        .await
        .unwrap();
    assert_eq!(client.commit(&lease, fresh).unwrap().status, 200);
    assert_eq!(fixture.calls(ME).len(), 1);
    client.detach_main_window(&lease).unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn authentication_intent_ticket_cannot_select_grant_after_accepted_role_revision() {
    let fixture = Fixture::start();
    fixture.control(LOGIN, "admin_login");
    let (broker, window) = logged(&fixture).await;
    let intent = broker.authentication_intent(window).unwrap().unwrap();
    let admin = broker.authenticated_scope(window).unwrap();
    assert_eq!(admin.role(), "admin");
    let request_id = Uuid::new_v4();
    let queued = broker
        .bind_invocation(window, Some(intent), request_id, InvocationKind::Read)
        .unwrap();
    assert_eq!(
        queued.check_invocation_job(Uuid::new_v4()),
        Err(Error::Stale)
    );
    assert_eq!(queued.check_invocation_job(request_id), Ok(()));
    assert_eq!(broker.me(window).await.unwrap().role, "user");
    assert_eq!(broker.authentication_intent(window).unwrap(), Some(intent));
    assert_eq!(queued.prepare_me(window).unwrap_err(), Error::Stale);
    assert_eq!(fixture.calls(ME).len(), 1);
    assert_eq!(broker.authenticated_scope(window).unwrap().role(), "user");
}

#[tokio::test(flavor = "current_thread")]
async fn authentication_intent_pending_cancel_uses_original_submission_and_never_current_descriptor()
 {
    let fixture = Fixture::start();
    let (client, lease, profile) = selected_authentication_client(&fixture).await;
    let (started, release) = client.hold_next_prepare();
    let id = Uuid::new_v4();
    let queued_client = client.clone();
    let queued_lease = lease.clone();
    let login = tokio::spawn(async move {
        queued_client
            .request_with_authentication_admission(
                &queued_lease,
                id,
                Some(profile),
                None,
                crate::NativeRequest::Login {
                    username: "fixture".into(),
                    password: password(),
                },
                || {},
            )
            .await
    });
    tokio::time::timeout(Duration::from_secs(3), started)
        .await
        .unwrap()
        .unwrap();
    assert_eq!(client.selected_intents(&lease), Err(Error::Busy));
    assert_eq!(
        client.cancel_request_with_intents(&lease, Some(profile), Some(Uuid::new_v4()), id),
        Err(Error::Stale)
    );
    assert_eq!(
        client.cancel_request_with_intents(&lease, Some(profile), None, id),
        Ok(())
    );
    assert!(matches!(login.await.unwrap(), Err(Error::Cancelled)));
    let _ = release.send(());
    tokio::time::timeout(Duration::from_secs(3), async {
        while client.selected_intents(&lease) == Err(Error::Busy) {
            tokio::time::sleep(Duration::from_millis(5)).await;
        }
    })
    .await
    .unwrap();
    assert_eq!(
        client.selected_intents(&lease).unwrap(),
        (Some(profile), None)
    );
    assert!(fixture.calls(LOGIN).is_empty());
    let accepted = admitted_authentication_login(&client, &lease, profile, None).await;
    assert_eq!(
        client.selected_intents(&lease).unwrap(),
        (Some(profile), Some(accepted))
    );
    assert_eq!(fixture.calls(LOGIN).len(), 1);
    assert_eq!(
        client.cancel_request_with_intents(&lease, Some(profile), None, id),
        Ok(())
    );
    assert_eq!(
        client.authentication_intent(&lease).unwrap(),
        Some(accepted)
    );
    client.detach_main_window(&lease).unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn authentication_intent_current_401_allows_same_selection_retry_without_null_bypass() {
    let fixture = Fixture::start();
    let (client, lease, profile) = selected_authentication_client(&fixture).await;
    let accepted = admitted_authentication_login(&client, &lease, profile, None).await;
    fixture.control(ME, "unauthorized");
    assert!(matches!(
        client
            .request_with_authentication_admission(
                &lease,
                Uuid::new_v4(),
                Some(profile),
                Some(accepted),
                crate::NativeRequest::Me,
                || {}
            )
            .await,
        Err(Error::Unauthorized)
    ));
    assert_eq!(
        client.selected_intents(&lease).unwrap(),
        (Some(profile), None)
    );
    assert!(matches!(
        client
            .request_with_authentication_admission(
                &lease,
                Uuid::new_v4(),
                Some(profile),
                Some(accepted),
                crate::NativeRequest::Logout,
                || {}
            )
            .await,
        Err(Error::ReauthRequired)
    ));
    fixture.control(ME, "normal");
    let fresh = admitted_authentication_login(&client, &lease, profile, None).await;
    assert_ne!(fresh, accepted);
    assert!(matches!(
        client
            .request_with_authentication_admission(
                &lease,
                Uuid::new_v4(),
                Some(profile),
                None,
                crate::NativeRequest::Login {
                    username: "fixture".into(),
                    password: password()
                },
                || {}
            )
            .await,
        Err(Error::Stale)
    ));
    assert_eq!(fixture.calls(LOGIN).len(), 2);
    assert!(fixture.calls(LOGOUT).is_empty());
    client.detach_main_window(&lease).unwrap();
}

const PASSWORD_CHANGE: &str = "/api/native/v1/auth/password";
fn changed_password() -> Password {
    Password::from_native_input("changed-native-fixture-password".into()).unwrap()
}

#[tokio::test(flavor = "current_thread")]
async fn authentication_intent_password_seals_before_dispatch_installs_new_family_and_ack_only_nonce()
 {
    use std::sync::atomic::{AtomicBool, Ordering};
    let fixture = Fixture::start();
    let (client, lease, profile) = selected_authentication_client(&fixture).await;
    let old = admitted_authentication_login(&client, &lease, profile, None).await;
    let old_scope = client.authenticated_scope_with_intent(&lease, old).unwrap();
    fixture.control(PASSWORD_CHANGE, "hold");
    let retired = Arc::new(AtomicBool::new(false));
    let marker = retired.clone();
    let pw_client = client.clone();
    let pw_lease = lease.clone();
    let changing = tokio::spawn(async move {
        pw_client
            .request_with_authentication_admission(
                &pw_lease,
                Uuid::new_v4(),
                Some(profile),
                Some(old),
                crate::NativeRequest::Password {
                    current_password: password(),
                    new_password: changed_password(),
                },
                move || marker.store(true, Ordering::Release),
            )
            .await
    });
    fixture.wait(PASSWORD_CHANGE, 1).await;
    assert!(retired.load(Ordering::Acquire));
    assert_eq!(client.selected_intents(&lease), Err(Error::Busy));
    assert!(matches!(
        client.check_authenticated_scope(&lease, &old_scope),
        Err(Error::ReauthRequired)
    ));
    fixture.release();
    let reply = client
        .commit(&lease, changing.await.unwrap().unwrap())
        .unwrap();
    assert_eq!(reply.status, 200);
    assert_eq!(reply.body.as_object().unwrap().len(), 1);
    let accepted = Uuid::parse_str(reply.body["authentication_intent"].as_str().unwrap()).unwrap();
    assert_ne!(accepted, old);
    let current = client
        .authenticated_scope_with_intent(&lease, accepted)
        .unwrap();
    assert_ne!(current.family_id(), old_scope.family_id());
    assert_eq!(current.client_instance_id(), old_scope.client_instance_id());
    assert_eq!(current.account_id(), old_scope.account_id());
    assert_eq!(current.refresh_sequence(), 0);
    assert_eq!(
        client.selected_intents(&lease).unwrap(),
        (Some(profile), Some(accepted))
    );
    let wire: Value =
        serde_json::from_str(fixture.calls(PASSWORD_CHANGE)[0]["body"].as_str().unwrap()).unwrap();
    assert_eq!(wire.as_object().unwrap().len(), 2);
    assert_eq!(wire["current_password"], "native-fixture-password");
    assert_eq!(wire["new_password"], "changed-native-fixture-password");
    assert_eq!(fixture.calls(PASSWORD_CHANGE)[0]["method"], "PUT");
    let delayed = client
        .request_with_authentication_admission(
            &lease,
            Uuid::new_v4(),
            Some(profile),
            Some(old),
            crate::NativeRequest::Password {
                current_password: password(),
                new_password: changed_password(),
            },
            || panic!("stale password must never retire current owner"),
        )
        .await;
    assert!(matches!(delayed, Err(Error::Stale)));
    assert!(matches!(
        client
            .request_with_authentication_admission(
                &lease,
                Uuid::new_v4(),
                Some(profile),
                Some(old),
                crate::NativeRequest::Logout,
                || {}
            )
            .await,
        Err(Error::Stale)
    ));
    assert_eq!(fixture.calls(PASSWORD_CHANGE).len(), 1);
    assert!(fixture.calls(LOGOUT).is_empty());
    let me = client
        .request_with_authentication_admission(
            &lease,
            Uuid::new_v4(),
            Some(profile),
            Some(accepted),
            crate::NativeRequest::Me,
            || {},
        )
        .await
        .unwrap();
    assert_eq!(client.commit(&lease, me).unwrap().status, 200);
    client.detach_main_window(&lease).unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn authentication_intent_password_uncertain_ack_never_replays_or_recovers_replacement() {
    let fixture = Fixture::start();
    let (client, lease, profile) = selected_authentication_client(&fixture).await;
    let old = admitted_authentication_login(&client, &lease, profile, None).await;
    fixture.control(PASSWORD_CHANGE, "password_drop_ack");
    let result = client
        .request_with_authentication_admission(
            &lease,
            Uuid::new_v4(),
            Some(profile),
            Some(old),
            crate::NativeRequest::Password {
                current_password: password(),
                new_password: changed_password(),
            },
            || {},
        )
        .await;
    assert!(matches!(result, Err(Error::Network)));
    assert_eq!(fixture.calls(PASSWORD_CHANGE).len(), 1);
    assert_eq!(
        client.selected_intents(&lease).unwrap(),
        (Some(profile), None)
    );
    assert!(matches!(
        client
            .request_with_authentication_admission(
                &lease,
                Uuid::new_v4(),
                Some(profile),
                Some(old),
                crate::NativeRequest::Password {
                    current_password: password(),
                    new_password: changed_password()
                },
                || {}
            )
            .await,
        Err(Error::ReauthRequired)
    ));
    assert_eq!(fixture.calls(PASSWORD_CHANGE).len(), 1);
    assert!(fixture.calls(REFRESH).is_empty());
    let login = client
        .request_with_authentication_admission(
            &lease,
            Uuid::new_v4(),
            Some(profile),
            None,
            crate::NativeRequest::Login {
                username: "fixture".into(),
                password: changed_password(),
            },
            || {},
        )
        .await
        .unwrap();
    let reply = client.commit(&lease, login).unwrap();
    let fresh = Uuid::parse_str(reply.body["authentication_intent"].as_str().unwrap()).unwrap();
    assert_ne!(fresh, old);
    assert_eq!(fixture.calls(LOGIN).len(), 2);
    client.detach_main_window(&lease).unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn authentication_intent_password_wrong_initial_successor_is_quarantined_once() {
    for mode in [
        "password_old_family",
        "password_wrong_account",
        "password_bad_sequence",
        "bad_instance",
    ] {
        let fixture = Fixture::start();
        let (client, lease, profile) = selected_authentication_client(&fixture).await;
        let old = admitted_authentication_login(&client, &lease, profile, None).await;
        fixture.control(PASSWORD_CHANGE, mode);
        let result = client
            .request_with_authentication_admission(
                &lease,
                Uuid::new_v4(),
                Some(profile),
                Some(old),
                crate::NativeRequest::Password {
                    current_password: password(),
                    new_password: changed_password(),
                },
                || {},
            )
            .await;
        assert!(matches!(result, Err(Error::Protocol)), "{mode}");
        assert_eq!(
            client.selected_intents(&lease).unwrap(),
            (Some(profile), None)
        );
        assert_eq!(fixture.calls(PASSWORD_CHANGE).len(), 1);
        assert!(fixture.calls(REFRESH).is_empty());
        client.detach_main_window(&lease).unwrap();
    }
}

const REGISTER: &str = "/api/native/v1/auth/register";
fn registration() -> crate::NativeRequest {
    crate::NativeRequest::Register {
        username: "fixture".into(),
        display_name: "Native Ω".into(),
        password: password(),
        invite_code: Zeroizing::new("public-test-invite".into()),
    }
}

#[tokio::test(flavor = "current_thread")]
async fn authentication_intent_register_acknowledges_account_only_before_separate_login() {
    let fixture = Fixture::start();
    let (client, lease, profile) = selected_authentication_client(&fixture).await;
    let created = client
        .request_with_authentication_admission(
            &lease,
            Uuid::new_v4(),
            Some(profile),
            None,
            registration(),
            || {},
        )
        .await
        .unwrap();
    let reply = client.commit(&lease, created).unwrap();
    assert_eq!(reply.status, 201);
    assert_eq!(reply.body.as_object().unwrap().len(), 1);
    assert_eq!(reply.body["user"]["display_name"], "Native Ω");
    assert_eq!(
        client.selected_intents(&lease).unwrap(),
        (Some(profile), None)
    );
    assert!(matches!(
        client.authenticated_scope(&lease),
        Err(Error::ReauthRequired)
    ));
    assert!(fixture.calls(LOGIN).is_empty());
    assert!(fixture.calls(REFRESH).is_empty());
    let call = &fixture.calls(REGISTER)[0];
    assert_eq!(call["method"], "POST");
    let headers = call["headers"].as_object().unwrap();
    assert!(!headers.keys().any(|key| matches!(
        key.to_ascii_lowercase().as_str(),
        "authorization" | "origin" | "cookie"
    )));
    let body: Value = serde_json::from_str(call["body"].as_str().unwrap()).unwrap();
    assert_eq!(body.as_object().unwrap().len(), 4);
    assert_eq!(body["invite_code"], "public-test-invite");
    let current = admitted_authentication_login(&client, &lease, profile, None).await;
    assert_eq!(
        client.selected_intents(&lease).unwrap(),
        (Some(profile), Some(current))
    );
    assert!(matches!(
        client
            .request_with_authentication_admission(
                &lease,
                Uuid::new_v4(),
                Some(profile),
                None,
                registration(),
                || {}
            )
            .await,
        Err(Error::Stale)
    ));
    assert!(matches!(
        client
            .request_with_authentication_admission(
                &lease,
                Uuid::new_v4(),
                Some(profile),
                Some(current),
                registration(),
                || {}
            )
            .await,
        Err(Error::Denied)
    ));
    assert_eq!(fixture.calls(REGISTER).len(), 1);
    assert_eq!(fixture.calls(LOGIN).len(), 1);
    client.detach_main_window(&lease).unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn authentication_intent_register_uncertain_ack_never_mints_auth_or_automatically_logs_in() {
    let fixture = Fixture::start();
    let (client, lease, profile) = selected_authentication_client(&fixture).await;
    fixture.control(REGISTER, "register_drop_ack");
    let result = client
        .request_with_authentication_admission(
            &lease,
            Uuid::new_v4(),
            Some(profile),
            None,
            registration(),
            || {},
        )
        .await;
    assert!(matches!(result, Err(Error::Network)));
    assert_eq!(fixture.calls(REGISTER).len(), 1);
    assert!(fixture.calls(LOGIN).is_empty());
    assert_eq!(
        client.selected_intents(&lease).unwrap(),
        (Some(profile), None)
    );
    let current = admitted_authentication_login(&client, &lease, profile, None).await;
    assert_eq!(client.authentication_intent(&lease).unwrap(), Some(current));
    assert_eq!(fixture.calls(REGISTER).len(), 1);
    assert_eq!(fixture.calls(LOGIN).len(), 1);
    client.detach_main_window(&lease).unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn authentication_intent_register_queued_success_is_stale_after_login_family_install() {
    let fixture = Fixture::start();
    let (client, lease, profile) = selected_authentication_client(&fixture).await;
    let created = client
        .request_with_authentication_admission(
            &lease,
            Uuid::new_v4(),
            Some(profile),
            None,
            registration(),
            || {},
        )
        .await
        .unwrap();
    let current = admitted_authentication_login(&client, &lease, profile, None).await;
    assert!(matches!(
        client.commit(&lease, created),
        Err(Error::Stale | Error::Cancelled)
    ));
    assert_eq!(client.authentication_intent(&lease).unwrap(), Some(current));
    assert_eq!(fixture.calls(REGISTER).len(), 1);
    client.detach_main_window(&lease).unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn authentication_intent_register_closed_envelope_never_accepts_secret_or_wrong_account_name()
{
    for mode in ["register_extra_field", "register_wrong_username"] {
        let fixture = Fixture::start();
        let (client, lease, profile) = selected_authentication_client(&fixture).await;
        fixture.control(REGISTER, mode);
        let result = client
            .request_with_authentication_admission(
                &lease,
                Uuid::new_v4(),
                Some(profile),
                None,
                registration(),
                || {},
            )
            .await;
        assert!(matches!(result, Err(Error::Protocol)), "{mode}");
        assert_eq!(
            client.selected_intents(&lease).unwrap(),
            (Some(profile), None)
        );
        assert_eq!(fixture.calls(REGISTER).len(), 1);
        assert!(fixture.calls(LOGIN).is_empty());
        client.detach_main_window(&lease).unwrap();
    }
}

#[path = "admin_metadata_tests.rs"]
mod admin_metadata_tests;

#[tokio::test(flavor = "current_thread")]
async fn authentication_intent_admin_reply_cannot_cross_accepted_role_revision_or_replacement_family()
 {
    let fixture = Fixture::start();
    let (client, lease, profile) = selected_authentication_client(&fixture).await;
    fixture.control(LOGIN, "admin_login");
    let admin = admitted_authentication_login(&client, &lease, profile, None).await;
    let stale_first_admission = client.request_with_authentication_admission(
        &lease,
        Uuid::new_v4(),
        Some(profile),
        Some(admin),
        crate::NativeRequest::AdminMetadata {
            operation: crate::AdminMetadataOperation::Users {},
        },
        || {},
    );
    let current = admitted_authentication_login(&client, &lease, profile, Some(admin)).await;
    assert!(matches!(stale_first_admission.await, Err(Error::Stale)));
    let admin_path = "/api/native/v1/admin/invites";
    fixture.control(admin_path, "hold");
    let pending_client = client.clone();
    let pending_lease = lease.clone();
    let pending = tokio::spawn(async move {
        pending_client
            .request_with_authentication_admission(
                &pending_lease,
                Uuid::new_v4(),
                Some(profile),
                Some(current),
                crate::NativeRequest::AdminMetadata {
                    operation: crate::AdminMetadataOperation::Invites {},
                },
                || {},
            )
            .await
    });
    fixture.wait(admin_path, 1).await;
    fixture.control(ME, "normal");
    let me = client
        .request_with_authentication_admission(
            &lease,
            Uuid::new_v4(),
            Some(profile),
            Some(current),
            crate::NativeRequest::Me,
            || {},
        )
        .await
        .unwrap();
    assert_eq!(client.commit(&lease, me).unwrap().body["role"], "user");
    fixture.release();
    assert!(matches!(pending.await.unwrap(), Err(Error::Stale)));
    assert_eq!(client.authentication_intent(&lease).unwrap(), Some(current));
    assert!(fixture.calls("/api/native/v1/admin/users").is_empty());
    assert_eq!(fixture.calls(admin_path).len(), 1);
    client.detach_main_window(&lease).unwrap();
}

#[cfg(feature = "synthetic-transport-fixture")]
#[tokio::test(flavor = "current_thread")]
async fn authentication_intent_actual_go_password_new_family_revokes_other_session_and_restores_owned_fixture()
 {
    #[derive(serde::Deserialize)]
    #[serde(deny_unknown_fields)]
    struct OwnedBootstrap {
        protocol: String,
        origin: String,
        community_id: String,
        username: String,
        password: String,
        user_id: String,
        public_ca_pem: String,
        content_authorization: String,
    }
    let Ok(path) = std::env::var("MNEMA_NATIVE_PASSWORD_BOOTSTRAP") else {
        return;
    };
    let metadata = std::fs::symlink_metadata(&path).unwrap();
    assert!(metadata.is_file() && !metadata.file_type().is_symlink());
    assert!(metadata.len() <= 16384);
    #[cfg(unix)]
    {
        use std::os::unix::fs::MetadataExt;
        assert_eq!(metadata.uid(), unsafe { libc::geteuid() });
        assert_eq!(metadata.mode() & 0o777, 0o600);
    }
    let bootstrap: OwnedBootstrap = serde_json::from_slice(&std::fs::read(path).unwrap()).unwrap();
    assert_eq!(bootstrap.protocol, "mnema-native-loopback-fixture-v1");
    assert_eq!(bootstrap.content_authorization, "unavailable");
    assert_eq!(bootstrap.username, "relay-user");
    assert_eq!(bootstrap.password, "native-preview-fixture-password");
    let origin = url::Url::parse(&bootstrap.origin).unwrap();
    assert_eq!(origin.scheme(), "https");
    assert_eq!(origin.host_str(), Some("127.0.0.1"));
    assert!(origin.port().is_some());
    let user = Uuid::parse_str(&bootstrap.user_id).unwrap();
    assert!(!user.is_nil());
    async fn connected(
        b: &OwnedBootstrap,
        account: Uuid,
    ) -> (crate::NativeClient, crate::NativeWindowLease, Uuid, Uuid) {
        let client = crate::NativeClient::qualification_fixture(
            b.origin.clone(),
            b.community_id.clone(),
            account,
            b.public_ca_pem.as_bytes().to_vec(),
        )
        .unwrap();
        let lease = client.attach_main_window().unwrap();
        let connected = client
            .request_with_authentication_admission(
                &lease,
                Uuid::new_v4(),
                None,
                None,
                crate::NativeRequest::Connect {
                    address: b.origin.clone(),
                },
                || {},
            )
            .await
            .unwrap();
        let profile = Uuid::parse_str(
            client.commit(&lease, connected).unwrap().body["profile_intent"]
                .as_str()
                .unwrap(),
        )
        .unwrap();
        let login = client
            .request_with_authentication_admission(
                &lease,
                Uuid::new_v4(),
                Some(profile),
                None,
                crate::NativeRequest::Login {
                    username: b.username.clone(),
                    password: Password::from_native_input(b.password.clone()).unwrap(),
                },
                || {},
            )
            .await
            .unwrap();
        let intent = Uuid::parse_str(
            client.commit(&lease, login).unwrap().body["authentication_intent"]
                .as_str()
                .unwrap(),
        )
        .unwrap();
        (client, lease, profile, intent)
    }
    let (client, lease, profile, old) = connected(&bootstrap, user).await;
    let (other, other_lease, other_profile, other_auth) = connected(&bootstrap, user).await;
    let old_scope = client.authenticated_scope_with_intent(&lease, old).unwrap();
    let successor = "native-password-successor-fixture-only";
    let replaced = client
        .request_with_authentication_admission(
            &lease,
            Uuid::new_v4(),
            Some(profile),
            Some(old),
            crate::NativeRequest::Password {
                current_password: Password::from_native_input(bootstrap.password.clone()).unwrap(),
                new_password: Password::from_native_input(successor.into()).unwrap(),
            },
            || {},
        )
        .await
        .unwrap();
    let ack = client.commit(&lease, replaced).unwrap();
    assert_eq!(ack.status, 200);
    assert_eq!(ack.body.as_object().unwrap().len(), 1);
    let current = Uuid::parse_str(ack.body["authentication_intent"].as_str().unwrap()).unwrap();
    assert_ne!(current, old);
    let new_scope = client
        .authenticated_scope_with_intent(&lease, current)
        .unwrap();
    assert_eq!(new_scope.account_id(), user);
    assert_ne!(new_scope.family_id(), old_scope.family_id());
    assert_eq!(
        new_scope.client_instance_id(),
        old_scope.client_instance_id()
    );
    assert_eq!(new_scope.refresh_sequence(), 0);
    assert!(matches!(
        other
            .request_with_authentication_admission(
                &other_lease,
                Uuid::new_v4(),
                Some(other_profile),
                Some(other_auth),
                crate::NativeRequest::Me,
                || {}
            )
            .await,
        Err(Error::Unauthorized)
    ));
    let me = client
        .request_with_authentication_admission(
            &lease,
            Uuid::new_v4(),
            Some(profile),
            Some(current),
            crate::NativeRequest::Me,
            || {},
        )
        .await
        .unwrap();
    assert_eq!(
        client.commit(&lease, me).unwrap().body["id"],
        user.to_string()
    );
    let restore = client
        .request_with_authentication_admission(
            &lease,
            Uuid::new_v4(),
            Some(profile),
            Some(current),
            crate::NativeRequest::Password {
                current_password: Password::from_native_input(successor.into()).unwrap(),
                new_password: Password::from_native_input(bootstrap.password.clone()).unwrap(),
            },
            || {},
        )
        .await
        .unwrap();
    let restored = Uuid::parse_str(
        client.commit(&lease, restore).unwrap().body["authentication_intent"]
            .as_str()
            .unwrap(),
    )
    .unwrap();
    assert_ne!(restored, current);
    let logout = client
        .request_with_authentication_admission(
            &lease,
            Uuid::new_v4(),
            Some(profile),
            Some(restored),
            crate::NativeRequest::Logout,
            || {},
        )
        .await
        .unwrap();
    assert_eq!(client.commit(&lease, logout).unwrap().status, 204);
    client.detach_main_window(&lease).unwrap();
    other.detach_main_window(&other_lease).unwrap();
    let (verify, verify_lease, verify_profile, verify_auth) = connected(&bootstrap, user).await;
    let logout = verify
        .request_with_authentication_admission(
            &verify_lease,
            Uuid::new_v4(),
            Some(verify_profile),
            Some(verify_auth),
            crate::NativeRequest::Logout,
            || {},
        )
        .await
        .unwrap();
    assert_eq!(verify.commit(&verify_lease, logout).unwrap().status, 204);
    verify.detach_main_window(&verify_lease).unwrap();
}

#[tokio::test(flavor = "current_thread")]
async fn author_expired_password_prepare_retires_cancelled_family_descriptor() {
    let fixture = Fixture::start();
    let (broker, owner) = logged(&fixture).await;
    let old = broker.authentication_intent(owner).unwrap().unwrap();
    {
        let mut state = broker.inner.state.lock().unwrap();
        let active = state.active.as_mut().unwrap();
        let grant = Arc::get_mut(&mut active.session.as_mut().unwrap().grant).unwrap();
        grant.access_expires_at = Utc::now() - chrono::Duration::seconds(1);
    }
    let bound = broker
        .bind_invocation(owner, Some(old), Uuid::new_v4(), InvocationKind::Password)
        .unwrap();
    assert!(matches!(
        bound
            .password_delivery(owner, password(), changed_password())
            .await,
        Err(Error::Expired)
    ));
    assert_eq!(fixture.calls(PASSWORD_CHANGE).len(), 0);
    assert_eq!(broker.status().unwrap(), Status::ReauthRequired);
    assert_eq!(broker.authentication_intent(owner).unwrap(), None);
    let retry = broker
        .bind_invocation(owner, None, Uuid::new_v4(), InvocationKind::Login)
        .unwrap();
    let delivery = retry
        .login_delivery(owner, "fixture", password())
        .await
        .unwrap();
    broker.commit_login(owner, delivery).unwrap();
    assert_ne!(broker.authentication_intent(owner).unwrap(), Some(old));
    assert_eq!(broker.status().unwrap(), Status::Authenticated);
    assert_eq!(fixture.calls(LOGIN).len(), 2);
    assert!(matches!(
        broker.bind_invocation(owner, Some(old), Uuid::new_v4(), InvocationKind::Password),
        Err(Error::Stale)
    ));
    assert_eq!(fixture.calls(PASSWORD_CHANGE).len(), 0);
}

#[tokio::test(flavor = "current_thread")]
async fn retained_native_metadata_ticket_cannot_prepare_after_same_family_refresh() {
    let fixture = Fixture::start();
    let (client, lease, profile) = selected_authentication_client(&fixture).await;
    let authentication = admitted_authentication_login(&client, &lease, profile, None).await;
    let original = Arc::new(
        client
            .authenticated_scope_with_intent(&lease, authentication)
            .unwrap(),
    );
    let (started, release) = client.hold_next_prepare();
    let waiting_client = client.clone();
    let waiting_lease = lease.clone();
    let old = original.clone();
    let waiting = tokio::spawn(async move {
        waiting_client
            .request_retained_authenticated(
                &waiting_lease,
                old,
                Uuid::new_v4(),
                crate::NativeRequest::Me,
            )
            .await
    });
    started.await.unwrap();
    let refresh = client
        .request_with_authentication_admission(
            &lease,
            Uuid::new_v4(),
            Some(profile),
            Some(authentication),
            crate::NativeRequest::Refresh,
            || {},
        )
        .await
        .unwrap();
    assert_eq!(client.commit(&lease, refresh).unwrap().status, 204);
    assert_eq!(
        client.authentication_intent(&lease).unwrap(),
        Some(authentication)
    );
    release.send(()).unwrap();
    assert!(matches!(waiting.await.unwrap(), Err(Error::Stale)));
    assert_eq!(fixture.calls(ME).len(), 0);
    let current = Arc::new(
        client
            .authenticated_scope_with_intent(&lease, authentication)
            .unwrap(),
    );
    let me = client
        .request_retained_authenticated(&lease, current, Uuid::new_v4(), crate::NativeRequest::Me)
        .await
        .unwrap();
    assert_eq!(client.commit(&lease, me).unwrap().status, 200);
    assert_eq!(fixture.calls(ME).len(), 1);
    assert!(matches!(
        client
            .request_retained_authenticated(
                &lease,
                original,
                Uuid::new_v4(),
                crate::NativeRequest::Me
            )
            .await,
        Err(Error::Stale)
    ));
    assert_eq!(fixture.calls(ME).len(), 1);
    client.detach_main_window(&lease).unwrap();
}
