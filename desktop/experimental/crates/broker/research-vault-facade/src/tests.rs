use super::*;
use std::{
    collections::HashMap,
    sync::{Arc, Barrier, Mutex},
    thread::{self, ThreadId},
};

#[derive(Clone, Copy, Default)]
enum Fault {
    #[default]
    None,
    AcquireSecurity,
    Read,
    WriteBefore,
    WriteAfter,
}
#[derive(Default)]
struct State {
    records: HashMap<[u8; 32], Zeroizing<Vec<u8>>>,
    owned: HashMap<[u8; 32], ThreadId>,
    events: Vec<&'static str>,
    fault: Fault,
    abandon_next: bool,
}
#[derive(Clone, Default)]
struct Api(Arc<Mutex<State>>);
struct Guard {
    api: Api,
    key: [u8; 32],
    owner: ThreadId,
    abandoned: bool,
}
impl Drop for Guard {
    fn drop(&mut self) {
        let mut state = self.api.0.lock().unwrap();
        state.owned.remove(&self.key);
        state.events.push("release")
    }
}
impl Api {
    fn fault(&self, fault: Fault) {
        self.0.lock().unwrap().fault = fault
    }
    fn events(&self) -> Vec<&'static str> {
        self.0.lock().unwrap().events.clone()
    }
    fn bytes(&self, ns: &Namespace) -> Option<Zeroizing<Vec<u8>>> {
        self.0
            .lock()
            .unwrap()
            .records
            .get(&ns.digest)
            .map(|b| Zeroizing::new(b.to_vec()))
    }
}
impl Boundary for Api {
    type Guard = Guard;
    fn acquire(&self, namespace: &Namespace) -> Result<Guard, Error> {
        let mut s = self.0.lock().unwrap();
        s.events.push("acquire");
        if matches!(s.fault, Fault::AcquireSecurity) {
            s.fault = Fault::None;
            return Err(Error::OwnershipUncertain);
        }
        if s.owned.contains_key(&namespace.digest) {
            return Err(Error::Busy);
        }
        let abandoned = std::mem::take(&mut s.abandon_next);
        let owner = thread::current().id();
        s.owned.insert(namespace.digest, owner);
        Ok(Guard {
            api: self.clone(),
            key: namespace.digest,
            owner,
            abandoned,
        })
    }
    fn abandoned(&self, guard: &Guard) -> bool {
        guard.abandoned
    }
    fn read(&self, guard: &mut Guard) -> Result<Option<SecretBlob>, Error> {
        let mut s = self.0.lock().unwrap();
        if thread::current().id() != guard.owner || s.owned.get(&guard.key) != Some(&guard.owner) {
            return Err(Error::OwnershipUncertain);
        }
        s.events.push("read");
        if matches!(s.fault, Fault::Read) {
            s.fault = Fault::None;
            return Err(Error::StoreUnavailable);
        }
        Ok(s.records
            .get(&guard.key)
            .map(|b| SecretBlob::new(b.to_vec())))
    }
    fn write(&self, guard: &mut Guard, bytes: &[u8]) -> Result<(), Error> {
        let mut s = self.0.lock().unwrap();
        if thread::current().id() != guard.owner || s.owned.get(&guard.key) != Some(&guard.owner) {
            return Err(Error::OwnershipUncertain);
        }
        s.events.push("write");
        if matches!(s.fault, Fault::WriteBefore) {
            s.fault = Fault::None;
            return Err(Error::WriteUncertain);
        }
        s.records.insert(guard.key, Zeroizing::new(bytes.to_vec()));
        if matches!(s.fault, Fault::WriteAfter) {
            s.fault = Fault::None;
            return Err(Error::WriteUncertain);
        }
        Ok(())
    }
}
fn ns() -> Namespace {
    Namespace::checked(
        "https://example.test",
        "/",
        "fixture-community",
        [1; 16],
        [2; 16],
    )
    .unwrap()
}
fn record(namespace: &Namespace, sequence: u64, token: u8) -> Record {
    Record::fresh(
        namespace,
        [3; 16],
        sequence,
        Refresh::from_native_bytes([token; 32]).unwrap(),
    )
    .unwrap()
}
fn installed() -> (Api, Vault<Api>, Namespace) {
    let api = Api::default();
    let vault = Vault::new(api.clone());
    let n = ns();
    vault
        .own(n.clone())
        .unwrap()
        .install_new(record(&n, 0, 11))
        .unwrap();
    (api, vault, n)
}

#[test]
fn canonical_namespace_is_bounded_unambiguous_and_partitions_all_identifiers() {
    let n = ns();
    let normalized = Namespace::checked(
        "https://EXAMPLE.test:443/",
        "/",
        "fixture-community",
        [1; 16],
        [2; 16],
    )
    .unwrap();
    assert_eq!(n, normalized);
    for other in [
        Namespace::checked(
            "https://elsewhere.test",
            "/",
            "fixture-community",
            [1; 16],
            [2; 16],
        ),
        Namespace::checked(
            "https://example.test",
            "/other/",
            "fixture-community",
            [1; 16],
            [2; 16],
        ),
        Namespace::checked(
            "https://example.test",
            "/",
            "other-community",
            [1; 16],
            [2; 16],
        ),
        Namespace::checked(
            "https://example.test",
            "/",
            "fixture-community",
            [4; 16],
            [2; 16],
        ),
        Namespace::checked(
            "https://example.test",
            "/",
            "fixture-community",
            [1; 16],
            [4; 16],
        ),
    ] {
        assert_ne!(n, other.unwrap())
    }
    let target = n.target();
    assert_eq!(target.len(), 95);
    assert!(
        target
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b == b'.')
    );
    assert!(!target.contains("example"));
    assert!(!target.contains("community"));
    assert_eq!(
        target.to_ascii_lowercase(),
        normalized.target().to_ascii_lowercase()
    )
}
#[test]
fn rejects_unsafe_or_ambiguous_namespace_inputs() {
    for origin in [
        "http://example.test",
        "https:///example.test",
        "https://example.test/.",
        "https://example.test/../",
        "https://example.test/path",
        "https://u:p@example.test",
        "https://@example.test",
        "https://example.test?x=1",
        "https://example.test#x",
        "https://example.test\n",
        "https://example.test\\other",
        " https://example.test",
    ] {
        assert_eq!(
            Namespace::checked(origin, "/", "community", [1; 16], [2; 16]),
            Err(Error::InvalidInput),
            "accepted invalid fixture"
        )
    }
    for path in [
        "",
        "relative/",
        "/without-end",
        "/../",
        "/a//b/",
        "/a%2Fb/",
        "/a.b/",
        "/\0/",
    ] {
        assert!(
            Namespace::checked("https://example.test", path, "community", [1; 16], [2; 16])
                .is_err()
        )
    }
    for community in ["", "bad space", "bad/segment", "secret\0"] {
        assert!(
            Namespace::checked("https://example.test", "/", community, [1; 16], [2; 16]).is_err()
        )
    }
    assert!(
        Namespace::checked("https://example.test", "/", "community", [0; 16], [2; 16]).is_err()
    );
    assert!(
        Namespace::checked("https://example.test", "/", "community", [1; 16], [0; 16]).is_err()
    );
    assert!(
        Namespace::checked(
            &format!("https://{}", "a".repeat(2049)),
            "/",
            "community",
            [1; 16],
            [2; 16]
        )
        .is_err()
    );
    assert!(
        Namespace::checked(
            "https://example.test",
            "/",
            &"a".repeat(129),
            [1; 16],
            [2; 16]
        )
        .is_err()
    )
}
#[test]
fn fixed_record_contains_only_refresh_and_validates_every_header_and_bound() {
    let n = ns();
    let r = record(&n, 0, 11);
    let bytes = r.encode();
    assert_eq!(bytes.len(), 104);
    assert!(bytes.len() < 2560);
    let restored = Record::decode(&bytes, &n).unwrap();
    assert_eq!(restored.refresh.0, [11; 32]);
    assert_eq!(restored.phase, Phase::Idle);
    for len in 0..104 {
        assert!(Record::decode(&bytes[..len], &n).is_err())
    }
    let mut extra = bytes.to_vec();
    extra.push(0);
    assert!(Record::decode(&extra, &n).is_err());
    for index in [0, 4, 5, 6, 7, 8] {
        let mut changed = Zeroizing::new(bytes.to_vec());
        changed[index] = 255;
        assert!(Record::decode(&changed, &n).is_err())
    }
    for range in [40..56, 72..104] {
        let mut changed = Zeroizing::new(bytes.to_vec());
        changed[range].fill(0);
        assert!(Record::decode(&changed, &n).is_err())
    }
    let mut changed = Zeroizing::new(bytes.to_vec());
    changed[56..64].copy_from_slice(&65536u64.to_be_bytes());
    assert!(Record::decode(&changed, &n).is_err());
    let mut changed = Zeroizing::new(bytes.to_vec());
    changed[64..72].copy_from_slice(&1u64.to_be_bytes());
    assert!(Record::decode(&changed, &n).is_err());
    let mut changed = Zeroizing::new(bytes.to_vec());
    changed[5] = 1;
    assert!(Record::decode(&changed, &n).is_err())
}
#[test]
fn journal_precedes_dispatch_and_successor_visibility() {
    let (api, vault, n) = installed();
    let mut owner = vault.own(n.clone()).unwrap();
    let previous = owner.begin_rotation([3; 16], 0, 1).unwrap();
    let observed = Record::decode(&api.bytes(&n).unwrap(), &n).unwrap();
    assert_eq!(observed.phase, Phase::InFlight);
    assert_eq!(observed.attempt, 1);
    assert!(api.events().ends_with(&["acquire", "read", "write"]));
    owner
        .commit_successor(&previous, record(&n, 1, 12))
        .unwrap();
    let restored = owner.restore_idle().unwrap().unwrap();
    assert_eq!(restored.sequence, 1);
    assert_eq!(restored.refresh.0, [12; 32]);
    assert_eq!(restored.attempt, 0)
}
#[test]
fn uncertain_journal_write_after_mutation_closes_current_and_restarted_owner() {
    let (api, vault, n) = installed();
    let mut owner = vault.own(n.clone()).unwrap();
    api.fault(Fault::WriteAfter);
    assert_eq!(
        owner.begin_rotation([3; 16], 0, 1).unwrap_err(),
        Error::WriteUncertain
    );
    assert_eq!(owner.restore_idle().unwrap_err(), Error::Poisoned);
    drop(owner);
    assert_eq!(
        vault.own(n).unwrap().restore_idle().unwrap_err(),
        Error::ReauthRequired
    )
}
#[test]
fn journal_failure_before_mutation_returns_no_dispatch_secret() {
    let (api, vault, n) = installed();
    let mut owner = vault.own(n.clone()).unwrap();
    api.fault(Fault::WriteBefore);
    assert_eq!(
        owner.begin_rotation([3; 16], 0, 1).unwrap_err(),
        Error::WriteUncertain
    );
    assert_eq!(owner.restore_idle().unwrap_err(), Error::Poisoned);
    drop(owner);
    assert_eq!(
        vault.own(n).unwrap().restore_idle().unwrap().unwrap().phase,
        Phase::Idle
    )
}
#[test]
fn unknown_successor_ack_never_reports_ready_but_restart_uses_only_successor() {
    let (api, vault, n) = installed();
    let mut owner = vault.own(n.clone()).unwrap();
    let previous = owner.begin_rotation([3; 16], 0, 1).unwrap();
    api.fault(Fault::WriteAfter);
    assert_eq!(
        owner.commit_successor(&previous, record(&n, 1, 12)),
        Err(Error::WriteUncertain)
    );
    assert_eq!(owner.restore_idle().unwrap_err(), Error::Poisoned);
    drop(owner);
    let r = vault.own(n).unwrap().restore_idle().unwrap().unwrap();
    assert_eq!(r.sequence, 1);
    assert_eq!(r.refresh.0, [12; 32])
}
#[test]
fn failed_successor_write_preserves_inflight_after_restart() {
    let (api, vault, n) = installed();
    let mut owner = vault.own(n.clone()).unwrap();
    let previous = owner.begin_rotation([3; 16], 0, 1).unwrap();
    api.fault(Fault::WriteBefore);
    assert_eq!(
        owner.commit_successor(&previous, record(&n, 1, 12)),
        Err(Error::WriteUncertain)
    );
    drop(owner);
    assert_eq!(
        vault.own(n).unwrap().restore_idle().unwrap_err(),
        Error::ReauthRequired
    )
}
#[test]
fn read_failure_poisoned_no_followup_write_or_plaintext_fallback() {
    let (api, vault, n) = installed();
    let mut owner = vault.own(n.clone()).unwrap();
    api.fault(Fault::Read);
    assert_eq!(owner.restore_idle().unwrap_err(), Error::StoreUnavailable);
    let events = api.events();
    assert_eq!(owner.install_new(record(&n, 1, 12)), Err(Error::Poisoned));
    assert_eq!(api.events(), events)
}
#[test]
fn permission_or_existing_mutex_security_failure_prevents_store_access() {
    let api = Api::default();
    api.fault(Fault::AcquireSecurity);
    let vault = Vault::new(api.clone());
    assert_eq!(vault.own(ns()).unwrap_err(), Error::OwnershipUncertain);
    assert_eq!(api.events(), vec!["acquire"])
}
#[test]
fn same_thread_reentry_and_other_threads_cannot_reopen_owned_namespace() {
    let (_api, vault, n) = installed();
    let owner = vault.own(n.clone()).unwrap();
    assert_eq!(vault.own(n.clone()).unwrap_err(), Error::Busy);
    let barrier = Arc::new(Barrier::new(2));
    thread::scope(|scope| {
        let sync = barrier.clone();
        let v = &vault;
        let key = n.clone();
        let child = scope.spawn(move || {
            sync.wait();
            assert_eq!(v.own(key).unwrap_err(), Error::Busy)
        });
        barrier.wait();
        child.join().unwrap()
    });
    let other = Namespace::checked(
        "https://other.test",
        "/",
        "fixture-community",
        [1; 16],
        [2; 16],
    )
    .unwrap();
    assert!(vault.own(other).unwrap().restore_idle().unwrap().is_none());
    drop(owner);
    assert!(vault.own(n).is_ok())
}
#[test]
fn ownership_is_retained_across_rotation_not_only_each_store_call() {
    let (_api, vault, n) = installed();
    let mut owner = vault.own(n.clone()).unwrap();
    let previous = owner.begin_rotation([3; 16], 0, 1).unwrap();
    assert_eq!(vault.own(n.clone()).unwrap_err(), Error::Busy);
    owner
        .commit_successor(&previous, record(&n, 1, 12))
        .unwrap();
    assert_eq!(vault.own(n.clone()).unwrap_err(), Error::Busy);
    drop(owner);
    assert!(vault.own(n).is_ok())
}
#[test]
fn abandoned_owner_persists_fail_closed_state_for_following_openers() {
    let (api, vault, n) = installed();
    api.0.lock().unwrap().abandon_next = true;
    assert_eq!(vault.own(n.clone()).unwrap_err(), Error::OwnershipUncertain);
    assert_eq!(
        Record::decode(&api.bytes(&n).unwrap(), &n).unwrap().phase,
        Phase::InFlight
    );
    assert_eq!(
        vault.own(n).unwrap().restore_idle().unwrap_err(),
        Error::ReauthRequired
    )
}
#[test]
fn abandoned_poison_write_failure_is_explicit_and_never_returns_owner() {
    let (api, vault, n) = installed();
    api.0.lock().unwrap().abandon_next = true;
    api.fault(Fault::WriteAfter);
    assert_eq!(vault.own(n.clone()).unwrap_err(), Error::WriteUncertain);
    assert_eq!(
        vault.own(n).unwrap().restore_idle().unwrap_err(),
        Error::ReauthRequired
    )
}
#[test]
fn existing_entry_cannot_be_overwritten_by_initial_login_install() {
    let (api, vault, n) = installed();
    let bytes = api.bytes(&n).unwrap();
    let mut owner = vault.own(n.clone()).unwrap();
    assert_eq!(owner.install_new(record(&n, 0, 12)), Err(Error::Conflict));
    assert_eq!(*api.bytes(&n).unwrap(), *bytes)
}
#[test]
fn corrupt_or_wrong_namespace_entry_is_rejected_and_never_rewritten() {
    let (api, vault, n) = installed();
    api.0.lock().unwrap().records.get_mut(&n.digest).unwrap()[8] ^= 1;
    let bytes = api.bytes(&n).unwrap();
    let mut owner = vault.own(n.clone()).unwrap();
    assert_eq!(owner.restore_idle().unwrap_err(), Error::Corrupt);
    assert_eq!(owner.install_new(record(&n, 0, 12)), Err(Error::Poisoned));
    assert_eq!(*api.bytes(&n).unwrap(), *bytes)
}
#[test]
fn successor_scope_family_sequence_refresh_and_previous_attempt_are_checked() {
    let (api, vault, n) = installed();
    let mut owner = vault.own(n.clone()).unwrap();
    let previous = owner.begin_rotation([3; 16], 0, 9).unwrap();
    let bytes = api.bytes(&n).unwrap();
    for variant in 0..5 {
        let mut successor = record(&n, 1, 12);
        match variant {
            0 => successor.namespace[0] ^= 1,
            1 => successor.family = [4; 16],
            2 => successor.sequence = 2,
            3 => successor.refresh.0 = [11; 32],
            _ => {
                successor.phase = Phase::InFlight;
                successor.attempt = 1
            }
        }
        assert_eq!(
            owner.commit_successor(&previous, successor),
            Err(Error::Conflict)
        );
        assert_eq!(*api.bytes(&n).unwrap(), *bytes)
    }
    api.0.lock().unwrap().records.get_mut(&n.digest).unwrap()[71] = 10;
    assert_eq!(
        owner.commit_successor(&previous, record(&n, 1, 12)),
        Err(Error::Conflict)
    )
}
#[test]
fn external_noncooperating_mutation_before_commit_is_detected_not_cas_claim() {
    let (api, vault, n) = installed();
    let mut owner = vault.own(n.clone()).unwrap();
    let previous = owner.begin_rotation([3; 16], 0, 1).unwrap();
    api.0
        .lock()
        .unwrap()
        .records
        .insert(n.digest, record(&n, 0, 99).encode());
    assert_eq!(
        owner.commit_successor(&previous, record(&n, 1, 12)),
        Err(Error::Conflict)
    );
    assert_eq!(
        Record::decode(&api.bytes(&n).unwrap(), &n)
            .unwrap()
            .refresh
            .0,
        [99; 32]
    )
}
#[test]
fn redacted_formatting_never_renders_secret_identifiers_or_raw_errors() {
    let n = ns();
    let r = record(&n, 0, 91);
    let blob = SecretBlob::new(vec![91; 104]);
    assert_eq!(format!("{blob:?}"), "SecretBlob(REDACTED)");
    let text = format!(
        "{n:?} {r:?} {:?} {:?} {}",
        r.refresh,
        Error::WriteUncertain,
        Error::WriteUncertain
    );
    assert!(!text.contains("example"));
    assert!(!text.contains("fixture-community"));
    assert!(!text.contains("91"));
    assert!(!text.contains(&n.target()));
    assert!(text.contains("REDACTED"))
}
#[test]
fn secret_and_wire_buffers_support_zeroization() {
    let mut refresh = Refresh::from_native_bytes([91; 32]).unwrap();
    refresh.0.zeroize();
    assert_eq!(refresh.0, [0; 32]);
    let mut wire = record(&ns(), 0, 91).encode();
    wire.zeroize();
    assert!(wire.iter().all(|b| *b == 0));
    assert!(Refresh::from_native_bytes([0; 32]).is_err());
    assert!(
        Record::fresh(
            &ns(),
            [0; 16],
            0,
            Refresh::from_native_bytes([1; 32]).unwrap()
        )
        .is_err()
    )
}

#[test]
fn documents_unresolved_cross_process_abandon_poison_failure_before_mutation() {
    let (api, vault, n) = installed();
    api.0.lock().unwrap().abandon_next = true;
    api.fault(Fault::WriteBefore);
    assert_eq!(vault.own(n.clone()).unwrap_err(), Error::WriteUncertain);
    // Native releasing an abandoned mutex clears that kernel indication.
    // An unmodified Idle credential can remain if poisoning did not commit.
    // This proves the explicitly documented qualification gap, not safe recovery.
    let later_candidate = vault.own(n).unwrap().restore_idle().unwrap().unwrap();
    assert_eq!(later_candidate.phase, Phase::Idle);
    assert_eq!(later_candidate.refresh.0, [11; 32]);
}
