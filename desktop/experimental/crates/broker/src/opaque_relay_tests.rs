use super::*;
use crate::{NativeOpaqueEvent, NativeOpaqueRelayOperation};
const CHANNEL: &str = "66666666-6666-6666-6666-666666666666";
const EVENT: &str = "77777777-7777-7777-7777-777777777777";
fn channel() -> Uuid {
    Uuid::parse_str(CHANNEL).unwrap()
}
fn event() -> NativeOpaqueRelayOperation {
    NativeOpaqueRelayOperation::Publish(
        NativeOpaqueEvent::from_native_outbox(
            channel(),
            Uuid::parse_str(EVENT).unwrap(),
            b"native-test-group",
            b"public-fixture-opaque-bytes",
        )
        .unwrap(),
    )
}
fn post() -> String {
    format!("/api/native/v1/channels/{CHANNEL}/ciphertext-events")
}
fn page() -> String {
    format!("{}?after=0&limit=10", post())
}

#[tokio::test(flavor = "current_thread")]
async fn opaque_fixed_tls_publish_receipt_page_and_explicit_same_bytes_retry() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    let delivery = b
        .opaque_relay(w, b.authenticated_scope(w).unwrap(), event())
        .await
        .unwrap();
    assert!(delivery.receipt().is_publish_ack());
    assert_eq!(
        delivery.receipt().records()[0].ciphertext(),
        b"public-fixture-opaque-bytes"
    );
    b.with_opaque_relay_publication(w, delivery, |receipt| assert_eq!(receipt.next_after(), 1))
        .unwrap();
    let duplicate = b
        .opaque_relay(w, b.authenticated_scope(w).unwrap(), event())
        .await
        .unwrap();
    assert_eq!(duplicate.receipt().records()[0].number(), 1);
    let delivery = b
        .opaque_relay(
            w,
            b.authenticated_scope(w).unwrap(),
            NativeOpaqueRelayOperation::Page {
                channel: channel(),
                after: 0,
            },
        )
        .await
        .unwrap();
    assert_eq!(delivery.receipt().records().len(), 1);
    assert!(!delivery.receipt().is_publish_ack());
    b.with_opaque_relay_publication(w, delivery, |receipt| {
        assert_eq!(receipt.records()[0].client_event_id().to_string(), EVENT)
    })
    .unwrap();
    let calls = f.calls(&post());
    assert_eq!(calls.len(), 2);
    assert_eq!(calls[0]["body"], calls[1]["body"]);
    let calls = f.calls(&page());
    assert_eq!(calls.len(), 1);
    assert_eq!(calls[0]["body"], "");
    for call in f.state()["calls"]
        .as_array()
        .unwrap()
        .iter()
        .filter(|x| x["path"].as_str().unwrap().contains("ciphertext-events"))
    {
        let headers = call["headers"].as_object().unwrap();
        assert!(
            !headers
                .keys()
                .any(|k| k.eq_ignore_ascii_case("cookie") || k.eq_ignore_ascii_case("origin"))
        );
        assert!(
            headers
                .keys()
                .any(|k| k.eq_ignore_ascii_case("authorization"))
        );
    }
}
#[tokio::test(flavor = "current_thread")]
async fn opaque_input_bounds_and_stale_native_scope_do_not_dispatch() {
    for (c, e, g, v) in [
        (Uuid::nil(), Uuid::new_v4(), vec![1], vec![1]),
        (channel(), Uuid::nil(), vec![1], vec![1]),
        (channel(), Uuid::new_v4(), vec![], vec![1]),
        (channel(), Uuid::new_v4(), vec![1; 129], vec![1]),
        (channel(), Uuid::new_v4(), vec![1], vec![1; 65537]),
    ] {
        assert!(NativeOpaqueEvent::from_native_outbox(c, e, &g, &v).is_err());
    }
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    let scope = b.authenticated_scope(w).unwrap();
    b.refresh(w).await.unwrap();
    assert!(matches!(
        b.opaque_relay(w, scope, event()).await,
        Err(Error::Stale)
    ));
    assert!(f.calls(&post()).is_empty());
    assert!(matches!(
        b.opaque_relay(
            w,
            b.authenticated_scope(w).unwrap(),
            NativeOpaqueRelayOperation::Page {
                channel: channel(),
                after: -1
            }
        )
        .await,
        Err(Error::InvalidInput)
    ));
}
#[tokio::test(flavor = "current_thread")]
async fn opaque_unknown_receipts_origin_redirect_large_body_and_sender_mismatch_fail_once() {
    for mode in [
        "redirect",
        "relay_wrong_author",
        "relay_bad_group",
        "relay_unknown_field",
        "relay_oversized",
    ] {
        let f = Fixture::start();
        let (b, w) = logged(&f).await;
        f.control(&post(), mode);
        assert!(
            b.opaque_relay(w, b.authenticated_scope(w).unwrap(), event())
                .await
                .is_err(),
            "{mode}"
        );
        assert_eq!(f.calls(&post()).len(), 1, "{mode}");
        assert_eq!(
            f.state()["calls"]
                .as_array()
                .unwrap()
                .iter()
                .filter(|c| c["port"] == f.port2)
                .count(),
            0
        );
    }
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    f.control(&page(), "relay_bad_cursor");
    assert!(matches!(
        b.opaque_relay(
            w,
            b.authenticated_scope(w).unwrap(),
            NativeOpaqueRelayOperation::Page {
                channel: channel(),
                after: 0
            }
        )
        .await,
        Err(Error::Protocol)
    ));
}
#[tokio::test(flavor = "current_thread")]
async fn opaque_lost_ack_never_automatically_retries_and_native_explicit_durable_retry_is_identical()
 {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    f.control(&post(), "relay_drop_ack");
    assert!(matches!(
        b.opaque_relay(w, b.authenticated_scope(w).unwrap(), event())
            .await,
        Err(Error::Network)
    ));
    assert_eq!(f.calls(&post()).len(), 1);
    f.control(&post(), "normal");
    let receipt = b
        .opaque_relay(w, b.authenticated_scope(w).unwrap(), event())
        .await
        .unwrap();
    assert_eq!(receipt.receipt().records()[0].number(), 1);
    let calls = f.calls(&post());
    assert_eq!(calls.len(), 2);
    assert_eq!(calls[0]["body"], calls[1]["body"]);
}
#[tokio::test(flavor = "current_thread")]
async fn opaque_late_success_and_delivered_ack_cannot_publish_after_native_role_retirement() {
    let f = Fixture::start();
    f.control(LOGIN, "admin_login");
    let (b, w) = logged(&f).await;
    f.control(&post(), "hold");
    let scope = b.authenticated_scope(w).unwrap();
    let pending = b.opaque_relay(w, scope, event());
    tokio::pin!(pending);
    let fixed_post = post();
    tokio::select! {r=&mut pending=>panic!("must hold {r:?}"),_=f.wait(&fixed_post,1)=>{}}
    f.control(ME, "normal");
    b.me(w).await.unwrap();
    f.release();
    assert!(matches!(pending.await, Err(Error::Stale)));
    f.control(&post(), "normal");
    let delivery = b
        .opaque_relay(w, b.authenticated_scope(w).unwrap(), event())
        .await
        .unwrap();
    b.refresh(w).await.unwrap();
    let called = std::cell::Cell::new(false);
    assert!(matches!(
        b.with_opaque_relay_publication(w, delivery, |_| called.set(true)),
        Err(Error::Stale)
    ));
    assert!(!called.get());
}
#[tokio::test(flavor = "current_thread")]
async fn opaque_actor_generic_ui_commit_is_denied_and_actual_window_replacement_retires_receipt() {
    let f = Fixture::start();
    let client = NativeClient::fixture(f.root.clone());
    let w = client.attach_main_window().unwrap();
    let connected = client
        .request(
            &w,
            NativeRequest::Connect {
                address: f.origin(),
            },
        )
        .await
        .unwrap();
    client.commit(&w, connected).unwrap();
    let login = client
        .request(
            &w,
            NativeRequest::Login {
                username: "fixture".to_owned(),
                password: Password::from_native_input("public-fixture-password".to_owned())
                    .unwrap(),
            },
        )
        .await
        .unwrap();
    client.commit(&w, login).unwrap();
    let reply = client
        .request_opaque_relay(&w, client.authenticated_scope(&w).unwrap(), event())
        .await
        .unwrap();
    assert!(matches!(
        client.commit(&w, reply),
        Err(Error::QualificationRequired)
    ));
    let reply = client
        .request_opaque_relay(&w, client.authenticated_scope(&w).unwrap(), event())
        .await
        .unwrap();
    assert!(reply.opaque_relay_receipt().is_some());
    client.attach_main_window().unwrap();
    let called = std::cell::Cell::new(false);
    assert!(matches!(
        client.with_opaque_relay_publication(&w, reply, |_| called.set(true)),
        Err(Error::Stale)
    ));
    assert!(!called.get());
}
#[tokio::test(flavor = "current_thread")]
async fn opaque_maximum_wire_body_is_bounded_and_debug_never_contains_bytes() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    let value =
        NativeOpaqueEvent::from_native_outbox(channel(), Uuid::new_v4(), &[9; 128], &[11; 65536])
            .unwrap();
    assert_eq!(format!("{value:?}"), "NativeOpaqueEvent(REDACTED)");
    let delivery = b
        .opaque_relay(
            w,
            b.authenticated_scope(w).unwrap(),
            NativeOpaqueRelayOperation::Publish(value),
        )
        .await
        .unwrap();
    assert_eq!(delivery.receipt().records()[0].ciphertext().len(), 65536);
    assert_eq!(format!("{delivery:?}"), "NativeOpaqueDelivery(REDACTED)");
    assert_eq!(
        format!("{:?}", delivery.receipt()),
        "NativeOpaqueReceipt(REDACTED)"
    );
    let calls = f.calls(&post());
    assert_eq!(calls.len(), 1);
    assert!(calls[0]["body"].as_str().unwrap().len() > 16 * 1024);
    assert!(calls[0]["body"].as_str().unwrap().len() < 96 * 1024);
}
#[tokio::test(flavor = "current_thread")]
async fn opaque_profile_switch_cancels_held_request_and_current_401_retires_auth() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    f.control(&post(), "hold");
    let pending = b.opaque_relay(w, b.authenticated_scope(w).unwrap(), event());
    tokio::pin!(pending);
    let fixed_post = post();
    tokio::select! {r=&mut pending=>panic!("must hold {r:?}"),_=f.wait(&fixed_post,1)=>{}}
    let second_origin = f.second_origin();
    let (switched, result) = tokio::join!(
        b.select_confirmed_profile(w, &second_origin, "second-community"),
        &mut pending
    );
    switched.unwrap();
    assert!(matches!(result, Err(Error::Stale) | Err(Error::Cancelled)));
    assert_eq!(b.status().unwrap(), Status::Selected);
    f.release();
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    f.control(&post(), "unauthorized");
    assert!(matches!(
        b.opaque_relay(w, b.authenticated_scope(w).unwrap(), event())
            .await,
        Err(Error::Unauthorized)
    ));
    assert_eq!(b.status().unwrap(), Status::ReauthRequired);
    assert!(matches!(
        b.authenticated_scope(w),
        Err(Error::ReauthRequired)
    ));
}
#[tokio::test(flavor = "current_thread")]
async fn opaque_page_event_ids_are_unique_per_authenticated_account_not_globally() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    b.opaque_relay(w, b.authenticated_scope(w).unwrap(), event())
        .await
        .unwrap();
    f.control(&page(), "relay_two_account_event");
    let accepted = b
        .opaque_relay(
            w,
            b.authenticated_scope(w).unwrap(),
            NativeOpaqueRelayOperation::Page {
                channel: channel(),
                after: 0,
            },
        )
        .await
        .unwrap();
    let records = accepted.receipt().records();
    assert_eq!(records.len(), 2);
    assert_eq!(records[0].client_event_id(), records[1].client_event_id());
    assert_ne!(records[0].account_id(), records[1].account_id());
    f.control(&page(), "relay_duplicate_account_event");
    assert!(matches!(
        b.opaque_relay(
            w,
            b.authenticated_scope(w).unwrap(),
            NativeOpaqueRelayOperation::Page {
                channel: channel(),
                after: 0
            }
        )
        .await,
        Err(Error::Protocol)
    ));
}

#[tokio::test(flavor = "current_thread")]
async fn retained_relay_keeps_exact_admitted_scope_through_http_and_final_enqueue() {
    let fixture = Fixture::start();
    let (client, lease, profile) = selected_authentication_client(&fixture).await;
    let authentication = admitted_authentication_login(&client, &lease, profile, None).await;
    let original = Arc::new(
        client
            .authenticated_scope_with_intent(&lease, authentication)
            .unwrap(),
    );
    let deadline = original.monotonic_access_deadline();
    assert_eq!(Arc::strong_count(&original), 1);
    let receipt = client
        .request_opaque_relay_retained(&lease, original.clone(), event())
        .await
        .unwrap();
    assert_eq!(Arc::strong_count(&original), 2);
    assert_eq!(original.monotonic_access_deadline(), deadline);
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
    client.commit(&lease, refresh).unwrap();
    assert_eq!(
        client.authentication_intent(&lease).unwrap(),
        Some(authentication)
    );
    let published = std::cell::Cell::new(false);
    assert!(matches!(
        client.with_opaque_relay_publication(&lease, receipt, |_| published.set(true)),
        Err(Error::Stale)
    ));
    assert!(!published.get());
    assert_eq!(Arc::strong_count(&original), 1);
    assert!(matches!(
        client
            .request_opaque_relay_retained(&lease, original.clone(), event())
            .await,
        Err(Error::Stale)
    ));
    assert_eq!(fixture.calls(&post()).len(), 1);
    let current = Arc::new(
        client
            .authenticated_scope_with_intent(&lease, authentication)
            .unwrap(),
    );
    let receipt = client
        .request_opaque_relay_retained(&lease, current.clone(), event())
        .await
        .unwrap();
    client
        .with_opaque_relay_publication(&lease, receipt, |_| published.set(true))
        .unwrap();
    assert!(published.get());
    assert_eq!(fixture.calls(&post()).len(), 2);
    client.detach_main_window(&lease).unwrap();
}
