use super::*;
use crate::{MetadataLocale, MetadataPresence, PersonalMetadataOperation};

const PROFILE: &str = "/api/native/v1/users/me/profile";
const LOCALE: &str = "/api/native/v1/users/me/locale";
const PRESENCE: &str = "/api/native/v1/users/me/presence";
const STATUS: &str = "/api/native/v1/users/me/status";
fn profile() -> PersonalMetadataOperation {
    PersonalMetadataOperation::Profile {
        display_name: "Ö🦀".repeat(12),
        bio: "é🦀".repeat(125),
    }
}

#[tokio::test(flavor = "current_thread")]
async fn personal_five_fixed_https_routes_validate_wire_and_canonical_target() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    for (input, path, field, expected) in [
        (profile(), PROFILE, "display_name", json!("Ö🦀".repeat(12))),
        (
            PersonalMetadataOperation::Locale {
                locale: MetadataLocale::De,
            },
            LOCALE,
            "locale",
            json!("de"),
        ),
        (
            PersonalMetadataOperation::Presence {
                presence: MetadataPresence::Focus,
            },
            PRESENCE,
            "presence",
            json!("focus"),
        ),
        (
            PersonalMetadataOperation::Status {
                status_text: "🦀".repeat(32),
            },
            STATUS,
            "status_text",
            json!("🦀".repeat(32)),
        ),
    ] {
        let reply = b
            .commit_me(w, b.personal_metadata(w, input).await.unwrap())
            .unwrap();
        let value = crate::broker::client::user_value(reply);
        assert_eq!(value[field], expected);
        let calls = f.calls(path);
        assert_eq!(calls.len(), 1);
        assert_eq!(calls[0]["method"], "PUT");
        let headers = calls[0]["headers"].as_object().unwrap();
        assert!(!headers.keys().any(|k| k.eq_ignore_ascii_case("Cookie")));
        assert!(!headers.keys().any(|k| k.eq_ignore_ascii_case("Origin")));
        assert!(
            headers
                .keys()
                .any(|k| k.eq_ignore_ascii_case("Authorization"))
        );
        assert!(calls[0]["path"].as_str().unwrap().find('?').is_none());
    }
    let target = "22222222-2222-2222-2222-222222222222";
    let reply = b
        .commit_me(
            w,
            b.personal_metadata(
                w,
                PersonalMetadataOperation::User {
                    user_id: target.to_owned(),
                },
            )
            .await
            .unwrap(),
        )
        .unwrap();
    assert_eq!(reply.account_id().hyphenated().to_string(), target);
    let calls = f.calls(&format!("/api/native/v1/users/{target}"));
    assert_eq!(calls.len(), 1);
    assert_eq!(calls[0]["method"], "GET");
    assert_eq!(calls[0]["body"], "");
    assert_eq!(b.status().unwrap(), Status::Authenticated);
}
#[tokio::test(flavor = "current_thread")]
async fn personal_closed_requests_and_local_bounds_make_no_network_calls() {
    for value in [
        json!({"operation":"profile","display_name":"x","bio":"y","url":"https://outside.example.invalid"}),
        json!({"operation":"locale","locale":"fr"}),
        json!({"operation":"presence","presence":"invisible"}),
        json!({"operation":"user","user_id":null}),
        json!({"operation":"password","password":"public-synthetic-marker"}),
    ] {
        assert!(serde_json::from_value::<PersonalMetadataOperation>(value).is_err());
    }
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    let start = f.state()["calls"].as_array().unwrap().len();
    for input in [
        PersonalMetadataOperation::Profile {
            display_name: "🦀".repeat(25),
            bio: String::new(),
        },
        PersonalMetadataOperation::Profile {
            display_name: String::new(),
            bio: "é".repeat(251),
        },
        PersonalMetadataOperation::Status {
            status_text: "x".repeat(33),
        },
        PersonalMetadataOperation::Status {
            status_text: "\0".to_owned(),
        },
        PersonalMetadataOperation::User {
            user_id: Uuid::nil().to_string(),
        },
        PersonalMetadataOperation::User {
            user_id: "ABCDEFAB-1234-4567-8901-123456789ABC".to_owned(),
        },
        PersonalMetadataOperation::User {
            user_id: "https://outside.example.invalid".to_owned(),
        },
    ] {
        assert!(matches!(
            b.personal_metadata(w, input).await,
            Err(Error::InvalidInput)
        ));
    }
    assert_eq!(f.state()["calls"].as_array().unwrap().len(), start);
    assert_eq!(b.status().unwrap(), Status::Authenticated);
}
#[tokio::test(flavor = "current_thread")]
async fn personal_invalid_response_and_redirect_never_publish_or_retry() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    for mode in [
        "personal_unknown_field",
        "personal_wrong_account",
        "personal_bad_bounds",
        "html",
        "cookie",
        "gzip",
        "redirect",
        "oversized",
    ] {
        f.control(PROFILE, mode);
        let before = f.calls(PROFILE).len();
        assert!(b.personal_metadata(w, profile()).await.is_err());
        assert_eq!(f.calls(PROFILE).len(), before + 1);
        assert_eq!(b.status().unwrap(), Status::Authenticated);
    }
}
#[tokio::test(flavor = "current_thread")]
async fn personal_current_401_requires_reauth_but_stale_401_cannot_clear_new_profile() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    f.control(PROFILE, "unauthorized");
    assert!(matches!(
        b.personal_metadata(w, profile()).await,
        Err(Error::Unauthorized)
    ));
    assert_eq!(b.status().unwrap(), Status::ReauthRequired);
    let (b, w) = logged(&f).await;
    f.control(PROFILE, "hold_unauthorized");
    let running = b.personal_metadata(w, profile());
    tokio::pin!(running);
    tokio::select! {r=&mut running=>panic!("held request unexpectedly completed: {}",r.is_ok()),_=f.wait(PROFILE,2)=>{}}
    let next_origin = f.second_origin();
    let (result, selection) = tokio::join!(
        running,
        b.select_confirmed_profile(w, &next_origin, "fixture-community-2")
    );
    selection.unwrap();
    assert!(matches!(result, Err(Error::Stale) | Err(Error::Cancelled)));
    assert_eq!(b.status().unwrap(), Status::Selected);
    f.release();
    b.login(w, "fixture", password()).await.unwrap();
    assert_eq!(b.status().unwrap(), Status::Authenticated);
}
#[tokio::test(flavor = "current_thread")]
async fn personal_publication_rejects_switch_foreign_window_and_cancelled_native_actor() {
    let f = Fixture::start();
    let (b, w) = logged(&f).await;
    let delivery = b.personal_metadata(w, profile()).await.unwrap();
    let foreign = b
        .register_native_window("side", WindowRole::Widget)
        .unwrap();
    assert!(matches!(b.commit_me(foreign, delivery), Err(Error::Denied)));
    let delivery = b.personal_metadata(w, profile()).await.unwrap();
    b.select_confirmed_profile(w, &f.second_origin(), "second")
        .await
        .unwrap();
    assert!(matches!(
        b.commit_me(w, delivery),
        Err(Error::Stale) | Err(Error::Cancelled)
    ));
    let client = NativeClient::fixture(f.root.clone());
    let lease = client.attach_main_window().unwrap();
    let connected = client
        .request(
            &lease,
            NativeRequest::Connect {
                address: f.origin(),
            },
        )
        .await
        .unwrap();
    client.commit(&lease, connected).unwrap();
    let logged = client
        .request(
            &lease,
            NativeRequest::Login {
                username: "fixture".to_owned(),
                password: password(),
            },
        )
        .await
        .unwrap();
    client.commit(&lease, logged).unwrap();
    f.control(STATUS, "hold");
    let request_id = Uuid::new_v4();
    let op = client.request_with_id(
        &lease,
        request_id,
        NativeRequest::PersonalMetadata {
            operation: PersonalMetadataOperation::Status {
                status_text: "synthetic pending".to_owned(),
            },
        },
    );
    tokio::pin!(op);
    tokio::select! {r=&mut op=>panic!("held actor unexpectedly completed: {}",r.is_ok()),_=f.wait(STATUS,1)=>{}}
    client.cancel_request(&lease, request_id).unwrap();
    assert!(matches!(
        op.await,
        Err(Error::Cancelled) | Err(Error::Stale)
    ));
    let me = client.request(&lease, NativeRequest::Me).await.unwrap();
    assert_eq!(client.commit(&lease, me).unwrap().status, 200);
}
