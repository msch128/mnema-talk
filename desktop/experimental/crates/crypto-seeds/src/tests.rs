//! Injected native boundary fault tests; no actual credential manager calls.
use super::*;
use std::cell::Cell;
#[derive(Clone, Copy)]
enum Fault {
    None,
    BeforeWrite,
    AfterWrite,
    WrongReadback,
    Read,
    Abandoned,
}
struct Fake {
    values: RefCell<HashMap<[u8; 32], Vec<u8>>>,
    fault: Cell<Fault>,
    writes: Cell<usize>,
}
impl Fake {
    fn new() -> Self {
        Self {
            values: RefCell::new(HashMap::new()),
            fault: Cell::new(Fault::None),
            writes: Cell::new(0),
        }
    }
}
impl Boundary for Fake {
    type Guard = [u8; 32];
    fn acquire(&self, n: &Namespace) -> Result<Self::Guard, Error> {
        Ok(n.digest)
    }
    fn abandoned(&self, _: &Self::Guard) -> bool {
        matches!(self.fault.get(), Fault::Abandoned)
    }
    fn read(&self, g: &mut Self::Guard) -> Result<Option<SecretBlob>, Error> {
        if matches!(self.fault.get(), Fault::Read) {
            return Err(Error::StoreUnavailable);
        }
        let b = self.values.borrow().get(g).cloned();
        Ok(b.map(|mut bytes| {
            if self.writes.get() > 0 && matches!(self.fault.get(), Fault::WrongReadback) {
                bytes[0] ^= 1
            }
            SecretBlob::new(bytes)
        }))
    }
    fn write(&self, g: &mut Self::Guard, b: &[u8]) -> Result<(), Error> {
        self.writes.set(self.writes.get() + 1);
        if matches!(self.fault.get(), Fault::BeforeWrite) {
            return Err(Error::WriteUncertain);
        }
        self.values.borrow_mut().insert(*g, b.to_vec());
        if matches!(self.fault.get(), Fault::AfterWrite) {
            return Err(Error::WriteUncertain);
        }
        Ok(())
    }
}
fn vault() -> Vault<Fake> {
    Vault {
        boundary: Fake::new(),
        namespace: Namespace { digest: [7; 32] },
        states: RefCell::new(HashMap::new()),
        _thread_bound: PhantomData,
    }
}
#[test]
fn fresh_seeds_read_back_exactly_and_aliases_have_separate_domains() {
    let v = vault();
    let a = v.create("issuer-root").unwrap();
    let b = v.create("database-key").unwrap();
    assert_eq!(v.read("issuer-root").unwrap().as_slice(), a.as_slice());
    assert_eq!(v.read("database-key").unwrap().as_slice(), b.as_slice());
    assert_ne!(a.as_slice(), b.as_slice());
    assert_eq!(v.boundary.writes.get(), 2);
}
#[test]
fn duplicate_create_never_overwrites_existing_key() {
    let v = vault();
    let original = v.create("device-key").unwrap();
    assert_eq!(v.create("device-key").unwrap_err(), Error::Quarantined);
    assert_eq!(
        v.read("device-key").unwrap().as_slice(),
        original.as_slice()
    );
    assert_eq!(v.boundary.writes.get(), 1);
}
#[test]
fn unknown_write_ack_burns_alias_before_and_after_actual_write() {
    for f in [Fault::BeforeWrite, Fault::AfterWrite, Fault::WrongReadback] {
        let v = vault();
        v.boundary.fault.set(f);
        assert!(v.create("issuer-root").is_err());
        v.boundary.fault.set(Fault::None);
        assert_eq!(v.read("issuer-root").unwrap_err(), Error::Quarantined);
        assert_eq!(v.create("issuer-root").unwrap_err(), Error::Quarantined);
        assert_eq!(v.boundary.writes.get(), 1);
    }
}
#[test]
fn preexisting_orphan_key_cannot_be_imported_or_replaced() {
    let v = vault();
    v.boundary
        .values
        .borrow_mut()
        .insert(v.namespace.alias("database-key").digest, vec![5; 32]);
    assert_eq!(v.read("database-key").unwrap_err(), Error::Quarantined);
    assert_eq!(v.create("database-key").unwrap_err(), Error::Quarantined);
    assert_eq!(v.boundary.writes.get(), 0);
}
#[test]
fn changed_or_unavailable_os_key_permanently_quarantines_live_alias() {
    for f in [Fault::Read, Fault::WrongReadback, Fault::Abandoned] {
        let v = vault();
        v.create("device-key").unwrap();
        v.boundary.fault.set(f);
        assert!(v.read("device-key").is_err());
        v.boundary.fault.set(Fault::None);
        assert_eq!(v.read("device-key").unwrap_err(), Error::Quarantined);
        assert_eq!(v.boundary.writes.get(), 1);
    }
}
#[test]
fn abandoned_owner_never_reads_or_writes_fresh_key() {
    let v = vault();
    v.boundary.fault.set(Fault::Abandoned);
    assert_eq!(
        v.create("issuer-root").unwrap_err(),
        Error::OwnershipUncertain
    );
    assert!(v.boundary.values.borrow().is_empty());
    assert_eq!(v.boundary.writes.get(), 0);
}
#[test]
fn only_fixed_crypto_aliases_are_admitted() {
    let v = vault();
    for a in [
        "",
        "refresh-token",
        "../issuer-root",
        "issuer-root\0",
        "Issuer-root",
    ] {
        assert_eq!(v.create(a).unwrap_err(), Error::InvalidInput);
        assert_eq!(v.read(a).unwrap_err(), Error::InvalidInput)
    }
    assert_eq!(v.boundary.writes.get(), 0);
}
