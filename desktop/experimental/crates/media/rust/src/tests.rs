use super::*;
#[cfg(feature = "synthetic-media-fixture")]
use std::time::Duration;
#[cfg(feature = "synthetic-media-fixture")]
fn id(n: u128) -> Id {
    Id::native(Uuid::from_u128(n)).unwrap()
}
#[test]
fn fixed_action_schema_rejects_unknown_fields_and_noncanonical_ids() {
    for input in [
        r#"{"type":"voice_join","payload":{"channel_id":"11111111-1111-1111-1111-111111111111","extra":true}}"#,
        r#"{"type":"voice_join","payload":{"channel_id":"11111111111111111111111111111111"}}"#,
        r#"{"type":"voice_join","payload":{"channel_id":"00000000-0000-0000-0000-000000000000"}}"#,
        r#"{"type":"voice_leave","payload":{"channel_id":"11111111-1111-1111-1111-111111111111"}}"#,
        r#"{"type":"webrtc_offer","payload":{}}"#,
        r#"{"type":"voice_join","payload":{"channel_id":"11111111-1111-1111-1111-111111111111"},"grant":true}"#,
    ] {
        assert!(decode_action(input.as_bytes()).is_err(), "schema must fail")
    }
    assert!(decode_action(br#"{"type":"voice_leave"}"#).is_ok());
    assert!(
        decode_action(
            br#"{"type":"webrtc_answer","payload":{"type":"answer","sdp":"public fixture"}}"#
        )
        .is_ok()
    );
}
#[test]
fn sdp_and_candidate_bounds_types_controls_are_enforced() {
    for sdp in ["".to_owned(), "x".repeat(49153), "a\0b".to_owned()] {
        let v = serde_json::json!({"type":"webrtc_answer","payload":{"type":"answer","sdp":sdp}});
        assert!(decode_action(&serde_json::to_vec(&v).unwrap()).is_err())
    }
    for value in [
        serde_json::json!({"candidate":"x".repeat(4097)}),
        serde_json::json!({"candidate":"a\nb"}),
        serde_json::json!({"candidate":"public","sdpMLineIndex":33}),
        serde_json::json!({"candidate":"public","sdpMLineIndex":-1}),
        serde_json::json!({"candidate":"public","usernameFragment":"x".repeat(257)}),
        serde_json::json!({"candidate":"public","access_token":"fixture"}),
    ] {
        let v = serde_json::json!({"type":"webrtc_candidate","payload":value});
        assert!(decode_action(&serde_json::to_vec(&v).unwrap()).is_err())
    }
    let v=br#"{"type":"webrtc_candidate","payload":{"candidate":"public fixture","sdpMid":"0","sdpMLineIndex":0,"usernameFragment":null}}"#;
    assert!(decode_action(v).is_ok());
    assert_eq!(
        decode_action(&vec![b' '; 65537]).unwrap_err(),
        Error::BodyLimit
    );
}
#[test]
fn incoming_bridge_never_forwards_control_or_content() {
    assert!(
        decode_message(br#"{"type":"chat_message","payload":{"text":"public fixture"}}"#)
            .unwrap()
            .is_none()
    );
    assert!(
        decode_message(br#"{"type":"native_renewed","payload":{"access_expires_at":"fixture"}}"#)
            .is_err()
    );
    assert!(decode_message(br#"{"type":"webrtc_offer","payload":{"type":"offer","sdp":"public fixture","key":"fixture"}}"#).is_err());
    assert!(matches!(
        decode_message(
            br#"{"type":"webrtc_offer","payload":{"type":"offer","sdp":"public fixture"}}"#
        )
        .unwrap(),
        Some(MediaMessage::Offer(_))
    ));
    assert_eq!(
        decode_message(&vec![b' '; 65537]).unwrap_err(),
        Error::BodyLimit
    );
}
#[test]
fn sensitive_debug_is_redacted() {
    let a=decode_action(br#"{"type":"webrtc_answer","payload":{"type":"answer","sdp":"private marker must not appear"}}"#).unwrap();
    assert_eq!(format!("{a:?}"), "SyntheticMediaValue(REDACTED)");
}
#[cfg(not(feature = "synthetic-media-fixture"))]
#[test]
fn production_default_cannot_load_fixture() {
    assert_eq!(
        load_owned_bootstrap(Path::new("/private/fixture/bootstrap.json")).unwrap_err(),
        Error::QualificationRequired
    );
}

#[cfg(feature = "synthetic-media-fixture")]
mod fixture {
    use super::*;
    fn value() -> serde_json::Value {
        serde_json::json!({"schema":1,"scope":"synthetic native authenticated SFU qualification, no MLS grant","origin":"https://127.0.0.1:8443","ca_pem":include_str!("test-ca.fixture.txt"),"community_id":id(1).uuid().to_string(),"channel_id":id(2).uuid().to_string(),"publisher":{"user_id":id(3).uuid().to_string(),"username":"nws-public-fixture","password":DUMMY_PASSWORD},"viewer":{"user_id":id(4).uuid().to_string(),"username":"media-public-fixture","password":DUMMY_PASSWORD}})
    }
    fn fixture() -> OwnedFixture {
        decode_bootstrap(&serde_json::to_vec(&value()).unwrap()).unwrap()
    }
    fn binding() -> NativeBinding {
        NativeBinding {
            window_context: id(10),
            profile_context: id(11),
            account_id: id(3),
            family_id: id(12),
            client_instance_id: id(13),
            socket_generation: 1,
            refresh_sequence: 0,
        }
    }
    fn cap() -> FixtureCapability {
        FixtureCapability::bind(
            &fixture(),
            Role::Publisher,
            binding(),
            Instant::now() + Duration::from_secs(10),
        )
        .unwrap()
    }
    #[test]
    fn bootstrap_requires_exact_fixed_loopback_policy() {
        for origin in [
            "http://127.0.0.1:8443",
            "https://127.0.0.2:8443",
            "https://localhost:8443",
            "https://fixture.example:8443",
            "https://user@127.0.0.1:8443",
            "https://127.0.0.1:8443/path",
            "https://127.0.0.1:8443?x=1",
            "https://127.0.0.1:8443#x",
            "https://127.0.0.1",
        ] {
            let mut v = value();
            v["origin"] = origin.into();
            assert!(decode_bootstrap(&serde_json::to_vec(&v).unwrap()).is_err())
        }
        let mut v = value();
        v["publisher"]["password"] = "other accidental credential".into();
        assert!(decode_bootstrap(&serde_json::to_vec(&v).unwrap()).is_err());
        let mut v = value();
        v["publisher"]["username"] = "production-account".into();
        assert!(decode_bootstrap(&serde_json::to_vec(&v).unwrap()).is_err());
        let mut v = value();
        v["viewer"]["user_id"] = v["publisher"]["user_id"].clone();
        assert!(decode_bootstrap(&serde_json::to_vec(&v).unwrap()).is_err());
        let mut v = value();
        v["token"] = "unwanted".into();
        assert!(decode_bootstrap(&serde_json::to_vec(&v).unwrap()).is_err());
    }
    #[test]
    fn authority_is_single_exact_pem_pin_not_global_trust() {
        let f = fixture();
        assert_ne!(f.authority_fingerprint(), [0; 32]);
        assert_eq!(f.origin().as_str(), "https://127.0.0.1:8443/");
        assert!(f.authority().to_der().is_ok());
        assert_eq!(format!("{f:?}"), "SyntheticMediaValue(REDACTED)");
        for (role, expected) in [(Role::Publisher, id(3)), (Role::Viewer, id(4))] {
            let profile = f.native_profile(role);
            assert_eq!(profile.origin(), f.origin());
            assert_eq!(profile.authority_pem(), f.authority_pem());
            assert_eq!(profile.authority_fingerprint(), f.authority_fingerprint());
            assert_eq!(profile.expected_account(), expected);
            assert_eq!(profile.community_id(), f.community_id());
            assert_eq!(profile.username(), f.login(role).username());
            assert_eq!(profile.password(), DUMMY_PASSWORD);
            assert_eq!(format!("{profile:?}"), "NativeFixtureProfile(REDACTED)");
        }
        for pem in [
            "not a cert".to_owned(),
            format!(
                "{}{}",
                include_str!("test-ca.fixture.txt"),
                include_str!("test-ca.fixture.txt")
            ),
            format!(
                "{}\n-----BEGIN PRIVATE KEY-----\nprivate\n-----END PRIVATE KEY-----",
                include_str!("test-ca.fixture.txt")
            ),
        ] {
            let mut v = value();
            v["ca_pem"] = pem.into();
            assert!(decode_bootstrap(&serde_json::to_vec(&v).unwrap()).is_err())
        }
    }
    #[test]
    fn capability_denies_every_changed_native_identity_and_deadline() {
        let c = cap();
        let b = c.bound();
        c.check(Some(&binding()), &b.handle, b.generation, Instant::now())
            .unwrap();
        let mut changes = vec![];
        for mutate in [0, 1, 2, 3, 4, 5, 6] {
            let mut v = binding();
            match mutate {
                0 => v.window_context = id(20),
                1 => v.profile_context = id(20),
                2 => v.account_id = id(20),
                3 => v.family_id = id(20),
                4 => v.client_instance_id = id(20),
                5 => v.socket_generation = 2,
                _ => v.refresh_sequence = 1,
            };
            changes.push(v)
        }
        for current in changes {
            assert!(
                c.check(Some(&current), &b.handle, b.generation, Instant::now())
                    .is_err()
            )
        }
        assert!(
            c.check(None, &b.handle, b.generation, Instant::now())
                .is_err()
        );
        assert!(
            c.check(Some(&binding()), "wrong", b.generation, Instant::now())
                .is_err()
        );
        assert!(
            c.check(Some(&binding()), &b.handle, 2, Instant::now())
                .is_err()
        );
        assert_eq!(
            c.check(
                Some(&binding()),
                &b.handle,
                1,
                Instant::now() + Duration::from_secs(11)
            ),
            Err(Error::Expired)
        );
        c.seal();
        assert!(
            c.check(Some(&binding()), &b.handle, 1, Instant::now())
                .is_err()
        );
    }
    #[test]
    fn fixed_channel_peer_and_role_cannot_be_widened() {
        let f = fixture();
        let c = cap();
        let b = c.bound();
        c.authorize(
            Some(&binding()),
            &b.handle,
            1,
            Instant::now(),
            &Action::Join {
                channel_id: f.channel_id(),
            },
        )
        .unwrap();
        assert!(
            c.authorize(
                Some(&binding()),
                &b.handle,
                1,
                Instant::now(),
                &Action::Join { channel_id: id(30) }
            )
            .is_err()
        );
        assert!(
            c.authorize(
                Some(&binding()),
                &b.handle,
                1,
                Instant::now(),
                &Action::Subscribe {
                    kind: SubscriptionKind::Screen,
                    user_id: f.publisher.id,
                    on: true
                }
            )
            .is_err()
        );
        let mut current = binding();
        current.account_id = f.viewer.id;
        let c = FixtureCapability::bind(
            &f,
            Role::Viewer,
            current,
            Instant::now() + Duration::from_secs(10),
        )
        .unwrap();
        let b = c.bound();
        c.authorize(
            Some(&current),
            &b.handle,
            1,
            Instant::now(),
            &Action::Subscribe {
                kind: SubscriptionKind::Screen,
                user_id: f.publisher.id,
                on: true,
            },
        )
        .unwrap();
        assert!(
            c.authorize(
                Some(&current),
                &b.handle,
                1,
                Instant::now(),
                &Action::Subscribe {
                    kind: SubscriptionKind::Screen,
                    user_id: id(30),
                    on: true
                }
            )
            .is_err()
        );
    }
    #[test]
    fn native_ack_renewal_requires_next_exact_same_session_and_live_lease() {
        let mut c = cap();
        let b = c.bound();
        let mut next = binding();
        next.refresh_sequence = 1;
        let mut bad = next;
        bad.family_id = id(99);
        assert!(
            c.renew_after_native_ack(bad, Instant::now() + Duration::from_secs(20))
                .is_err()
        );
        bad = next;
        bad.refresh_sequence = 2;
        assert!(
            c.renew_after_native_ack(bad, Instant::now() + Duration::from_secs(20))
                .is_err()
        );
        c.renew_after_native_ack(next, Instant::now() + Duration::from_secs(20))
            .unwrap();
        assert!(
            c.check(Some(&binding()), &b.handle, 1, Instant::now())
                .is_err()
        );
        c.check(Some(&next), &b.handle, 1, Instant::now()).unwrap();
        c.seal();
        next.refresh_sequence = 2;
        assert!(
            c.renew_after_native_ack(next, Instant::now() + Duration::from_secs(30))
                .is_err()
        );
    }
    #[test]
    fn capability_is_unique_and_bounded_no_renderer_generation_overflow() {
        let first = cap().bound();
        let second = cap().bound();
        assert_ne!(first.handle, second.handle);
        assert_eq!(first.handle.len(), 64);
        let mut v = binding();
        v.socket_generation = 9_007_199_254_740_992;
        assert!(
            FixtureCapability::bind(
                &fixture(),
                Role::Publisher,
                v,
                Instant::now() + Duration::from_secs(10)
            )
            .is_err()
        );
        assert!(
            FixtureCapability::bind(
                &fixture(),
                Role::Publisher,
                binding(),
                Instant::now() + Duration::from_secs(301)
            )
            .is_err()
        );
    }
    #[test]
    fn actual_event_reply_schema_and_sequence_cannot_publish_after_seal() {
        let c = cap();
        let mut cursor = EventCursor::default();
        let message = decode_message(
            br#"{"type":"webrtc_offer","payload":{"type":"offer","sdp":"public fixture"}}"#,
        )
        .unwrap()
        .unwrap();
        let event = cursor
            .message(&c, Some(&binding()), Instant::now(), message.clone())
            .unwrap();
        let v = serde_json::to_value(event).unwrap();
        assert_eq!(v["type"], "message");
        assert_eq!(v["sequence"], 1);
        assert_eq!(v["generation"], 1);
        assert_eq!(v["message"]["type"], "offer");
        assert!(v.get("family_id").is_none());
        let reply = serde_json::to_value(c.bound()).unwrap();
        assert!(reply.get("channelId").is_some());
        assert!(reply.get("publisherUserId").is_some());
        assert!(reply.get("selfUserId").is_some());
        c.seal();
        assert!(
            cursor
                .message(&c, Some(&binding()), Instant::now(), message)
                .is_err()
        );
        let v = serde_json::to_value(cursor.closed(&c).unwrap()).unwrap();
        assert_eq!(v["sequence"], 2);
        cursor.sequence = 9_007_199_254_740_991;
        assert!(cursor.closed(&c).is_err());
    }
    #[cfg(unix)]
    #[test]
    fn owned_file_modes_links_and_symlinks_are_failclosed() {
        use std::{
            fs,
            os::unix::fs::{PermissionsExt, symlink},
        };
        let dir = std::env::temp_dir().join(format!("mnema-public-policy-test-{}", Uuid::new_v4()));
        fs::create_dir(&dir).unwrap();
        fs::set_permissions(&dir, fs::Permissions::from_mode(0o700)).unwrap();
        let p = dir.join("bootstrap.json");
        fs::write(&p, serde_json::to_vec(&value()).unwrap()).unwrap();
        fs::set_permissions(&p, fs::Permissions::from_mode(0o600)).unwrap();
        assert!(load_owned_bootstrap(&p).is_ok());
        fs::set_permissions(&p, fs::Permissions::from_mode(0o644)).unwrap();
        assert!(load_owned_bootstrap(&p).is_err());
        fs::set_permissions(&p, fs::Permissions::from_mode(0o600)).unwrap();
        fs::set_permissions(&dir, fs::Permissions::from_mode(0o755)).unwrap();
        assert!(load_owned_bootstrap(&p).is_err());
        fs::set_permissions(&dir, fs::Permissions::from_mode(0o700)).unwrap();
        let other = dir.join("other.json");
        fs::rename(&p, &other).unwrap();
        symlink(&other, &p).unwrap();
        assert!(load_owned_bootstrap(&p).is_err());
        fs::remove_file(&p).unwrap();
        fs::hard_link(&other, &p).unwrap();
        assert!(load_owned_bootstrap(&p).is_err());
        fs::remove_file(&other).unwrap();
        fs::write(&p, vec![b' '; 16385]).unwrap();
        assert_eq!(load_owned_bootstrap(&p).unwrap_err(), Error::BodyLimit);
        fs::remove_dir_all(dir).unwrap();
    }
}
