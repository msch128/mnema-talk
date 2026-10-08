//! Explicit synthetic CA/auth tests; never invoke a dialog/Keychain or replace
//! the actual OS callback qualification. Private test-only decision construction.
use super::*;
use mnema_private_native_client_broker::{
    NativeClient, NativeRequest, NativeWindowLease, Password,
};
use std::{
    fs,
    io::{BufRead, BufReader},
    path::PathBuf,
    process::{Child, Command, Stdio},
    time::Duration,
};
struct Fixture {
    child: Child,
    path: PathBuf,
    origin: String,
    root: Vec<u8>,
}
impl Fixture {
    fn start(admin: bool) -> Self {
        let dir = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
        let path = dir.join("test-runtime").join(Uuid::new_v4().to_string());
        fs::create_dir_all(path.parent().unwrap()).unwrap();
        let mut child = Command::new("python3")
            .arg(dir.join("../native-client-integration/broker/tests/https_fixture.py"))
            .arg(&path)
            .stdout(Stdio::piped())
            .stderr(Stdio::null())
            .spawn()
            .unwrap();
        let mut line = String::new();
        BufReader::new(child.stdout.take().unwrap())
            .read_line(&mut line)
            .unwrap();
        let value: serde_json::Value = serde_json::from_str(&line).unwrap();
        let origin = format!("https://127.0.0.1:{}", value["port"].as_u64().unwrap());
        let root = fs::read(path.join("ca.pem")).unwrap();
        if admin {
            fs::write(
                path.join("control.json"),
                br#"{"/api/native/v1/auth/login":{"mode":"admin_login"}}"#,
            )
            .unwrap();
        }
        Self {
            child,
            path,
            origin,
            root,
        }
    }
    async fn logged(
        &self,
    ) -> (
        NativeClient,
        NativeWindowLease,
        Arc<NativeAuthenticatedScope>,
    ) {
        let client = NativeClient::qualification_fixture(
            self.origin.clone(),
            "fixture-community".to_owned(),
            Uuid::parse_str("11111111-1111-1111-1111-111111111111").unwrap(),
            self.root.clone(),
        )
        .unwrap();
        let window = client.attach_main_window().unwrap();
        let connected = client
            .request(
                &window,
                NativeRequest::Connect {
                    address: self.origin.clone(),
                },
            )
            .await
            .unwrap();
        client.commit(&window, connected).unwrap();
        let login = client
            .request(
                &window,
                NativeRequest::Login {
                    username: "fixture".to_owned(),
                    password: Password::from_native_input("public-fixture-password".to_owned())
                        .unwrap(),
                },
            )
            .await
            .unwrap();
        client.commit(&window, login).unwrap();
        let scope = Arc::new(client.authenticated_scope(&window).unwrap());
        (client, window, scope)
    }
}
impl Drop for Fixture {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
        let _ = fs::remove_dir_all(&self.path);
    }
}
fn facts() -> NativeTrustDisplayFacts {
    NativeTrustDisplayFacts::from_native_core(
        Uuid::new_v4(),
        Uuid::new_v4(),
        b"fixture-public-group",
        [1; 32],
        [2; 32],
        [3; 32],
    )
    .unwrap()
}
#[tokio::test(flavor = "current_thread")]
async fn native_request_uses_real_admin_scope_and_canonical_full_public_facts() {
    let f = Fixture::start(true);
    let (_client, _window, scope) = f.logged().await;
    let request = NativeDialogRequest::first_root(
        Uuid::new_v4(),
        scope.clone(),
        facts(),
        Instant::now() + Duration::from_secs(30),
    )
    .unwrap();
    let text = request.text().unwrap();
    assert!(text.contains(&format!("Origin: {}\n", f.origin)));
    assert!(text.contains(&scope.account_id().to_string()));
    assert!(text.contains(&"02".repeat(32)));
    assert!(text.contains(&"01".repeat(32)));
    assert!(text.contains(&"03".repeat(32)));
    assert!(!text.contains("Bearer"));
    assert!(!text.contains("public-fixture-password"));
    assert_eq!(format!("{request:?}"), "NativeDialogRequest(REDACTED)");
}
#[tokio::test(flavor = "current_thread")]
async fn native_decision_matches_exact_allocation_and_cancellation_is_not_approval() {
    let f = Fixture::start(true);
    let (_client, _window, scope) = f.logged().await;
    let operation = Uuid::new_v4();
    let request = NativeDialogRequest::first_root(
        operation,
        scope.clone(),
        facts(),
        Instant::now() + Duration::from_secs(30),
    )
    .unwrap();
    let other =
        NativeDialogRequest::first_root(operation, scope, facts(), request.deadline()).unwrap();
    let approved = NativeDialogDecision {
        request: request.clone(),
        approved: true,
        cancelled: Arc::new(AtomicBool::new(false)),
    };
    assert!(!approved.matches_request(&other));
    assert!(matches!(approved.consume(&other), Err(DialogError::Denied)));
    let denied = NativeDialogDecision {
        request: request.clone(),
        approved: false,
        cancelled: Arc::new(AtomicBool::new(false)),
    };
    assert!(matches!(
        denied.consume(&request),
        Err(DialogError::Cancelled)
    ));
    let approved = NativeDialogDecision {
        request: request.clone(),
        approved: true,
        cancelled: Arc::new(AtomicBool::new(false)),
    };
    assert!(approved.matches_request(&request.clone()));
    approved.consume(&request).unwrap();
}
#[tokio::test(flavor = "current_thread")]
async fn nonadmin_invalid_fields_and_unbounded_deadlines_cannot_prepare_native_root_prompt() {
    let f = Fixture::start(false);
    let (_client, _window, scope) = f.logged().await;
    assert!(
        NativeDialogRequest::first_root(
            Uuid::new_v4(),
            scope,
            facts(),
            Instant::now() + Duration::from_secs(30)
        )
        .is_err()
    );
    for (channel, device, group, root, key) in [
        (Uuid::nil(), Uuid::new_v4(), vec![1], [1; 32], [3; 32]),
        (Uuid::new_v4(), Uuid::nil(), vec![1], [1; 32], [3; 32]),
        (
            Uuid::new_v4(),
            Uuid::new_v4(),
            vec![1; 129],
            [1; 32],
            [3; 32],
        ),
        (Uuid::new_v4(), Uuid::new_v4(), vec![], [1; 32], [3; 32]),
        (Uuid::new_v4(), Uuid::new_v4(), vec![1], [0; 32], [3; 32]),
        (Uuid::new_v4(), Uuid::new_v4(), vec![1], [1; 32], [0; 32]),
    ] {
        assert!(
            NativeTrustDisplayFacts::from_native_core(channel, device, &group, root, [2; 32], key)
                .is_err()
        );
    }
    let f = Fixture::start(true);
    let (_client, _window, scope) = f.logged().await;
    assert!(
        NativeDialogRequest::first_root(
            Uuid::nil(),
            scope.clone(),
            facts(),
            Instant::now() + Duration::from_secs(30)
        )
        .is_err()
    );
    assert!(
        NativeDialogRequest::first_root(
            Uuid::new_v4(),
            scope.clone(),
            facts(),
            scope.monotonic_access_deadline() + Duration::from_secs(1)
        )
        .is_err()
    );
    assert!(
        NativeDialogRequest::first_root(
            Uuid::new_v4(),
            scope,
            facts(),
            Instant::now() - Duration::from_secs(1)
        )
        .is_err()
    );
}
#[tokio::test(flavor = "current_thread")]
async fn default_service_denies_and_actual_window_retirement_remains_native_gate() {
    let f = Fixture::start(true);
    let (client, window, scope) = f.logged().await;
    let request = NativeDialogRequest::first_root(
        Uuid::new_v4(),
        scope.clone(),
        facts(),
        Instant::now() + Duration::from_secs(30),
    )
    .unwrap();
    assert!(matches!(
        DeniedNativeTrustDialog.confirm_first_root(request).await,
        Err(DialogError::Unavailable)
    ));
    client.detach_main_window(&window).unwrap();
    assert!(client.check_authenticated_scope(&window, &scope).is_err());
}

#[tokio::test(flavor = "current_thread")]
async fn cancellation_after_native_result_before_consumption_denies_exact_decision() {
    let f = Fixture::start(true);
    let (_client, _window, scope) = f.logged().await;
    let request = NativeDialogRequest::first_root(
        Uuid::new_v4(),
        scope,
        facts(),
        Instant::now() + Duration::from_secs(30),
    )
    .unwrap();
    let cancelled = Arc::new(AtomicBool::new(false));
    // Model the narrow callback interval: the native OS result was approved,
    // but successful cancellation still owns the same pending operation latch.
    let decision = NativeDialogDecision {
        request: request.clone(),
        approved: true,
        cancelled: cancelled.clone(),
    };
    let ready = Arc::new(std::sync::Barrier::new(2));
    let finished = Arc::new(std::sync::Barrier::new(2));
    let worker_ready = ready.clone();
    let worker_finished = finished.clone();
    let worker = std::thread::spawn(move || {
        worker_ready.wait();
        cancelled.store(true, Ordering::Release);
        worker_finished.wait();
    });
    ready.wait();
    finished.wait();
    worker.join().unwrap();
    assert!(matches!(
        decision.consume(&request),
        Err(DialogError::Cancelled)
    ));
}
