use super::*;
use crate::{AdminMetadataOperation as A, NativeRequest};
const TARGET: &str = "22222222-2222-2222-2222-222222222222";
const PREFIX: &str = "/api/native/v1/admin/";
fn operation(v: serde_json::Value) -> A {
    serde_json::from_value(v).unwrap()
}
fn cases() -> Vec<(A, String, &'static str, u16)> {
    vec![
        (json!({"operation":"invites"}),"invites".into(),"GET",200),
        (json!({"operation":"create_invite","code":"FixtureCode","max_uses":1,"expires_in_hours":24}),"invites".into(),"POST",201),
        (json!({"operation":"delete_invite","id":TARGET}),format!("invites/{TARGET}"),"DELETE",204),
        (json!({"operation":"users"}),"users".into(),"GET",200),
        (json!({"operation":"disable_user","id":TARGET}),format!("users/{TARGET}/disable"),"POST",204),
        (json!({"operation":"enable_user","id":TARGET}),format!("users/{TARGET}/enable"),"POST",204),
        (json!({"operation":"revoke_user_sessions","id":TARGET}),format!("users/{TARGET}/sessions/revoke"),"POST",204),
        (json!({"operation":"kick_user","id":TARGET}),format!("users/{TARGET}/kick"),"POST",204),
        (json!({"operation":"user_status","id":TARGET,"status_text":"synthetic"}),format!("users/{TARGET}/status"),"PUT",200),
        (json!({"operation":"create_category","name":"fixture","sort_order":1}),"categories".into(),"POST",201),
        (json!({"operation":"delete_category","id":TARGET}),format!("categories/{TARGET}"),"DELETE",204),
        (json!({"operation":"rename_category","id":TARGET,"name":"renamed"}),format!("categories/{TARGET}"),"PATCH",200),
        (json!({"operation":"create_channel","category_id":null,"name":"voice","kind":"voice","topic":"topic","sort_order":1}),"channels".into(),"POST",201),
        (json!({"operation":"delete_channel","id":TARGET}),format!("channels/{TARGET}"),"DELETE",204),
        (json!({"operation":"update_channel","id":TARGET,"name":"renamed","topic":"topic","user_limit":2}),format!("channels/{TARGET}"),"PATCH",200),
        (json!({"operation":"duplicate_channel","id":TARGET}),format!("channels/{TARGET}/duplicate"),"POST",201),
        (json!({"operation":"layout","categories":[],"channels":[]}),"layout".into(),"PUT",204),
        (json!({"operation":"system"}),"system".into(),"GET",200),
        (json!({"operation":"system_update"}),"system/update".into(),"GET",200),
    ].into_iter().map(|(v,p,m,s)|(operation(v),p,m,s)).collect()
}
fn rename() -> A {
    operation(json!({"operation":"rename_category","id":TARGET,"name":"test"}))
}

#[tokio::test(flavor = "current_thread")]
async fn admin_all19_fixed_https_methods_bodies_statuses_and_closed_dtos() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    for (op, tail, method, status) in cases() {
        let path = format!("{PREFIX}{tail}");
        let before = f.calls(&path).len();
        let d = b.admin_metadata(w, op).await.unwrap();
        let reply = b.commit_admin(w, d).unwrap();
        assert_eq!(reply.status, status);
        assert_eq!(reply.body.is_null(), status == 204);
        let calls = f.calls(&path);
        assert_eq!(calls.len(), before + 1);
        let call = &calls[before];
        assert_eq!(call["method"], method);
        let headers = call["headers"].as_object().unwrap();
        assert!(
            headers
                .keys()
                .any(|k| k.eq_ignore_ascii_case("Authorization"))
        );
        assert!(
            !headers
                .keys()
                .any(|k| k.eq_ignore_ascii_case("Origin") || k.eq_ignore_ascii_case("Cookie"))
        );
        assert!(!call["path"].as_str().unwrap().contains('?'));
        if method == "GET"
            || method == "DELETE"
            || (method == "POST"
                && (tail.ends_with("/disable")
                    || tail.ends_with("/enable")
                    || tail.ends_with("/revoke")
                    || tail.ends_with("/kick")
                    || tail.ends_with("/duplicate")))
        {
            assert_eq!(call["body"], "");
        }
    }
    // A foreign user's admin reply never replaces this account or its role.
    let me = b.me(w).await.unwrap();
    assert_eq!(me.role, "user");
    assert_ne!(me.account_id().to_string(), TARGET);
}
#[tokio::test(flavor = "current_thread")]
async fn admin_invalid_requests_bounds_ids_and_layout_reject_without_network() {
    for v in [
        json!({"operation":"users","url":"https://outside.example.invalid"}),
        json!({"operation":"password_reset","id":TARGET}),
        json!({"operation":"system_update","method":"POST"}),
        json!({"operation":"create_channel","name":"x","kind":"video","topic":"","sort_order":0,"category_id":null}),
        json!({"operation":"layout","categories":[{"id":TARGET,"sort_order":0,"unknown":true}],"channels":[]}),
    ] {
        assert!(serde_json::from_value::<A>(v).is_err());
    }
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    let before = f.state()["calls"].as_array().unwrap().len();
    for v in [
        json!({"operation":"delete_channel","id":"https://outside.example.invalid"}),
        json!({"operation":"enable_user","id":"00000000-0000-0000-0000-000000000000"}),
        json!({"operation":"create_invite","code":"bad","max_uses":0,"expires_in_hours":0}),
        json!({"operation":"create_category","name":"🦀".repeat(65),"sort_order":0}),
        json!({"operation":"user_status","id":TARGET,"status_text":"x".repeat(33)}),
        json!({"operation":"update_channel","id":TARGET,"name":null,"topic":null,"user_limit":1000}),
        json!({"operation":"layout","categories":[{"id":TARGET,"sort_order":0},{"id":TARGET,"sort_order":1}],"channels":[]}),
        json!({"operation":"layout","categories":(0..501).map(|_|json!({"id":TARGET,"sort_order":0})).collect::<Vec<_>>(),"channels":[]}),
    ] {
        assert!(matches!(
            b.admin_metadata(w, operation(v)).await,
            Err(Error::InvalidInput) | Err(Error::BodyLimit)
        ));
    }
    assert_eq!(f.state()["calls"].as_array().unwrap().len(), before);
    // Actual server permits 500 layout entries. A valid full-size layout must
    // pass the bounded native body limit rather than silently cutting parity.
    let channels=(0..500).map(|i|json!({"id":Uuid::from_u128(i+1).hyphenated().to_string(),"category_id":TARGET,"sort_order":i})).collect::<Vec<_>>();
    let reply = b
        .commit_admin(
            w,
            b.admin_metadata(
                w,
                operation(json!({"operation":"layout","categories":[],"channels":channels})),
            )
            .await
            .unwrap(),
        )
        .unwrap();
    assert_eq!(reply.status, 204);
    let calls = f.calls(&format!("{PREFIX}layout"));
    let n = calls.last().unwrap()["body"].as_str().unwrap().len();
    assert!(n > 16384 && n <= 65536);
}
#[tokio::test(flavor = "current_thread")]
async fn admin_wrong_target_unknown_reply_status_redirect_and_204_body_fail_closed() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    let path = format!("{PREFIX}categories/{TARGET}");
    for mode in [
        "admin_unknown_field",
        "admin_bad_target",
        "admin_wrong_status",
        "redirect",
        "cookie",
        "gzip",
        "html",
        "metadata_oversized",
    ] {
        f.control(&path, mode);
        let before = f.calls(&path).len();
        assert!(b.admin_metadata(w, rename()).await.is_err());
        assert_eq!(f.calls(&path).len(), before + 1);
    }
    let system = format!("{PREFIX}system");
    for mode in [
        "admin_system_unknown",
        "admin_system_negative",
        "admin_system_bad_reach",
    ] {
        f.control(&system, mode);
        assert!(
            b.admin_metadata(w, operation(json!({"operation":"system"})))
                .await
                .is_err()
        );
    }
    let users = format!("{PREFIX}users");
    f.control(&users, "admin_users_unknown");
    assert!(
        b.admin_metadata(w, operation(json!({"operation":"users"})))
            .await
            .is_err()
    );
    let path = format!("{PREFIX}users/{TARGET}/disable");
    f.control(&path, "admin_invalid204");
    assert!(
        b.admin_metadata(
            w,
            operation(json!({"operation":"disable_user","id":TARGET}))
        )
        .await
        .is_err()
    );
    assert_eq!(b.status().unwrap(), Status::Authenticated);
}
#[tokio::test(flavor = "current_thread")]
async fn admin_server_denial_codes_are_redacted_no_retry_and_401_only_reauths_current() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    let path = format!("{PREFIX}categories/{TARGET}");
    for status in [400, 403, 404, 409, 429, 500, 503] {
        f.control(&path, &format!("admin_error_{status}"));
        let before = f.calls(&path).len();
        let r = b
            .commit_admin(w, b.admin_metadata(w, rename()).await.unwrap())
            .unwrap();
        assert_eq!(r.status, status);
        assert!(
            !r.body
                .to_string()
                .contains("public-synthetic-marker-do-not-forward")
        );
        assert_eq!(f.calls(&path).len(), before + 1);
        assert_eq!(b.status().unwrap(), Status::Authenticated);
    }
    f.control(&path, "unauthorized");
    assert!(matches!(
        b.admin_metadata(w, rename()).await,
        Err(Error::Unauthorized)
    ));
    assert_eq!(b.status().unwrap(), Status::ReauthRequired);
    let (b, w) = logged(&f).await;
    f.control(&path, "hold_unauthorized");
    let run = b.admin_metadata(w, rename());
    tokio::pin!(run);
    tokio::select! {r=&mut run=>panic!("held admin completed early: {}",r.is_ok()),_=f.wait(&path,9)=>{}}
    let origin = f.second_origin();
    let (r, selection) = tokio::join!(run, b.select_confirmed_profile(w, &origin, "other"));
    selection.unwrap();
    assert!(matches!(r, Err(Error::Stale) | Err(Error::Cancelled)));
    assert_eq!(b.status().unwrap(), Status::Selected);
    f.release();
}
#[tokio::test(flavor = "current_thread")]
async fn admin_foreign_window_switch_and_actor_cancellation_cannot_publish() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    let d = b.admin_metadata(w, rename()).await.unwrap();
    let other = b
        .register_native_window("side", WindowRole::Widget)
        .unwrap();
    assert!(matches!(b.commit_admin(other, d), Err(Error::Denied)));
    let d = b.admin_metadata(w, rename()).await.unwrap();
    b.select_confirmed_profile(w, &f.second_origin(), "other")
        .await
        .unwrap();
    assert!(matches!(
        b.commit_admin(w, d),
        Err(Error::Stale) | Err(Error::Cancelled)
    ));
    let client = NativeClient::fixture(f.root.clone());
    let lease = client.attach_main_window().unwrap();
    let p = client
        .request(
            &lease,
            NativeRequest::Connect {
                address: f.origin(),
            },
        )
        .await
        .unwrap();
    client.commit(&lease, p).unwrap();
    let p = client
        .request(
            &lease,
            NativeRequest::Login {
                username: "fixture".into(),
                password: password(),
            },
        )
        .await
        .unwrap();
    client.commit(&lease, p).unwrap();
    let path = format!("{PREFIX}categories/{TARGET}");
    f.control(&path, "hold");
    let request_id = Uuid::new_v4();
    let op = client.request_with_id(
        &lease,
        request_id,
        NativeRequest::AdminMetadata {
            operation: rename(),
        },
    );
    tokio::pin!(op);
    tokio::select! {r=&mut op=>panic!("held admin completed early: {}",r.is_ok()),_=f.wait(&path,3)=>{}}
    client.cancel_request(&lease, request_id).unwrap();
    assert!(matches!(op.await, Err(Error::Cancelled)));
    f.release();
}
